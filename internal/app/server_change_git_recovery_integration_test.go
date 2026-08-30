//go:build integration

package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestServerChangeKeepsExactPendingAuditWhenGitTemporarilyFails(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A"}
	_ = installVersion(t, svc, snap)

	goodGitDir := svc.Git.Dir
	beforeHead := svc.Git.Head("refs/heads/main")
	// Point the adapter at a directory that is not a Git repository. The SQLite
	// transaction must already be durable when Git finalization fails.
	svc.Git.Dir = filepath.Join(t.TempDir(), "missing-history.git")
	if err := svc.SetFavorite(ctx, "/a.mp3", true); err == nil {
		t.Fatal("expected Git finalization failure")
	}

	var favorite, marked, active int
	var op, commit string
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_favorites WHERE path='/a.mp3')").Scan(&favorite); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_songs WHERE path='/a.mp3'").Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT operation,COALESCE(git_commit,''),active FROM server_changes ORDER BY id DESC LIMIT 1").Scan(&op, &commit, &active); err != nil {
		t.Fatal(err)
	}
	if favorite != 1 || marked != 1 || op != "FAVORITE" || commit != "" || active != 1 {
		t.Fatalf("pending exact audit mismatch: favorite=%d marked=%d op=%q commit=%q active=%d", favorite, marked, op, commit, active)
	}

	svc.Git.Dir = goodGitDir
	if err := svc.ReconcileGit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconcile should fill the exact pending change, not add a generic drift record: %d rows", count)
	}
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT operation,COALESCE(git_commit,'') FROM server_changes LIMIT 1").Scan(&op, &commit); err != nil {
		t.Fatal(err)
	}
	if op != "FAVORITE" || commit == "" {
		t.Fatalf("reconciled audit mismatch: op=%q commit=%q", op, commit)
	}
	if afterHead := svc.Git.Head("refs/heads/main"); afterHead == "" || afterHead == beforeHead {
		t.Fatalf("Git main did not advance after reconciliation: before=%q after=%q", beforeHead, afterHead)
	}
}
