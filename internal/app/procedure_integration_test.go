//go:build integration

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
)

func TestActiveProcedureRejectsSecondZipAndCancelKeepsArtifacts(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	z := minimalBackupZip(t)
	pid, err := svc.CreateProcedure(ctx, z)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateProcedure(ctx, z); err == nil || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("second active procedure should be rejected: %v", err)
	}
	var before int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_artifacts WHERE procedure_id=?", pid).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("expected archived import artifacts")
	}
	if err = svc.CancelProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "CANCELLED" {
		t.Fatalf("status=%s", p.Status)
	}
	var after int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_artifacts WHERE procedure_id=?", pid).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("cancel removed artifacts: before=%d after=%d", before, after)
	}
	if _, err = svc.CreateProcedure(ctx, z); err != nil {
		t.Fatalf("cancelled procedure must release upload lock: %v", err)
	}
}

func TestCommitRejectsChangedServerHead(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "A", 10)
	vid := installVersion(t, svc, base)
	pid := createCandidateProcedure(t, svc, vid, base)
	if err := svc.SetFavorite(ctx, "/a.mp3", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.CommitProcedure(ctx, pid); err == nil || !strings.Contains(err.Error(), "server state changed") {
		t.Fatalf("expected HEAD change rejection, got %v", err)
	}
}

func TestResolvedConflictBecomesStaleAfterNewM(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "A", 10)
	vid := installVersion(t, svc, base)
	ours := base.Songs["/a.mp3"]
	ours.Title = "SERVER-B"
	if err := svc.UpdateMetadata(ctx, "/a.mp3", ours); err != nil {
		t.Fatal(err)
	}
	theirs := snapshotWithSong("/a.mp3", "PHONE-C", 10)
	pid := createCandidateProcedure(t, svc, vid, theirs)
	cs, err := svc.ListConflicts(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Status != "UNRESOLVED" {
		t.Fatalf("initial conflicts=%#v", cs)
	}
	if err = svc.ResolveConflict(ctx, cs[0].ID, "OURS", nil); err != nil {
		t.Fatal(err)
	}
	latest := ours
	latest.Title = "SERVER-D"
	if err = svc.UpdateMetadata(ctx, "/a.mp3", latest); err != nil {
		t.Fatal(err)
	}
	if err = svc.RefreshProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	cs, err = svc.ListConflicts(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Status != "STALE" {
		t.Fatalf("resolved conflict should become STALE: %#v", cs)
	}
}

func TestServerDeleteSurvivesPhonePlayCountOnlyChange(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/a.mp3", "A", 10)
	base.CurrentCounts["/a.mp3"] = domain.CurrentCounts{Week: 2, Month: 3, Year: 4}
	vid := installVersion(t, svc, base)
	if err := svc.DeleteSong(ctx, "/a.mp3"); err != nil {
		t.Fatal(err)
	}
	theirs := snapshotWithSong("/a.mp3", "A", 11)
	theirs.CurrentCounts["/a.mp3"] = domain.CurrentCounts{Week: 3, Month: 4, Year: 5}
	pid := createCandidateProcedure(t, svc, vid, theirs)
	cs, err := svc.ListConflicts(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatalf("play-count-only source change must not conflict with server delete: %#v", cs)
	}
	if err = svc.CommitProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	working, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := working.Songs["/a.mp3"]; ok {
		t.Fatal("server-deleted song was resurrected by play-count change")
	}
	var active int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes WHERE target_type='song' AND target_key='/a.mp3' AND operation='DELETE' AND active=1").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active == 0 {
		t.Fatal("server DELETE M must remain active")
	}
}

func TestPhonePathChangeIsDeleteAndAdd(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := snapshotWithSong("/old/a.mp3", "A", 1)
	vid := installVersion(t, svc, base)
	theirs := snapshotWithSong("/new/a.mp3", "A", 1)
	pid := createCandidateProcedure(t, svc, vid, theirs)
	cs, err := svc.ListConflicts(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatalf("path delete+add should not conflict: %#v", cs)
	}
	diffs, err := svc.ListDiffs(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]string{}
	for _, d := range diffs {
		if d.TargetType == "song" {
			ops[d.TargetKey] = d.Operation
		}
	}
	if ops["/old/a.mp3"] != "DELETE" || ops["/new/a.mp3"] != "ADD" {
		t.Fatalf("path change was not represented as DELETE+ADD: %#v", ops)
	}
}

func newIntegrationService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	st, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	g, err := gitstore.Open(filepath.Join(root, "git", "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	return New(st, g, root)
}

func snapshotWithSong(path, title string, count int64) domain.Snapshot {
	s := domain.EmptySnapshot()
	s.Songs[path] = domain.Song{Path: path, Title: title, Artist: "Artist", Album: "Album", DurationMS: 123000, PlayCount: count}
	return s
}

func installVersion(t *testing.T, svc *Service, snap domain.Snapshot) int64 {
	t.Helper()
	ctx := context.Background()
	sid, err := svc.saveSnapshot(ctx, 0, "VERSION", snap)
	if err != nil {
		t.Fatal(err)
	}
	var vid int64
	err = svc.Store.Tx(ctx, func(tx *sql.Tx) error {
		if err := replaceWorking(ctx, tx, snap); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, "INSERT INTO musicolet_versions(version_no,snapshot_id,source_zip_sha256,created_at) VALUES(1,?,'fixture',?)", sid, db.NowMS())
		if err != nil {
			return err
		}
		vid, err = r.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return vid
}

func createCandidateProcedure(t *testing.T, svc *Service, baseVersionID int64, snap domain.Snapshot) int64 {
	t.Helper()
	ctx := context.Background()
	now := db.NowMS()
	r, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO import_procedures(status,base_version_id,source_zip_path,source_zip_sha256,created_at,updated_at) VALUES('REVIEWING',?,'fixture.zip','fixture',?,?)", baseVersionID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := r.LastInsertId()
	sid, err := svc.saveSnapshot(ctx, pid, "CANDIDATE", snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Store.DB.ExecContext(ctx, "UPDATE import_procedures SET candidate_snapshot_id=? WHERE id=?", sid, pid); err != nil {
		t.Fatal(err)
	}
	if err = svc.AnalyzeProcedure(ctx, pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

func minimalBackupZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("0.favs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(`{"S_P":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
