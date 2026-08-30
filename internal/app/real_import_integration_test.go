//go:build integration

package app

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
)

func TestRealImportProcedureV1ServerMV2(t *testing.T) {
	basePath := os.Getenv("MUSICOLET_REAL_BASE_BACKUP")
	incomingPath := os.Getenv("MUSICOLET_REAL_BACKUP")
	if basePath == "" || incomingPath == "" {
		t.Skip("set MUSICOLET_REAL_BASE_BACKUP and MUSICOLET_REAL_BACKUP")
	}
	ctx := context.Background()
	root := t.TempDir()
	st, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	git, err := gitstore.Open(filepath.Join(root, "git", "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, git, root)

	baseBytes, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	pid1, err := svc.CreateProcedure(ctx, baseBytes)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := svc.GetProcedure(ctx, pid1)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Status != "READY_TO_COMMIT" {
		t.Fatalf("V1 procedure status=%s", p1.Status)
	}
	if err = svc.CommitProcedure(ctx, pid1); err != nil {
		t.Fatal(err)
	}
	_, _, ver, err := st.LatestVersion(ctx)
	if err != nil || ver != 1 {
		t.Fatalf("V1 version=%d err=%v", ver, err)
	}
	workingV1, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(workingV1.Songs) != 6652 {
		t.Fatalf("V1 songs=%d", len(workingV1.Songs))
	}
	playV1, err := svc.Playback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if playV1.QueueName != "至喜²｜H↑" {
		t.Fatalf("V1 active queue=%q", playV1.QueueName)
	}
	oldQueueID := playV1.QueueID

	baseSnap, err := (musicolet.Parser{}).ParseZip(ctx, basePath, filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatal(err)
	}
	incomingSnap, err := (musicolet.Parser{}).ParseZip(ctx, incomingPath, filepath.Join(t.TempDir(), "incoming"))
	if err != nil {
		t.Fatal(err)
	}
	stablePath := stableRealSongPath(baseSnap, incomingSnap)
	if stablePath == "" {
		t.Fatal("no stable song found")
	}
	before := workingV1.Songs[stablePath]
	edited := before
	edited.Title = before.Title + " [server-M]"
	if err = svc.UpdateMetadata(ctx, stablePath, edited); err != nil {
		t.Fatal(err)
	}
	if err = svc.IncrementPlay(ctx, stablePath, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	serverBeforeV2, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	incomingBytes, err := os.ReadFile(incomingPath)
	if err != nil {
		t.Fatal(err)
	}
	pid2, err := svc.CreateProcedure(ctx, incomingBytes)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := svc.GetProcedure(ctx, pid2)
	if err != nil {
		t.Fatal(err)
	}
	conflicts, err := svc.ListConflicts(ctx, pid2)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected zero V2 conflicts, got %#v", conflicts)
	}
	if p2.Status != "READY_TO_COMMIT" {
		t.Fatalf("V2 procedure status=%s", p2.Status)
	}

	expectedCount := serverBeforeV2.Songs[stablePath].PlayCount + (incomingSnap.Songs[stablePath].PlayCount - baseSnap.Songs[stablePath].PlayCount)
	if err = svc.CommitProcedure(ctx, pid2); err != nil {
		t.Fatal(err)
	}
	_, _, ver, err = st.LatestVersion(ctx)
	if err != nil || ver != 2 {
		t.Fatalf("V2 version=%d err=%v", ver, err)
	}
	final, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := final.Songs[stablePath]
	if got.Title != edited.Title {
		t.Fatalf("server metadata M lost: got=%q want=%q", got.Title, edited.Title)
	}
	if got.PlayCount != expectedCount {
		t.Fatalf("play count merge=%d want=%d", got.PlayCount, expectedCount)
	}
	var metadataActive, playActive int
	if err = st.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes WHERE target_type='song' AND target_key=? AND operation='METADATA' AND active=1", stablePath).Scan(&metadataActive); err != nil {
		t.Fatal(err)
	}
	if err = st.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes WHERE target_type='song' AND target_key=? AND operation='PLAY' AND active=1", stablePath).Scan(&playActive); err != nil {
		t.Fatal(err)
	}
	if metadataActive == 0 {
		t.Fatal("metadata M should remain active after unchanged phone Song Core")
	}
	if playActive != 0 {
		t.Fatal("PLAY M should be settled after successful import")
	}
	playV2, err := svc.Playback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if playV2.QueueName != "至喜²｜H↑" {
		t.Fatalf("V2 active queue=%q", playV2.QueueName)
	}
	if playV2.QueueID == oldQueueID {
		t.Fatalf("queue ID was not rebuilt; remap test ineffective: %d", oldQueueID)
	}
}

func stableRealSongPath(base, incoming domain.Snapshot) string {
	keys := make([]string, 0, len(base.Songs))
	for p := range base.Songs {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		b := base.Songs[p]
		n, ok := incoming.Songs[p]
		if !ok || b.CoreKey() != n.CoreKey() {
			continue
		}
		if b.PlayCount != n.PlayCount || b.LastPlayedMS != n.LastPlayedMS || base.CurrentCounts[p] != incoming.CurrentCounts[p] {
			continue
		}
		return p
	}
	return ""
}
