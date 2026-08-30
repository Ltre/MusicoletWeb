//go:build integration

package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestApplyImportJournalRollsBackEntireSQLiteFinalizeOnAuditFailure(t *testing.T) {
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

	result := domain.EmptySnapshot()
	result.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "MERGED", Artist: "Artist", Album: "Album", DurationMS: 123000, PlayCount: 2}
	stateJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	now := db.NowMS()
	r, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO commit_journal(kind,procedure_id,target_version_no,state_json,status,created_at,updated_at) VALUES('IMPORT',?,2,?,'GIT_DONE',?,?)", pid, string(stateJSON), now, now)
	if err != nil {
		t.Fatal(err)
	}
	jid, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER fail_import_audit BEFORE INSERT ON server_changes WHEN NEW.operation='IMPORT_V2' BEGIN SELECT RAISE(ABORT,'forced import audit failure'); END`); err != nil {
		t.Fatal(err)
	}

	err = svc.applyImportJournal(ctx, jid, pid, 2, p.CandidateSnapshotID, p.SHA256, result, "fake-main-commit")
	if err == nil || !strings.Contains(err.Error(), "forced import audit failure") {
		t.Fatalf("expected forced finalize failure, got %v", err)
	}

	working, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := working.Songs["/a.mp3"].Title; got != "BASE" {
		t.Fatalf("working state leaked partial import: %q", got)
	}

	var versions int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM musicolet_versions WHERE version_no=2").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 0 {
		t.Fatalf("partial Musicolet V2 leaked after rollback: %d", versions)
	}

	var snapshotState string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT state FROM snapshots WHERE id=?", p.CandidateSnapshotID).Scan(&snapshotState); err != nil {
		t.Fatal(err)
	}
	if snapshotState != "CANDIDATE" {
		t.Fatalf("candidate snapshot was promoted despite rollback: %q", snapshotState)
	}

	var procedureStatus, journalStatus string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT status FROM import_procedures WHERE id=?", pid).Scan(&procedureStatus); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT status FROM commit_journal WHERE id=?", jid).Scan(&journalStatus); err != nil {
		t.Fatal(err)
	}
	if procedureStatus == "COMMITTED" || journalStatus == "DONE" {
		t.Fatalf("failed finalize was marked complete: procedure=%s journal=%s", procedureStatus, journalStatus)
	}
}
