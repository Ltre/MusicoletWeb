package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
)

func (s *Service) ListDiffs(ctx context.Context, id int64) ([]Diff, error) {
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,operation,COALESCE(base_json,'null'),COALESCE(ours_json,'null'),COALESCE(theirs_json,'null'),conflict FROM semantic_diffs WHERE procedure_id=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Diff
	for rows.Next() {
		var d Diff
		var b, o, t string
		var c int
		if e = rows.Scan(&d.ID, &d.TargetType, &d.TargetKey, &d.Operation, &b, &o, &t, &c); e != nil {
			return nil, e
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
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT id,target_type,target_key,status,COALESCE(resolution,''),COALESCE(base_json,'null'),COALESCE(ours_json,'null'),COALESCE(theirs_json,'null'),COALESCE(manual_json,'null'),resolved_server_head FROM merge_conflicts WHERE procedure_id=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ConflictRow
	for rows.Next() {
		var c ConflictRow
		var b, o, t, m string
		if e = rows.Scan(&c.ID, &c.TargetType, &c.TargetKey, &c.Status, &c.Resolution, &b, &o, &t, &m, &c.ResolvedHead); e != nil {
			return nil, e
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
	head, _ := s.Store.ServerHead(ctx)
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE merge_conflicts SET status='RESOLVED',resolution=?,manual_json=?,resolved_server_head=?,updated_at=? WHERE id=?", res, string(manual), head, db.NowMS(), cid)
	return e
}

func (s *Service) RefreshProcedure(ctx context.Context, id int64) error {
	p, e := s.GetProcedure(ctx, id)
	if e != nil {
		return e
	}
	head, _ := s.Store.ServerHead(ctx)
	if head == p.LastHead {
		return nil
	}
	return s.AnalyzeProcedure(ctx, id)
}

func (s *Service) CancelProcedure(ctx context.Context, id int64) error {
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='CANCELLED',cancelled_at=?,updated_at=? WHERE id=? AND status NOT IN ('COMMITTED','CANCELLED')", db.NowMS(), db.NowMS(), id)
	return e
}

func (s *Service) CommitProcedure(ctx context.Context, id int64) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	p, e := s.GetProcedure(ctx, id)
	if e != nil {
		return e
	}
	head, _ := s.Store.ServerHead(ctx)
	if head != p.LastHead {
		return fmt.Errorf("server state changed; refresh procedure before commit")
	}
	var unresolved int
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts WHERE procedure_id=? AND status<>'RESOLVED'", id).Scan(&unresolved); e != nil {
		return e
	}
	if unresolved > 0 {
		return fmt.Errorf("%d conflicts unresolved", unresolved)
	}
	theirs, e := s.loadSnapshot(ctx, p.CandidateSnapshotID)
	if e != nil {
		return e
	}
	_, baseSID, versionNo, e := s.Store.LatestVersion(ctx)
	if e != nil {
		return e
	}
	result := theirs
	if baseSID != 0 {
		base, _ := s.loadSnapshot(ctx, baseSID)
		ours, _ := s.loadWorking(ctx)
		result, e = s.resolveSnapshot(ctx, id, base, ours, theirs)
		if e != nil {
			return e
		}
	}
	result.RawFiles = nil
	stateJSON, _ := json.Marshal(result)
	sourceParent := s.Git.Head("refs/heads/musicolet-source")
	mainParent := s.Git.Head("refs/heads/main")
	now := db.NowMS()
	r, e := s.Store.DB.ExecContext(ctx, "INSERT INTO commit_journal(kind,procedure_id,target_version_no,state_json,source_parent,main_parent,status,created_at,updated_at) VALUES('IMPORT',?,?,?,?,?,'PREPARED',?,?)", id, versionNo+1, string(stateJSON), sourceParent, mainParent, now, now)
	if e != nil {
		return e
	}
	jid, _ := r.LastInsertId()
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTING',updated_at=? WHERE id=?", now, id)
	sourceCommit, e := s.Git.CommitJSON("refs/heads/musicolet-source", fmt.Sprintf("Musicolet V%d", versionNo+1), musicolet.CanonicalSnapshot(theirs), sourceParent)
	if e != nil {
		return e
	}
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET source_commit=?,status='SOURCE_DONE',updated_at=? WHERE id=?", sourceCommit, db.NowMS(), jid)
	parents := []string{}
	if mainParent != "" {
		parents = append(parents, mainParent)
	}
	parents = append(parents, sourceCommit)
	mainCommit, e := s.Git.CommitJSON("refs/heads/main", fmt.Sprintf("Import Musicolet V%d", versionNo+1), stateJSON, parents...)
	if e != nil {
		return e
	}
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE commit_journal SET main_commit=?,status='GIT_DONE',updated_at=? WHERE id=?", mainCommit, db.NowMS(), jid)
	return s.applyImportJournal(ctx, jid, id, versionNo+1, p.CandidateSnapshotID, p.SHA256, result, mainCommit)
}
