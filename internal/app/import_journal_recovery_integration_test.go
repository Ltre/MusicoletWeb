//go:build integration

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
)

func TestRecoverCommitJournalRejectsUnrelatedAdvancedSourceHead(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)
	theirs := snapshotWithSong("/a.mp3", "PHONE", 2)
	pid := createCandidateProcedure(t, svc, vid, theirs)
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}

	sp, err := svc.Git.CommitJSON("refs/heads/musicolet-source", "base source", []byte(`{"base":"source"}`))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := svc.Git.CommitJSON("refs/heads/main", "base main", []byte(`{"base":"main"}`))
	if err != nil {
		t.Fatal(err)
	}
	result := theirs
	result.RawFiles = nil
	state, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	now := db.NowMS()
	if _, err = svc.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTING' WHERE id=?", pid); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO commit_journal(kind,procedure_id,target_version_no,state_json,source_parent,main_parent,status,created_at,updated_at) VALUES('IMPORT',?,2,?,?,?,'PREPARED',?,?)", pid, string(state), sp, mp, now, now)
	if err != nil {
		t.Fatal(err)
	}
	jid, _ := r.LastInsertId()
	if _, err = svc.Git.CommitJSON("refs/heads/musicolet-source", "unrelated", []byte(`{"wrong":true}`), sp); err != nil {
		t.Fatal(err)
	}

	err = svc.RecoverCommitJournals(ctx)
	if err == nil || !strings.Contains(err.Error(), "advanced unexpectedly") {
		t.Fatalf("unrelated source HEAD must be rejected, got %v", err)
	}
	var sourceCommit, status string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(source_commit,''),status FROM commit_journal WHERE id=?", jid).Scan(&sourceCommit, &status); err != nil {
		t.Fatal(err)
	}
	if sourceCommit != "" || status != "PREPARED" {
		t.Fatalf("journal incorrectly adopted unrelated commit: source=%q status=%s", sourceCommit, status)
	}
	var v2 int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM musicolet_versions WHERE version_no=2").Scan(&v2); err != nil {
		t.Fatal(err)
	}
	if v2 != 0 {
		t.Fatal("rejected recovery must not finalize V2")
	}
	_ = p
}

func TestRecoverCommitJournalAdoptsMatchingCrashCommitAndFinishes(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)
	theirs := snapshotWithSong("/a.mp3", "PHONE", 2)
	pid := createCandidateProcedure(t, svc, vid, theirs)
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}

	sp, err := svc.Git.CommitJSON("refs/heads/musicolet-source", "base source", []byte(`{"base":"source"}`))
	if err != nil {
		t.Fatal(err)
	}
	mp, err := svc.Git.CommitJSON("refs/heads/main", "base main", []byte(`{"base":"main"}`))
	if err != nil {
		t.Fatal(err)
	}
	result := theirs
	result.RawFiles = nil
	state, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	now := db.NowMS()
	if _, err = svc.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTING' WHERE id=?", pid); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO commit_journal(kind,procedure_id,target_version_no,state_json,source_parent,main_parent,status,created_at,updated_at) VALUES('IMPORT',?,2,?,?,?,'PREPARED',?,?)", pid, string(state), sp, mp, now, now)
	if err != nil {
		t.Fatal(err)
	}
	jid, _ := r.LastInsertId()

	expectedSource, err := svc.Git.CommitJSON("refs/heads/musicolet-source", "simulated crash after source commit", musicolet.CanonicalSnapshot(theirs), sp)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.RecoverCommitJournals(ctx); err != nil {
		t.Fatal(err)
	}
	var sourceCommit, mainCommit, journalStatus string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(source_commit,''),COALESCE(main_commit,''),status FROM commit_journal WHERE id=?", jid).Scan(&sourceCommit, &mainCommit, &journalStatus); err != nil {
		t.Fatal(err)
	}
	if sourceCommit != expectedSource || mainCommit == "" || journalStatus != "DONE" {
		t.Fatalf("matching crash commit was not recovered: source=%q main=%q status=%s", sourceCommit, mainCommit, journalStatus)
	}
	p, err = svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "COMMITTED" {
		t.Fatalf("recovered procedure status=%s", p.Status)
	}
	var v2 int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM musicolet_versions WHERE version_no=2").Scan(&v2); err != nil {
		t.Fatal(err)
	}
	if v2 != 1 {
		t.Fatalf("recovery must finalize exactly one V2, got %d", v2)
	}
}
