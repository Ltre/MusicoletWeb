//go:build integration

package app

import (
	"context"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestSourceQueueIdentityAndReuse(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	for _, p := range []string{"/a.mp3", "/b.mp3", "/c.mp3"} {
		snap.Songs[p] = domain.Song{Path: p, Title: p}
	}
	_ = installVersion(t, svc, snap)

	albumQ, err := svc.EnsureSourceQueue(ctx, "album", "album:AAA", "AAA", []string{"/a.mp3", "/b.mp3"}, "/a.mp3", false)
	if err != nil {
		t.Fatal(err)
	}
	playlistQ, err := svc.EnsureSourceQueue(ctx, "playlist", "playlist:AAA", "AAA", []string{"/b.mp3", "/c.mp3"}, "/b.mp3", false)
	if err != nil {
		t.Fatal(err)
	}
	if albumQ == playlistQ {
		t.Fatal("different source objects must not share one queue")
	}
	var albumName, playlistName string
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", albumQ).Scan(&albumName); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", playlistQ).Scan(&playlistName); err != nil {
		t.Fatal(err)
	}
	if albumName != "AAA" || playlistName != "AAA #2" {
		t.Fatalf("queue naming collision mismatch: album=%q playlist=%q", albumName, playlistName)
	}

	again, err := svc.EnsureSourceQueue(ctx, "album", "album:AAA", "AAA", []string{"/c.mp3", "/a.mp3"}, "/a.mp3", true)
	if err != nil {
		t.Fatal(err)
	}
	if again != albumQ {
		t.Fatalf("same source must reuse queue: got %d want %d", again, albumQ)
	}
	got := queuePaths(ctx, svc.Store.DB, albumQ)
	if !equal(got, []string{"/a.mp3", "/b.mp3"}) {
		t.Fatalf("reused source queue must keep existing order/content, got %v", got)
	}
}

func TestQueueMoveNoDuplicateDeleteSwitchesRememberedStateAndStopTargetFollowsPath(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	for _, p := range []string{"/a.mp3", "/b.mp3", "/c.mp3"} {
		snap.Songs[p] = domain.Song{Path: p, Title: p}
	}
	_ = installVersion(t, svc, snap)

	q1, err := svc.EnsureSourceQueue(ctx, "album", "one", "One", []string{"/a.mp3", "/b.mp3"}, "/a.mp3", false)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := svc.EnsureSourceQueue(ctx, "playlist", "two", "Two", []string{"/b.mp3", "/c.mp3"}, "/b.mp3", false)
	if err != nil {
		t.Fatal(err)
	}

	// Existing members are moved, never duplicated.
	if err = svc.QueueAdd(ctx, q1, "/a.mp3", false); err != nil {
		t.Fatal(err)
	}
	if got := queuePaths(ctx, svc.Store.DB, q1); !equal(got, []string{"/b.mp3", "/a.mp3"}) {
		t.Fatalf("existing queue member should move to end: %v", got)
	}
	if err = svc.QueueAdd(ctx, q1, "/b.mp3", true); err != nil {
		t.Fatal(err)
	}
	if got := queuePaths(ctx, svc.Store.DB, q1); !equal(got, []string{"/a.mp3", "/b.mp3"}) {
		t.Fatalf("play-next should move existing member after current: %v", got)
	}

	// Remember an independent position in q2, switch back to q1, then delete q1.
	if err = svc.SetPlayback(ctx, q2, "/b.mp3", 7777, true); err != nil {
		t.Fatal(err)
	}
	if err = svc.SetPlayback(ctx, q1, "/a.mp3", 3210, true); err != nil {
		t.Fatal(err)
	}
	if err = svc.DeleteQueue(ctx, q1); err != nil {
		t.Fatal(err)
	}
	pv, err := svc.Playback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pv.QueueID != q2 || pv.Path != "/b.mp3" || pv.PositionMS != 7777 || !pv.Playing {
		t.Fatalf("delete-current must continue next queue at remembered state: %#v", pv)
	}

	if err = svc.SetStopTarget(ctx, q2, "/c.mp3"); err != nil {
		t.Fatal(err)
	}
	if err = svc.QueueMove(ctx, q2, "/c.mp3", 0); err != nil {
		t.Fatal(err)
	}
	pv, _ = svc.Playback(ctx)
	if pv.StopPath != "/c.mp3" {
		t.Fatalf("stop target must follow song when it moves: %#v", pv)
	}
	if err = svc.QueueRemove(ctx, q2, "/c.mp3"); err != nil {
		t.Fatal(err)
	}
	pv, _ = svc.Playback(ctx)
	if pv.StopPath != "" {
		t.Fatalf("removing stop-target song must cancel target: %#v", pv)
	}
}
