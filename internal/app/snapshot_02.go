package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Ltre/MusicoletWeb/internal/db"
)

func (s *Service) recordChangeLocked(ctx context.Context, targetType, targetKey, operation string, before, after any, targets ...[2]string) error {
	return s.applyChangeLocked(ctx, targetType, targetKey, operation, before, after, nil, targets...)
}

func (s *Service) previousLinkedGitCommit(ctx context.Context, beforeChangeID int64) (string, error) {
	var commit string
	err := s.Store.DB.QueryRowContext(ctx, "SELECT git_commit FROM server_changes WHERE id<? AND git_commit IS NOT NULL AND git_commit<>'' ORDER BY id DESC LIMIT 1", beforeChangeID).Scan(&commit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commit, err
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
	if err = rows.Close(); err != nil {
		return err
	}
	if len(ps) > 1 {
		return fmt.Errorf("%d Server Changes have pending Git audit; exact intermediate Working States cannot be reconstructed safely", len(ps))
	}
	if len(ps) == 1 {
		p := ps[0]
		state, err := s.loadWorking(ctx)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(state)
		if err != nil {
			return err
		}
		knownParent, err := s.previousLinkedGitCommit(ctx, p.id)
		if err != nil {
			return err
		}
		head := s.Git.Head("refs/heads/main")
		commit := ""
		if head == knownParent {
			commit, err = s.Git.CommitJSON("refs/heads/main", "reconcile: "+p.op, payload, knownParent)
			if err != nil {
				return err
			}
		} else {
			if head == "" {
				return fmt.Errorf("Git main ref disappeared while Server Change %d expects parent %s", p.id, knownParent)
			}
			matches, err := s.Git.CommitMatches(head, payload, knownParent)
			if err != nil {
				return err
			}
			if !matches {
				return fmt.Errorf("Git main ref %s does not match pending Server Change %d state/parent; refusing lossy reconciliation", head, p.id)
			}
			commit = head
		}
		r, err := s.Store.DB.ExecContext(ctx, "UPDATE server_changes SET git_commit=? WHERE id=? AND git_commit IS NULL", commit, p.id)
		if err != nil {
			return err
		}
		if err = db.CheckAffected(r); err != nil {
			return err
		}
	}

	versionID, _, versionNo, err := s.Store.LatestVersion(ctx)
	if err != nil {
		return err
	}
	if versionNo <= 0 {
		return nil
	}
	state, err := s.loadWorking(ctx)
	if err != nil {
		return err
	}
	current, err := json.Marshal(state)
	if err != nil {
		return err
	}
	head := s.Git.Head("refs/heads/main")
	gitState, readErr := s.Git.ReadState("refs/heads/main")
	if readErr != nil && head != "" {
		return readErr
	}
	if readErr == nil && bytes.Equal(bytes.TrimSpace(gitState), bytes.TrimSpace(current)) {
		return nil
	}

	r, err := s.Store.DB.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,before_json,after_json,git_commit,active,created_at) VALUES(?,?,?,?,NULL,?,NULL,1,?)", versionID, "system", "working-state", "RECOVER_WORKING_STATE", string(current), db.NowMS())
	if err != nil {
		return err
	}
	cid, err := r.LastInsertId()
	if err != nil {
		return err
	}
	commit, err := s.Git.CommitJSON("refs/heads/main", "reconcile: unjournaled working-state drift", current, head)
	if err != nil {
		return err
	}
	r, err = s.Store.DB.ExecContext(ctx, "UPDATE server_changes SET git_commit=? WHERE id=? AND git_commit IS NULL", commit, cid)
	if err != nil {
		return err
	}
	return db.CheckAffected(r)
}
