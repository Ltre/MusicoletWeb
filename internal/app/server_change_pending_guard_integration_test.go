//go:build integration

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestPendingGitAuditBlocksSubsequentServerMutation(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A"}
	_ = installVersion(t, svc, snap)

	goodGitDir := svc.Git.Dir
	svc.Git.Dir = filepath.Join(t.TempDir(), "missing-history.git")
	if err := svc.SetFavorite(ctx, "/a.mp3", true); err == nil {
		t.Fatal("expected first mutation Git finalization failure")
	}
	meta := snap.Songs["/a.mp3"]
	meta.Title = "B"
	if err := svc.UpdateMetadata(ctx, "/a.mp3", meta); err == nil || !strings.Contains(err.Error(), "pending Git audit") {
		t.Fatalf("second Server M must be blocked while audit is pending: %v", err)
	}
	var title string
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT title FROM working_songs WHERE path='/a.mp3'").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "A" {
		t.Fatalf("blocked mutation changed Working State: %q", title)
	}
	var changes int
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("blocked mutation created extra Server Change rows: %d", changes)
	}

	svc.Git.Dir = goodGitDir
	if err := svc.ReconcileGit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateMetadata(ctx, "/a.mp3", meta); err != nil {
		t.Fatalf("mutation should resume after exact audit recovery: %v", err)
	}
}

func TestReconcileGitAdoptsCommitWrittenBeforeSQLiteLinkFailure(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A"}
	_ = installVersion(t, svc, snap)

	if _, err := svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER fail_git_link BEFORE UPDATE OF git_commit ON server_changes WHEN NEW.git_commit IS NOT NULL BEGIN SELECT RAISE(ABORT,'forced git link failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetFavorite(ctx, "/a.mp3", true); err == nil || !strings.Contains(err.Error(), "forced git link failure") {
		t.Fatalf("expected SQLite link failure after Git commit, got %v", err)
	}
	writtenHead := svc.Git.Head("refs/heads/main")
	if writtenHead == "" {
		t.Fatal("Git commit should already exist before SQLite link failure")
	}
	if _, err := svc.Store.DB.ExecContext(ctx, "DROP TRIGGER fail_git_link"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileGit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := svc.Git.Head("refs/heads/main"); got != writtenHead {
		t.Fatalf("reconcile created a duplicate Git commit: before=%s after=%s", writtenHead, got)
	}
	var commit string
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(git_commit,'') FROM server_changes LIMIT 1").Scan(&commit); err != nil {
		t.Fatal(err)
	}
	if commit != writtenHead {
		t.Fatalf("pending audit did not adopt existing Git commit: %q", commit)
	}
}

func TestReconcileGitRejectsMultipleLegacyPendingServerChanges(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A"}
	vid := installVersion(t, svc, snap)
	for _, op := range []string{"LEGACY_ONE", "LEGACY_TWO"} {
		if _, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,git_commit,active,created_at) VALUES(?,?,?,?,NULL,1,1)", vid, "system", op, op); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ReconcileGit(ctx); err == nil || !strings.Contains(err.Error(), "exact intermediate Working States") {
		t.Fatalf("multiple legacy pending audits must fail closed: %v", err)
	}
}
