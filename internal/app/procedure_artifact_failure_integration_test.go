//go:build integration

package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCreateProcedureFailsIfOriginalZipArtifactCannotBeAudited(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	if _, err := svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER fail_original_artifact BEFORE INSERT ON import_artifacts WHEN NEW.kind='original_zip' BEGIN SELECT RAISE(ABORT,'forced original artifact failure'); END`); err != nil {
		t.Fatal(err)
	}

	pid, err := svc.CreateProcedure(ctx, minimalBackupZip(t))
	if err == nil || !strings.Contains(err.Error(), "forced original artifact failure") {
		t.Fatalf("expected original artifact audit failure, pid=%d err=%v", pid, err)
	}
	if pid == 0 {
		t.Fatal("failed audited upload should still retain its Procedure id")
	}
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "FAILED" {
		t.Fatalf("procedure status=%s", p.Status)
	}
	if p.ZipPath == "" {
		t.Fatal("archived ZIP path must be retained")
	}
	if _, err = os.Stat(p.ZipPath); err != nil {
		t.Fatalf("original ZIP should remain on disk for audit: %v", err)
	}
	var runs int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM parser_runs WHERE procedure_id=?", pid).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("parser must not start when original artifact audit failed: runs=%d", runs)
	}
	if active, err := svc.ActiveProcedure(ctx); err != nil || active != nil {
		t.Fatalf("FAILED Procedure must release active lock: active=%#v err=%v", active, err)
	}
}

func TestParseProcedureFailsIfDecryptedArtifactCannotBeAudited(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	if _, err := svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER fail_decrypted_artifact BEFORE INSERT ON import_artifacts WHEN NEW.kind='decrypted_dir' BEGIN SELECT RAISE(ABORT,'forced decrypted artifact failure'); END`); err != nil {
		t.Fatal(err)
	}

	pid, err := svc.CreateProcedure(ctx, minimalBackupZip(t))
	if err == nil || !strings.Contains(err.Error(), "forced decrypted artifact failure") {
		t.Fatalf("expected decrypted artifact audit failure, pid=%d err=%v", pid, err)
	}
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "FAILED" {
		t.Fatalf("procedure status=%s", p.Status)
	}
	if _, err = os.Stat(p.ZipPath); err != nil {
		t.Fatalf("original ZIP must survive parser audit failure: %v", err)
	}
	var original, decrypted int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_artifacts WHERE procedure_id=? AND kind='original_zip'", pid).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_artifacts WHERE procedure_id=? AND kind='decrypted_dir'", pid).Scan(&decrypted); err != nil {
		t.Fatal(err)
	}
	if original != 1 || decrypted != 0 {
		t.Fatalf("artifact audit mismatch: original=%d decrypted=%d", original, decrypted)
	}
	var status, errorText string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT status,COALESCE(error_text,'') FROM parser_runs WHERE procedure_id=? ORDER BY id DESC LIMIT 1", pid).Scan(&status, &errorText); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED" || !strings.Contains(errorText, "forced decrypted artifact failure") {
		t.Fatalf("parser run audit mismatch: status=%s error=%q", status, errorText)
	}
}
