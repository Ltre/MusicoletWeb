package app

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/Ltre/MusicoletWeb/internal/db"
)

func (s *Service) recordChangeLocked(ctx context.Context, targetType, targetKey, operation string, before, after any, targets ...[2]string) error {
	return s.applyChangeLocked(ctx, targetType, targetKey, operation, before, after, nil, targets...)
}

func (s *Service) ReconcileGit(ctx context.Context) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,operation FROM server_changes WHERE git_commit IS NULL ORDER BY id")
	if err != nil {
		return err
	}
	type pending struct {
		id int64
		op string
	}
	var ps []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.op); err != nil {
			rows.Close()
			return err
		}
		ps = append(ps, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range ps {
		state, e := s.loadWorking(ctx)
		if e != nil {
			return e
		}
		b, _ := json.Marshal(state)
		parent := s.Git.Head("refs/heads/main")
		c, e := s.Git.CommitJSON("refs/heads/main", "reconcile: "+p.op, b, parent)
		if e != nil {
			return e
		}
		if _, e = s.Store.DB.ExecContext(ctx, "UPDATE server_changes SET git_commit=? WHERE id=?", c, p.id); e != nil {
			return e
		}
	}
	if _, _, vno, _ := s.Store.LatestVersion(ctx); vno > 0 {
		state, e := s.loadWorking(ctx)
		if e != nil {
			return e
		}
		current, _ := json.Marshal(state)
		gitState, e := s.Git.ReadState("refs/heads/main")
		if e == nil && !bytes.Equal(bytes.TrimSpace(gitState), bytes.TrimSpace(current)) {
			parent := s.Git.Head("refs/heads/main")
			c, e := s.Git.CommitJSON("refs/heads/main", "reconcile: unjournaled working-state drift", current, parent)
			if e != nil {
				return e
			}
			_, _ = s.Store.DB.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,before_json,after_json,git_commit,active,created_at) SELECT id,'system','working-state','RECOVER_WORKING_STATE',NULL,?,?,1,? FROM musicolet_versions WHERE version_no=?", string(current), c, db.NowMS(), vno)
		}
	}
	return nil
}
