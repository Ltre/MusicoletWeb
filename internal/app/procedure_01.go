package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/merge"
)

func (s *Service) CreateProcedure(ctx context.Context, zipData []byte) (int64, error) {
	if id, err := s.Store.ActiveProcedure(ctx); err != nil {
		return 0, err
	} else if id != 0 {
		return 0, fmt.Errorf("procedure %d is still active", id)
	}

	h := sha256.Sum256(zipData)
	sha := hex.EncodeToString(h[:])
	now := db.NowMS()
	var id int64
	if err := s.Store.Tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, "INSERT INTO import_procedures(status,source_zip_path,source_zip_sha256,created_at,updated_at) VALUES('PARSING','',?,?,?)", sha, now, now)
		if err != nil {
			return err
		}
		id, err = r.LastInsertId()
		return err
	}); err != nil {
		return 0, err
	}

	dir := filepath.Join(s.DataDir, "imports", fmt.Sprintf("procedure-%06d", id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.failProcedure(ctx, id, err)
		return id, err
	}
	p := filepath.Join(dir, "original.zip")
	if err := os.WriteFile(p, zipData, 0o600); err != nil {
		s.failProcedure(ctx, id, err)
		return id, err
	}

	r, err := s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET source_zip_path=? WHERE id=?", p, id)
	if err != nil {
		s.failProcedure(ctx, id, err)
		return id, err
	}
	if err = db.CheckAffected(r); err != nil {
		s.failProcedure(ctx, id, err)
		return id, err
	}
	if _, err = s.Store.DB.ExecContext(ctx, "INSERT INTO import_artifacts(procedure_id,kind,path,sha256,created_at) VALUES(?,?,?,?,?)", id, "original_zip", p, sha, now); err != nil {
		s.failProcedure(ctx, id, err)
		return id, err
	}

	if err = s.ParseProcedure(ctx, id); err != nil {
		return id, err
	}
	return id, nil
}

func (s *Service) ParseProcedure(ctx context.Context, id int64) (err error) {
	p, err := s.GetProcedure(ctx, id)
	if err != nil {
		return err
	}
	started := db.NowMS()
	r, err := s.Store.DB.ExecContext(ctx, "INSERT INTO parser_runs(procedure_id,parser_version,status,started_at) VALUES(?,?,'RUNNING',?)", id, ParserVersion, started)
	if err != nil {
		return err
	}
	runID, err := r.LastInsertId()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.failProcedure(ctx, id, err)
			_, _ = s.Store.DB.ExecContext(ctx, "UPDATE parser_runs SET status='FAILED',error_text=?,finished_at=? WHERE id=?", err.Error(), db.NowMS(), runID)
		}
	}()

	work := filepath.Dir(p.ZipPath)
	snap, report, err := s.Parser.ParseZipWithReport(ctx, p.ZipPath, work)
	if err != nil {
		return err
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	decryptedDir := filepath.Join(work, "decrypted")
	if _, err = s.Store.DB.ExecContext(ctx, "INSERT INTO import_artifacts(procedure_id,kind,path,sha256,created_at) VALUES(?, 'decrypted_dir', ?, NULL, ?)", id, decryptedDir, db.NowMS()); err != nil {
		return err
	}
	r, err = s.Store.DB.ExecContext(ctx, "UPDATE parser_runs SET status='SUCCEEDED',report_json=?,finished_at=? WHERE id=?", string(reportJSON), db.NowMS(), runID)
	if err != nil {
		return err
	}
	if err = db.CheckAffected(r); err != nil {
		return err
	}
	sid, err := s.saveSnapshot(ctx, id, "CANDIDATE", snap)
	if err != nil {
		return err
	}
	r, err = s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET candidate_snapshot_id=?,status='REVIEWING',updated_at=? WHERE id=?", sid, db.NowMS(), id)
	if err != nil {
		return err
	}
	if err = db.CheckAffected(r); err != nil {
		return err
	}
	return s.AnalyzeProcedure(ctx, id)
}

func (s *Service) failProcedure(ctx context.Context, id int64, cause error) {
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='FAILED',updated_at=? WHERE id=? AND status<>'COMMITTED'", db.NowMS(), id)
}

func (s *Service) GetProcedure(ctx context.Context, id int64) (Procedure, error) {
	var p Procedure
	var base, cand sql.NullInt64
	err := s.Store.DB.QueryRowContext(ctx, "SELECT id,status,base_version_id,candidate_snapshot_id,source_zip_path,source_zip_sha256,last_analyzed_server_head FROM import_procedures WHERE id=?", id).Scan(&p.ID, &p.Status, &base, &cand, &p.ZipPath, &p.SHA256, &p.LastHead)
	if base.Valid {
		p.BaseVersionID = base.Int64
	}
	if cand.Valid {
		p.CandidateSnapshotID = cand.Int64
	}
	return p, err
}

func (s *Service) ActiveProcedure(ctx context.Context) (*Procedure, error) {
	id, err := s.Store.ActiveProcedure(ctx)
	if err != nil || id == 0 {
		return nil, err
	}
	p, err := s.GetProcedure(ctx, id)
	return &p, err
}

func (s *Service) AnalyzeProcedure(ctx context.Context, id int64) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	p, err := s.GetProcedure(ctx, id)
	if err != nil {
		return err
	}
	theirs, err := s.loadSnapshot(ctx, p.CandidateSnapshotID)
	if err != nil {
		return err
	}
	_, baseSID, _, err := s.Store.LatestVersion(ctx)
	if err != nil {
		return err
	}
	if baseSID == 0 {
		head, _ := s.Store.ServerHead(ctx)
		_, err = s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='READY_TO_COMMIT',last_analyzed_server_head=?,updated_at=? WHERE id=?", head, db.NowMS(), id)
		return err
	}
	base, err := s.loadSnapshot(ctx, baseSID)
	if err != nil {
		return err
	}
	ours, err := s.loadWorking(ctx)
	if err != nil {
		return err
	}
	head, _ := s.Store.ServerHead(ctx)
	oldRows, _ := s.ListConflicts(ctx, id)
	oldConflicts := map[string]ConflictRow{}
	stale := map[string]bool{}
	for _, c := range oldRows {
		k := c.TargetType + "\x00" + c.TargetKey
		oldConflicts[k] = c
		if c.Status == "RESOLVED" && c.ResolvedHead.Valid && head > c.ResolvedHead.Int64 {
			var hit int
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM server_changes WHERE id>? AND target_type=? AND target_key=?)", c.ResolvedHead.Int64, c.TargetType, c.TargetKey).Scan(&hit)
			stale[k] = hit != 0
		}
	}

	return s.Store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM merge_conflicts WHERE procedure_id=?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM semantic_diffs WHERE procedure_id=?", id); err != nil {
			return err
		}
		conf := 0
		for _, path := range unionSongKeys(base.Songs, ours.Songs, theirs.Songs) {
			b, o, t := ptr(base.Songs, path), ptr(ours.Songs, path), ptr(theirs.Songs, path)
			d := merge.MergeSong(b, o, t)
			op := songOp(b, t)
			isConf := d.Conflict != nil
			did, err := insertDiff(ctx, tx, id, "song", path, op, b, o, t, isConf)
			if err != nil {
				return err
			}
			if isConf {
				conf++
				if err = insertConflictPreserved(ctx, tx, id, did, "song", path, b, o, t, oldConflicts, stale); err != nil {
					return err
				}
			}
		}
		for _, typ := range []string{"playlist", "queue"} {
			bm, om, tm := listMap(base, typ), listMap(ours, typ), listMap(theirs, typ)
			for _, name := range unionListKeys(bm, om, tm) {
				b, o, t := bm[name], om[name], tm[name]
				r := merge.MergeOrdered(typ+":"+name, b, o, t)
				changed := !equal(b, t) || !equal(b, o)
				if !changed {
					continue
				}
				did, err := insertDiff(ctx, tx, id, typ, name, "MEMBERS", b, o, t, len(r.Conflicts) > 0)
				if err != nil {
					return err
				}
				if len(r.Conflicts) > 0 {
					conf++
					if err = insertConflictPreserved(ctx, tx, id, did, typ, name, b, o, t, oldConflicts, stale); err != nil {
						return err
					}
				}
			}
		}
		bq, oq, tq := queueNames(base.Queues), queueNames(ours.Queues), queueNames(theirs.Queues)
		if !equal(bq, oq) || !equal(bq, tq) {
			r := merge.MergeOrdered("queue-order", bq, oq, tq)
			did, err := insertDiff(ctx, tx, id, "queue_order", "all", "ORDER", bq, oq, tq, len(r.Conflicts) > 0)
			if err != nil {
				return err
			}
			if len(r.Conflicts) > 0 {
				conf++
				if err = insertConflictPreserved(ctx, tx, id, did, "queue_order", "all", bq, oq, tq, oldConflicts, stale); err != nil {
					return err
				}
			}
		}
		if err := analyzeFavorites(ctx, tx, id, base, ours, theirs, &conf); err != nil {
			return err
		}
		if err := analyzeCounts(ctx, tx, id, base, ours, theirs); err != nil {
			return err
		}
		if err := analyzeSettings(ctx, tx, id, base, ours, theirs); err != nil {
			return err
		}
		rawKeys := map[string]bool{}
		for k := range base.RawFiles {
			rawKeys[k] = true
		}
		for k := range theirs.RawFiles {
			rawKeys[k] = true
		}
		for k := range rawKeys {
			if base.RawFiles[k] != theirs.RawFiles[k] {
				if _, err := insertDiff(ctx, tx, id, "raw", k, "CHAR_DIFF", base.RawFiles[k], nil, theirs.RawFiles[k], false); err != nil {
					return err
				}
			}
		}
		status := "READY_TO_COMMIT"
		if conf > 0 {
			var unresolved int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts WHERE procedure_id=? AND status<>'RESOLVED'", id).Scan(&unresolved)
			if unresolved > 0 {
				status = "RESOLVING"
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE import_procedures SET base_version_id=(SELECT id FROM musicolet_versions ORDER BY version_no DESC LIMIT 1),status=?,last_analyzed_server_head=?,updated_at=? WHERE id=?", status, head, db.NowMS(), id)
		return err
	})
}
