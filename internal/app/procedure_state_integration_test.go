//go:build integration

package app

import (
	"context"
	"strings"
	"testing"
)

func TestProcedureResolutionTransitionsToReadyAndCancelledProcedureCannotResume(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)

	ours := base.Songs["/a.mp3"]
	ours.Title = "SERVER"
	if err := svc.UpdateMetadata(ctx, "/a.mp3", ours); err != nil {
		t.Fatal(err)
	}
	theirs := snapshotWithSong("/a.mp3", "PHONE", 1)
	pid := createCandidateProcedure(t, svc, vid, theirs)
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "RESOLVING" {
		t.Fatalf("expected RESOLVING, got %s", p.Status)
	}
	conflicts, err := svc.ListConflicts(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict, got %d", len(conflicts))
	}
	if err = svc.ResolveConflict(ctx, conflicts[0].ID, "OURS", nil); err != nil {
		t.Fatal(err)
	}
	p, err = svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "READY_TO_COMMIT" {
		t.Fatalf("last resolution must transition procedure to READY_TO_COMMIT, got %s", p.Status)
	}

	if err = svc.CancelProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	p, err = svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "CANCELLED" {
		t.Fatalf("cancel status=%s", p.Status)
	}
	if err = svc.CommitProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "cannot commit from status CANCELLED") {
		t.Fatalf("cancelled procedure must not commit: %v", err)
	}
	if err = svc.RefreshProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "cannot refresh from status CANCELLED") {
		t.Fatalf("cancelled procedure must not refresh: %v", err)
	}
	if err = svc.ResolveConflict(ctx, conflicts[0].ID, "THEIRS", nil); err == nil || !strings.Contains(err.Error(), "cannot resolve conflicts from status CANCELLED") {
		t.Fatalf("cancelled procedure must not accept new resolution: %v", err)
	}
	p, err = svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "CANCELLED" {
		t.Fatalf("terminal procedure was resurrected: %s", p.Status)
	}
}

func TestProcedureCannotCommitWhileConflictsRemain(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "BASE", 1)
	vid := installVersion(t, svc, base)
	ours := base.Songs["/a.mp3"]
	ours.Title = "SERVER"
	if err := svc.UpdateMetadata(ctx, "/a.mp3", ours); err != nil {
		t.Fatal(err)
	}
	pid := createCandidateProcedure(t, svc, vid, snapshotWithSong("/a.mp3", "PHONE", 1))
	if err := svc.CommitProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "cannot commit from status RESOLVING") {
		t.Fatalf("RESOLVING procedure must not commit: %v", err)
	}
}
