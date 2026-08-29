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
	theirs, _ := s.loadSnapshot(ctx, snapshotID)
	decisions, _ := s.activeChangeDecisions(ctx, result, theirs)
	return s.Store.Tx(ctx, func(tx *sql.Tx) error {
		var exists int
		_ = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM musicolet_versions WHERE version_no=?)", versionNo).Scan(&exists)
		if exists == 0 {
			if e := replaceWorking(ctx, tx, result); e != nil {
				return e
			}
			now := db.NowMS()
			r, e := tx.ExecContext(ctx, "INSERT INTO musicolet_versions(version_no,snapshot_id,source_zip_sha256,created_at) VALUES(?,?,?,?)", versionNo, snapshotID, zipSHA, now)
			if e != nil {
				return e
			}
			vid, _ := r.LastInsertId()
			if _, e = tx.ExecContext(ctx, "UPDATE snapshots SET state='VERSION' WHERE id=?", snapshotID); e != nil {
				return e
			}
			for cid, active := range decisions {
				_, _ = tx.ExecContext(ctx, "UPDATE server_changes SET active=? WHERE id=?", boolInt(active), cid)
			}
			_, _ = tx.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,after_json,git_commit,active,created_at) VALUES(?,?,?,?,?,?,0,?)", vid, "import", "version", fmt.Sprintf("IMPORT_V%d", versionNo), merge.JSON(result), mainCommit, now)
			_, _ = tx.ExecContext(ctx, "UPDATE working_songs SET has_server_changes=EXISTS(SELECT 1 FROM change_targets ct JOIN server_changes sc ON sc.id=ct.change_id WHERE sc.active=1 AND ct.target_type='song' AND ct.target_key=working_songs.path)")
			_, _ = tx.ExecContext(ctx, "UPDATE working_playlists SET has_server_changes=EXISTS(SELECT 1 FROM server_changes sc WHERE sc.active=1 AND sc.target_type='playlist' AND sc.target_key=working_playlists.name)")
			_, _ = tx.ExecContext(ctx, "UPDATE working_queues SET has_server_changes=EXISTS(SELECT 1 FROM server_changes sc WHERE sc.active=1 AND sc.target_type='queue' AND sc.target_key=working_queues.name)")
		}
		now := db.NowMS()
		_, _ = tx.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTED',committed_at=COALESCE(committed_at,?),updated_at=? WHERE id=?", now, now, pid)
		_, _ = tx.ExecContext(ctx, "UPDATE commit_journal SET status='DONE',updated_at=? WHERE id=?", now, jid)
		return nil
	})
}

// RecoverCommitJournals makes import commits crash-resumable across the SQLite/Git boundary.

func (s *Service) RecoverCommitJournals(ctx context.Context) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,procedure_id,target_version_no,state_json,COALESCE(source_parent,''),COALESCE(main_parent,''),COALESCE(source_commit,''),COALESCE(main_commit,''),status FROM commit_journal WHERE status<>'DONE' ORDER BY id")
	if e != nil {
		return e
	}
	defer rows.Close()
	type jrow struct {
		id, pid, ver                  int64
		state, sp, mp, sc, mc, status string
	}
	var js []jrow
	for rows.Next() {
		var j jrow
		if e = rows.Scan(&j.id, &j.pid, &j.ver, &j.state, &j.sp, &j.mp, &j.sc, &j.mc, &j.status); e != nil {
			return e
		}
		js = append(js, j)
	}
	for _, j := range js {
		p, e := s.GetProcedure(ctx, j.pid)
		if e != nil {
			return e
		}
		var result domain.Snapshot
		if e = json.Unmarshal([]byte(j.state), &result); e != nil {
			return e
		}
		theirs, e := s.loadSnapshot(ctx, p.CandidateSnapshotID)
		if e != nil {
			return e
		}
		sc := j.sc
		if sc == "" {
			cur := s.Git.Head("refs/heads/musicolet-source")
			if cur != "" && cur != j.sp {
				sc = cur
			} else {
				sc, e = s.Git.CommitJSON("refs/heads/musicolet-source", fmt.Sprintf("Musicolet V%d", j.ver), musicolet.CanonicalSnapshot(theirs), j.sp)
				if e != nil {
					return e
				}
			}
			_, _ = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET source_commit=?,status='SOURCE_DONE',updated_at=? WHERE id=?", sc, db.NowMS(), j.id)
		}
		mc := j.mc
		if mc == "" {
			cur := s.Git.Head("refs/heads/main")
			if cur != "" && cur != j.mp {
				mc = cur
			} else {
				parents := []string{}
				if j.mp != "" {
					parents = append(parents, j.mp)
				}
				parents = append(parents, sc)
				mc, e = s.Git.CommitJSON("refs/heads/main", fmt.Sprintf("Import Musicolet V%d", j.ver), []byte(j.state), parents...)
				if e != nil {
					return e
				}
			}
			_, _ = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET main_commit=?,status='GIT_DONE',updated_at=? WHERE id=?", mc, db.NowMS(), j.id)
		}
		if e = s.applyImportJournal(ctx, j.id, j.pid, j.ver, p.CandidateSnapshotID, p.SHA256, result, mc); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) activeChangeDecisions(ctx context.Context, result, incoming domain.Snapshot) (map[int64]bool, error) {
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,operation FROM server_changes WHERE active=1 ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[int64]bool{}
	plR, plI := mapPL(result.Playlists), mapPL(incoming.Playlists)
	qR, qI := mapQ(result.Queues), mapQ(incoming.Queues)
	for rows.Next() {
		var id int64
		var typ, key, op string
		if e = rows.Scan(&id, &typ, &key, &op); e != nil {
			return nil, e
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
		default:
			active = true
		}
		out[id] = active
	}
	return out, rows.Err()
}
