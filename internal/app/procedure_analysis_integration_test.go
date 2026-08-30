//go:build integration

package app

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func TestAnalyzeProcedureRejectsTerminalStatus(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)
	pid := createCandidateProcedure(t, svc, vid, base)
	if err := svc.CancelProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if err := svc.AnalyzeProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "cannot analyze from status CANCELLED") {
		t.Fatalf("terminal Procedure must not be re-analyzed: %v", err)
	}
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "CANCELLED" {
		t.Fatalf("terminal Procedure was mutated by direct Analyze: %s", p.Status)
	}
}

func TestAnalyzeProcedureRollsBackDiffRefreshIfStatusPersistenceFails(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)
	incoming := snapshotWithSong("/a.mp3", "PHONE", 1)
	pid := createCandidateProcedure(t, svc, vid, incoming)

	before, err := svc.ListDiffs(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.SetFavorite(ctx, "/a.mp3", true); err != nil {
		t.Fatal(err)
	}
	trigger := `CREATE TRIGGER fail_analysis_status BEFORE UPDATE ON import_procedures WHEN NEW.id=` + strconv.FormatInt(pid, 10) + ` AND NEW.last_analyzed_server_head>OLD.last_analyzed_server_head BEGIN SELECT RAISE(ABORT,'forced analysis status failure'); END`
	if _, err = svc.Store.DB.ExecContext(ctx, trigger); err != nil {
		t.Fatal(err)
	}
	if err = svc.AnalyzeProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "forced analysis status failure") {
		t.Fatalf("expected analysis status persistence failure, got %v", err)
	}
	after, err := svc.ListDiffs(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("failed analysis leaked partial diff refresh: before=%d after=%d", len(before), len(after))
	}
}
