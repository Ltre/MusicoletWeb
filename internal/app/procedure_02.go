package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
)

func (s *Service) ListParserRuns(ctx context.Context, id int64) ([]ParserRun, error) {
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,procedure_id,parser_version,status,COALESCE(report_json,'null'),COALESCE(error_text,''),started_at,finished_at FROM parser_runs WHERE procedure_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ParserRun
	for rows.Next() {
		var x ParserRun
		var report string
		if err = rows.Scan(&x.ID, &x.ProcedureID, &x.ParserVersion, &x.Status, &report, &x.Error, &x.StartedAt, &x.FinishedAt); err != nil {
			return nil, err
		}
		if json.Valid([]byte(report)) {
			x.Report = json.RawMessage(report)
		}
		if x.FinishedAt.Valid {
			v := x.FinishedAt.Int64
			x.FinishedAtMS = &v
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Service) ListDiffs(ctx context.Context, id int64) ([]Diff, error) {
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,operation,COALESCE(base_json,'null'),COALESCE(ours_json,'null'),COALESCE(theirs_json,'null'),conflict FROM semantic_diffs WHERE procedure_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Diff
	for rows.Next() {
		var d Diff
		var b, o, t string
		var c int
		if err = rows.Scan(&d.ID, &d.TargetType, &d.TargetKey, &d.Operation, &b, &o, &t, &c); err != nil {
			return nil, err
		}
		d.Base = []byte(b)
		d.Ours = []byte(o)
		d.Theirs = []byte(t)
		d.Conflict = c != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Service) ListConflicts(ctx context.Context, id int64) ([]ConflictRow, error) {
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,status,COALESCE(resolution,''),COALESCE(base_json,'null'),COALESCE(ours_json,'null'),COALESCE(theirs_json,'null'),COALESCE(manual_json,'null'),resolved_server_head FROM merge_conflicts WHERE procedure_id=? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConflictRow
	for rows.Next() {
		var c ConflictRow
		var b, o, t, m string
		if err = rows.Scan(&c.ID, &c.TargetType, &c.TargetKey, &c.Status, &c.Resolution, &b, &o, &t, &m, &c.ResolvedHead); err != nil {
			return nil, err
		}
		c.Base = []byte(b)
		c.Ours = []byte(o)
		c.Theirs = []byte(t)
		c.Manual = []byte(m)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) ResolveConflict(ctx context.Context, cid int64, res string, manual json.RawMessage) error {
	if res != "OURS" && res != "THEIRS" && res != "MANUAL" {
		return errors.New("invalid resolution")
	}
	if res == "MANUAL" && !json.Valid(manual) {
		return errors.New("manual resolution must be JSON")
	}
	head, err := s.Store.ServerHead(ctx)
	if err != nil {
		return err
	}
	return s.Store.Tx(ctx, func(tx *sql.Tx) error {
		pid, typ, key, base, ours, theirs, err := conflictForUpdate(ctx, tx, cid)
		if err != nil {
			return err
		}
		var status string
		var analyzedHead int64
		if err = tx.QueryRowContext(ctx, "SELECT status,last_analyzed_server_head FROM import_procedures WHERE id=?", pid).Scan(&status, &analyzedHead); err != nil {
			return err
		}
		if status != "RESOLVING" && status != "READY_TO_COMMIT" {
			return fmt.Errorf("procedure %d cannot resolve conflicts from status %s", pid, status)
		}
		if head != analyzedHead {
			return fmt.Errorf("server state changed; refresh procedure before resolving")
		}

		result := json.RawMessage(ours)
		if res == "THEIRS" {
			result = json.RawMessage(theirs)
		} else if res == "MANUAL" {
			result = manual
		}
		r, err := tx.ExecContext(ctx, "UPDATE merge_conflicts SET status='RESOLVED',resolution=?,manual_json=?,resolved_server_head=?,updated_at=? WHERE id=? AND procedure_id=?", res, string(manual), head, db.NowMS(), cid, pid)
		if err != nil {
			return err
		}
		if err = db.CheckAffected(r); err != nil {
			return err
		}
		r, err = tx.ExecContext(ctx, "INSERT INTO conflict_resolutions(conflict_id,procedure_id,target_type,target_key,resolution,server_head,base_json,ours_json,theirs_json,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", cid, pid, typ, key, res, head, base, ours, theirs, string(result), db.NowMS())
		if err != nil {
			return err
		}
		rid, err := r.LastInsertId()
		if err != nil {
			return err
		}
		patch := buildResolutionPatch(typ, json.RawMessage(ours), json.RawMessage(theirs), result)
		if _, err = tx.ExecContext(ctx, "INSERT INTO resolution_patches(resolution_id,patch_json,created_at) VALUES(?,?,?)", rid, string(patch), db.NowMS()); err != nil {
			return err
		}
		var unresolved int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts WHERE procedure_id=? AND status<>'RESOLVED'", pid).Scan(&unresolved); err != nil {
			return err
		}
		nextStatus := "RESOLVING"
		if unresolved == 0 {
			nextStatus = "READY_TO_COMMIT"
		}
		r, err = tx.ExecContext(ctx, "UPDATE import_procedures SET status=?,updated_at=? WHERE id=? AND status IN ('RESOLVING','READY_TO_COMMIT')", nextStatus, db.NowMS(), pid)
		if err != nil {
			return err
		}
		return db.CheckAffected(r)
	})
}

func (s *Service) RefreshProcedure(ctx context.Context, id int64) error {
	p, err := s.GetProcedure(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != "REVIEWING" && p.Status != "RESOLVING" && p.Status != "READY_TO_COMMIT" {
		return fmt.Errorf("procedure %d cannot refresh from status %s", id, p.Status)
	}
	head, err := s.Store.ServerHead(ctx)
	if err != nil {
		return err
	}
	if head == p.LastHead {
		return nil
	}
	return s.AnalyzeProcedure(ctx, id)
}

func (s *Service) CancelProcedure(ctx context.Context, id int64) error {
	p, err := s.GetProcedure(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != "PARSING" && p.Status != "REVIEWING" && p.Status != "RESOLVING" && p.Status != "READY_TO_COMMIT" {
		return fmt.Errorf("procedure %d cannot cancel from status %s", id, p.Status)
	}
	now := db.NowMS()
	r, err := s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='CANCELLED',cancelled_at=?,updated_at=? WHERE id=? AND status=?", now, now, id, p.Status)
	if err != nil {
		return err
	}
	return db.CheckAffected(r)
}

func (s *Service) CommitProcedure(ctx context.Context, id int64) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()

	p, err := s.GetProcedure(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != "READY_TO_COMMIT" {
		return fmt.Errorf("procedure %d cannot commit from status %s", id, p.Status)
	}
	head, err := s.Store.ServerHead(ctx)
	if err != nil {
		return err
	}
	if head != p.LastHead {
		return fmt.Errorf("server state changed; refresh procedure before commit")
	}
	var unresolved int
	if err = s.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts WHERE procedure_id=? AND status<>'RESOLVED'", id).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved > 0 {
		return fmt.Errorf("%d conflicts unresolved", unresolved)
	}

	theirs, err := s.loadSnapshot(ctx, p.CandidateSnapshotID)
	if err != nil {
		return err
	}
	_, baseSID, versionNo, err := s.Store.LatestVersion(ctx)
	if err != nil {
		return err
	}
	result := theirs
	if baseSID != 0 {
		base, err := s.loadSnapshot(ctx, baseSID)
		if err != nil {
			return err
		}
		ours, err := s.loadWorking(ctx)
		if err != nil {
			return err
		}
		result, err = s.resolveSnapshot(ctx, id, base, ours, theirs)
		if err != nil {
			return err
		}
	}
	result.RawFiles = nil
	stateJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	sourceParent := s.Git.Head("refs/heads/musicolet-source")
	mainParent := s.Git.Head("refs/heads/main")
	now := db.NowMS()
	var jid int64
	if err = s.Store.Tx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, "INSERT INTO commit_journal(kind,procedure_id,target_version_no,state_json,source_parent,main_parent,status,created_at,updated_at) VALUES('IMPORT',?,?,?,?,?,'PREPARED',?,?)", id, versionNo+1, string(stateJSON), sourceParent, mainParent, now, now)
		if err != nil {
			return err
		}
		jid, err = r.LastInsertId()
		if err != nil {
			return err
		}
		r, err = tx.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTING',updated_at=? WHERE id=? AND status='READY_TO_COMMIT'", now, id)
		if err != nil {
			return err
		}
		return db.CheckAffected(r)
	}); err != nil {
		return err
	}

	sourceCommit, err := s.Git.CommitJSON("refs/heads/musicolet-source", fmt.Sprintf("Musicolet V%d", versionNo+1), musicolet.CanonicalSnapshot(theirs), sourceParent)
	if err != nil {
		return err
	}
	r, err := s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET source_commit=?,status='SOURCE_DONE',updated_at=? WHERE id=?", sourceCommit, db.NowMS(), jid)
	if err != nil {
		return err
	}
	if err = db.CheckAffected(r); err != nil {
		return err
	}

	parents := []string{}
	if mainParent != "" {
		parents = append(parents, mainParent)
	}
	parents = append(parents, sourceCommit)
	mainCommit, err := s.Git.CommitJSON("refs/heads/main", fmt.Sprintf("Import Musicolet V%d", versionNo+1), stateJSON, parents...)
	if err != nil {
		return err
	}
	r, err = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET main_commit=?,status='GIT_DONE',updated_at=? WHERE id=?", mainCommit, db.NowMS(), jid)
	if err != nil {
		return err
	}
	if err = db.CheckAffected(r); err != nil {
		return err
	}
	return s.applyImportJournal(ctx, jid, id, versionNo+1, p.CandidateSnapshotID, p.SHA256, result, mainCommit)
}
