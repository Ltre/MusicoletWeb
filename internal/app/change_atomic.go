package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Ltre/MusicoletWeb/internal/db"
)

// applyChangeLocked keeps the business mutation and its SQLite audit row in one
// transaction. Git is deliberately finalized after that transaction: a Git
// failure leaves a server_change with git_commit=NULL, which ReconcileGit can
// repair without losing the exact business operation/before/after payload.
// Caller must already hold s.mutation.
func (s *Service) applyChangeLocked(ctx context.Context, targetType, targetKey, operation string, before, after any, mutate func(*sql.Tx) error, targets ...[2]string) error {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	var cid int64
	err := s.Store.Tx(ctx, func(tx *sql.Tx) error {
		if mutate != nil {
			if err := mutate(tx); err != nil {
				return err
			}
		}
		var err error
		cid, err = insertServerChangeTx(ctx, tx, targetType, targetKey, operation, b, a, targets...)
		return err
	})
	if err != nil {
		return err
	}
	return s.finalizeServerChangeGitLocked(ctx, cid, operation)
}

func insertServerChangeTx(ctx context.Context, tx *sql.Tx, targetType, targetKey, operation string, before, after []byte, targets ...[2]string) (int64, error) {
	var baseVersionID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM musicolet_versions ORDER BY version_no DESC LIMIT 1").Scan(&baseVersionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("server changes require an imported Musicolet base version")
		}
		return 0, err
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,before_json,after_json,git_commit,active,created_at) VALUES(?,?,?,?,?,?,NULL,1,?)", baseVersionID, targetType, targetKey, operation, string(before), string(after), db.NowMS())
	if err != nil {
		return 0, err
	}
	cid, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	if cid == 0 {
		return 0, errors.New("server change insert returned no id")
	}
	all := append([][2]string{{targetType, targetKey}}, targets...)
	for _, target := range all {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO change_targets(change_id,target_type,target_key) VALUES(?,?,?)", cid, target[0], target[1]); err != nil {
			return 0, err
		}
		switch target[0] {
		case "song":
			if _, err = tx.ExecContext(ctx, "UPDATE working_songs SET has_server_changes=1 WHERE path=?", target[1]); err != nil {
				return 0, err
			}
		case "queue":
			if _, err = tx.ExecContext(ctx, "UPDATE working_queues SET has_server_changes=1 WHERE name=?", target[1]); err != nil {
				return 0, err
			}
		case "playlist":
			if _, err = tx.ExecContext(ctx, "UPDATE working_playlists SET has_server_changes=1 WHERE name=?", target[1]); err != nil {
				return 0, err
			}
		}
	}
	return cid, nil
}

func (s *Service) finalizeServerChangeGitLocked(ctx context.Context, cid int64, operation string) error {
	state, err := s.loadWorking(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	parent := s.Git.Head("refs/heads/main")
	commit, err := s.Git.CommitJSON("refs/heads/main", operation, payload, parent)
	if err != nil {
		return err
	}
	_, err = s.Store.DB.ExecContext(ctx, "UPDATE server_changes SET git_commit=? WHERE id=?", commit, cid)
	return err
}
