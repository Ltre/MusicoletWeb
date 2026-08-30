//go:build integration

package app

import (
	"context"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestServerChangesAreMarkedAndAuditedInGit(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	snap := domain.EmptySnapshot()
	snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A", Artist: "Artist"}
	snap.Songs["/b.mp3"] = domain.Song{Path: "/b.mp3", Title: "B", Artist: "Artist"}
	snap.Playlists = []domain.Playlist{{Name: "P", Paths: []string{"/a.mp3"}}}
	snap.Queues = []domain.Queue{{Name: "Q", Paths: []string{"/a.mp3", "/b.mp3"}, CurrentIndex: 0}}
	_ = installVersion(t, svc, snap)

	var qid int64
	if err := svc.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE name='Q'").Scan(&qid); err != nil {
		t.Fatal(err)
	}

	beforeHead := svc.Git.Head("refs/heads/main")
	if err := svc.SetFavorite(ctx, "/a.mp3", true); err != nil {
		t.Fatal(err)
	}
	meta := snap.Songs["/a.mp3"]
	meta.Title = "SERVER-A"
	if err := svc.UpdateMetadata(ctx, "/a.mp3", meta); err != nil {
		t.Fatal(err)
	}
	if err := svc.PlaylistAction(ctx, "P", "add", "/b.mp3", 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.QueueMove(ctx, qid, "/b.mp3", 0); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.Store.DB.QueryContext(ctx, `
		SELECT operation,COALESCE(git_commit,''),active
		FROM server_changes
		WHERE operation IN ('FAVORITE','METADATA','ADD','MOVE')
		ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	count := 0
	for rows.Next() {
		var op, commit string
		var active int
		if err = rows.Scan(&op, &commit, &active); err != nil {
			t.Fatal(err)
		}
		count++
		seen[op] = true
		if commit == "" {
			t.Fatalf("server change %s missing git commit", op)
		}
		if active != 1 {
			t.Fatalf("server change %s unexpectedly inactive", op)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 4 || !seen["FAVORITE"] || !seen["METADATA"] || !seen["ADD"] || !seen["MOVE"] {
		t.Fatalf("unexpected audited changes: count=%d seen=%v", count, seen)
	}
	if head := svc.Git.Head("refs/heads/main"); head == "" || head == beforeHead {
		t.Fatalf("git main HEAD did not advance: before=%q after=%q", beforeHead, head)
	}

	var songA, songB, playlist, queue int
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_songs WHERE path='/a.mp3'").Scan(&songA); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_songs WHERE path='/b.mp3'").Scan(&songB); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_playlists WHERE name='P'").Scan(&playlist); err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_queues WHERE name='Q'").Scan(&queue); err != nil {
		t.Fatal(err)
	}
	if songA != 1 || songB != 1 || playlist != 1 || queue != 1 {
		t.Fatalf("change marks missing: songA=%d songB=%d playlist=%d queue=%d", songA, songB, playlist, queue)
	}
}
