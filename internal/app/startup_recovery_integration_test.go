//go:build integration

package app

import (
	"context"
	"testing"
)

func TestRecoverStartupReconcilesPendingServerChangeBeforeServing(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)

	r, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO server_changes(base_version_id,target_type,target_key,operation,before_json,after_json,git_commit,active,created_at) VALUES(?,?,?,?,?,?,NULL,1,1)", vid, "system", "startup-test", "STARTUP_PENDING", "null", `{"ok":true}`)
	if err != nil {
		t.Fatal(err)
	}
	cid, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.RecoverStartup(ctx); err != nil {
		t.Fatal(err)
	}
	var commit string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(git_commit,'') FROM server_changes WHERE id=?", cid).Scan(&commit); err != nil {
		t.Fatal(err)
	}
	if commit == "" {
		t.Fatal("startup recovery left pending Server Change without Git audit")
	}
	if head := svc.Git.Head("refs/heads/main"); head != commit {
		t.Fatalf("Git main head=%q want recovered commit=%q", head, commit)
	}
}
