package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"github.com/Ltre/MusicoletWeb/internal/merge"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
)

func (s *Service) applyImportJournal(ctx context.Context, jid, pid, versionNo, snapshotID int64, zipSHA string, result domain.Snapshot, mainCommit string) error {
	theirs, err := s.loadSnapshot(ctx, snapshotID)
	if err != nil {
		return err
	}
	decisions, err := s.activeChangeDecisions(ctx, result, theirs)
	if err != nil {
		return err
	}

	return s.Store.Tx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM musicolet_versions WHERE version_no=?)", versionNo).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if err := replaceWorking(ctx, tx, result); err != nil {
				return err
			}
			now := db.NowMS()
			r, err := tx.ExecContext(ctx, "INSERT INTO musicolet_versions(version_no,snapshot_id,source_zip_sha256,created_at) VALUES(?,?,?,?)", versionNo, snapshotID, zipSHA, now)
			if err != nil {
				return err
			}
			vid, err := r.LastInsertId()
			if err != nil {
				return err
			}
			if r, err = tx.ExecContext(ctx, "UPDATE snapshots SET state='VERSION' WHERE id=?", snapshotID); err != nil {
				return err
			} else if err = db.CheckAffected(r); err != nil {
				return err
			}
			for cid, active := range decisions {
				r, err = tx.ExecContext(ctx, "UPDATE server_changes SET active=? WHERE id=?", boolInt(active), cid)
				if err != nil {
					return err
				}
				if err = db.CheckAffected(r); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,after_json,git_commit,active,created_at) VALUES(?,?,?,?,?,?,0,?)", vid, "import", "version", fmt.Sprintf("IMPORT_V%d", versionNo), merge.JSON(result), mainCommit, now); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE working_songs SET has_server_changes=EXISTS(SELECT 1 FROM change_targets ct JOIN server_changes sc ON sc.id=ct.change_id WHERE sc.active=1 AND ct.target_type='song' AND ct.target_key=working_songs.path)"); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE working_playlists SET has_server_changes=EXISTS(SELECT 1 FROM change_targets ct JOIN server_changes sc ON sc.id=ct.change_id WHERE sc.active=1 AND ct.target_type='playlist' AND ct.target_key=working_playlists.name)"); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE working_queues SET has_server_changes=EXISTS(SELECT 1 FROM change_targets ct JOIN server_changes sc ON sc.id=ct.change_id WHERE sc.active=1 AND ct.target_type='queue' AND ct.target_key=working_queues.name)"); err != nil {
				return err
			}
		}

		now := db.NowMS()
		r, err := tx.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTED',committed_at=COALESCE(committed_at,?),updated_at=? WHERE id=?", now, now, pid)
		if err != nil {
			return err
		}
		if err = db.CheckAffected(r); err != nil {
			return err
		}
		r, err = tx.ExecContext(ctx, "UPDATE commit_journal SET status='DONE',updated_at=? WHERE id=?", now, jid)
		if err != nil {
			return err
		}
		return db.CheckAffected(r)
	})
}

func (s *Service) recoverJournalCommit(ref, label, msg, parent string, data []byte, parents ...string) (string, error) {
	cur := s.Git.Head(ref)
	if cur == parent {
		return s.Git.CommitJSON(ref, msg, data, parents...)
	}
	if cur == "" && parent == "" {
		return s.Git.CommitJSON(ref, msg, data, parents...)
	}
	if cur == "" {
		return "", fmt.Errorf("%s ref disappeared during journal recovery", label)
	}
	ok, err := s.Git.CommitMatches(cur, data, parents...)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s ref advanced unexpectedly to %s; refusing to attach journal to unrelated commit", label, cur)
	}
	return cur, nil
}

func (s *Service) validateRecordedJournalCommit(ref, label, commit string, data []byte, parents ...string) error {
	ok, err := s.Git.CommitMatches(commit, data, parents...)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("recorded %s commit %s does not match journal state/parents", label, commit)
	}
	if head := s.Git.Head(ref); head != commit {
		return fmt.Errorf("%s ref is %s, expected recorded journal commit %s", label, head, commit)
	}
	return nil
}

func (s *Service) RecoverCommitJournals(ctx context.Context) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,procedure_id,target_version_no,state_json,COALESCE(source_parent,''),COALESCE(main_parent,''),COALESCE(source_commit,''),COALESCE(main_commit,''),status FROM commit_journal WHERE status<>'DONE' ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	type jrow struct {
		id, pid, ver                   int64
		state, sp, mp, sc, mc, status string
	}
	var js []jrow
	for rows.Next() {
		var j jrow
		if err = rows.Scan(&j.id, &j.pid, &j.ver, &j.state, &j.sp, &j.mp, &j.sc, &j.mc, &j.status); err != nil {
			return err
		}
		js = append(js, j)
	}
	if err = rows.Err(); err != nil {
		return err
	}

	for _, j := range js {
		p, err := s.GetProcedure(ctx, j.pid)
		if err != nil {
			return err
		}
		var result domain.Snapshot
		if err = json.Unmarshal([]byte(j.state), &result); err != nil {
			return err
		}
		theirs, err := s.loadSnapshot(ctx, p.CandidateSnapshotID)
		if err != nil {
			return err
		}
		sourceData := musicolet.CanonicalSnapshot(theirs)

		sc := j.sc
		if sc == "" {
			sc, err = s.recoverJournalCommit("refs/heads/musicolet-source", "musicolet-source", fmt.Sprintf("Musicolet V%d", j.ver), j.sp, sourceData, j.sp)
			if err != nil {
				return err
			}
			if _, err = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET source_commit=?,status='SOURCE_DONE',updated_at=? WHERE id=?", sc, db.NowMS(), j.id); err != nil {
				return err
			}
		} else if err = s.validateRecordedJournalCommit("refs/heads/musicolet-source", "musicolet-source", sc, sourceData, j.sp); err != nil {
			return err
		}

		parents := []string{}
		if j.mp != "" {
			parents = append(parents, j.mp)
		}
		parents = append(parents, sc)
		mc := j.mc
		if mc == "" {
			mc, err = s.recoverJournalCommit("refs/heads/main", "main", fmt.Sprintf("Import Musicolet V%d", j.ver), j.mp, []byte(j.state), parents...)
			if err != nil {
				return err
			}
			if _, err = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET main_commit=?,status='GIT_DONE',updated_at=? WHERE id=?", mc, db.NowMS(), j.id); err != nil {
				return err
			}
		} else if err = s.validateRecordedJournalCommit("refs/heads/main", "main", mc, []byte(j.state), parents...); err != nil {
			return err
		}

		if err = s.applyImportJournal(ctx, j.id, j.pid, j.ver, p.CandidateSnapshotID, p.SHA256, result, mc); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) activeChangeDecisions(ctx context.Context, result, incoming domain.Snapshot) (map[int64]bool, error) {
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,operation FROM server_changes WHERE active=1 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	plR, plI := mapPL(result.Playlists), mapPL(incoming.Playlists)
	qR, qI := mapQ(result.Queues), mapQ(incoming.Queues)
	for rows.Next() {
		var id int64
		var typ, key, op string
		if err = rows.Scan(&id, &typ, &key, &op); err != nil {
			return nil, err
		}
		active := false
		switch {
		case typ == "song" && op == "FAVORITE":
			active = result.Favorites[key] != incoming.Favorites[key]
		case typ == "song" && op == "PLAY":
			active = false
		case typ == "song":
			r, rok := result.Songs[key]
			i, iok := incoming.Songs[key]
			active = rok != iok || (rok && iok && r.CoreKey() != i.CoreKey())
		case typ == "playlist":
			active = !equal(plR[key], plI[key])
		case typ == "queue":
			active = !equal(qR[key], qI[key])
		case typ == "queue_order":
			active = !equal(queueNames(result.Queues), queueNames(incoming.Queues))
		default:
			active = true
		}
		out[id] = active
	}
	return out, rows.Err()
}
