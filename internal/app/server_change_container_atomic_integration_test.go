//go:build integration

package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestQueueAndPlaylistServerMutationsRollBackWhenAuditInsertFails(t *testing.T) {
	type fixture struct {
		svc      *Service
		q1, q2   int64
		ctx      context.Context
	}
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		ctx := context.Background()
		svc := newIntegrationService(t)
		snap := domain.EmptySnapshot()
		for _, p := range []string{"A", "B", "C"} {
			snap.Songs[p] = domain.Song{Path: p, Title: p}
		}
		snap.Queues = []domain.Queue{
			{Name: "Q1", Paths: []string{"A", "B"}, CurrentIndex: 0},
			{Name: "Q2", Paths: []string{"C"}, CurrentIndex: 0},
		}
		snap.Playlists = []domain.Playlist{{Name: "P", Paths: []string{"A", "B"}}}
		snap.CurrentQueueIndex = 0
		_ = installVersion(t, svc, snap)
		var q1, q2 int64
		if err := svc.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE name='Q1'").Scan(&q1); err != nil {
			t.Fatal(err)
		}
		if err := svc.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE name='Q2'").Scan(&q2); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER force_server_change_failure BEFORE INSERT ON server_changes BEGIN SELECT RAISE(ABORT, 'forced server_change failure'); END`); err != nil {
			t.Fatal(err)
		}
		return fixture{svc: svc, q1: q1, q2: q2, ctx: ctx}
	}
	assertNoAudit := func(t *testing.T, f fixture) {
		t.Helper()
		var changes int
		if err := f.svc.Store.DB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM server_changes").Scan(&changes); err != nil {
			t.Fatal(err)
		}
		if changes != 0 {
			t.Fatalf("failed mutation left %d server_changes", changes)
		}
	}
	assertQueue := func(t *testing.T, f fixture, id int64, want []string) {
		t.Helper()
		if got := queuePaths(f.ctx, f.svc.Store.DB, id); !reflect.DeepEqual(got, want) {
			t.Fatalf("queue changed despite audit failure: got=%v want=%v", got, want)
		}
	}
	assertPlaylist := func(t *testing.T, f fixture, want []string) {
		t.Helper()
		var id int64
		if err := f.svc.Store.DB.QueryRowContext(f.ctx, "SELECT id FROM working_playlists WHERE name='P'").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if got := playlistPaths(f.ctx, f.svc.Store.DB, id); !reflect.DeepEqual(got, want) {
			t.Fatalf("playlist changed despite audit failure: got=%v want=%v", got, want)
		}
	}

	t.Run("queue-add", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.QueueAdd(f.ctx, f.q1, "C", false); err == nil {
			t.Fatal("expected forced audit failure")
		}
		assertQueue(t, f, f.q1, []string{"A", "B"})
		assertNoAudit(t, f)
	})

	t.Run("queue-delete", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.DeleteQueue(f.ctx, f.q1); err == nil {
			t.Fatal("expected forced audit failure")
		}
		var count int
		if err := f.svc.Store.DB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM working_queues WHERE id=?", f.q1).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("queue delete leaked despite audit failure")
		}
		assertQueue(t, f, f.q1, []string{"A", "B"})
		assertNoAudit(t, f)
	})

	t.Run("queue-rename", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.RenameQueue(f.ctx, f.q1, "Renamed"); err == nil {
			t.Fatal("expected forced audit failure")
		}
		var name string
		if err := f.svc.Store.DB.QueryRowContext(f.ctx, "SELECT name FROM working_queues WHERE id=?", f.q1).Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != "Q1" {
			t.Fatalf("queue rename leaked: %q", name)
		}
		assertNoAudit(t, f)
	})

	t.Run("queue-global-order", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.ReorderQueues(f.ctx, f.q2, 0); err == nil {
			t.Fatal("expected forced audit failure")
		}
		rows, err := f.svc.Store.DB.QueryContext(f.ctx, "SELECT name FROM working_queues ORDER BY sort_position,id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		if !reflect.DeepEqual(names, []string{"Q1", "Q2"}) {
			t.Fatalf("queue order leaked: %v", names)
		}
		assertNoAudit(t, f)
	})

	t.Run("queue-reorder-items", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.QueueReorderItems(f.ctx, f.q1, []string{"B", "A"}, "REVERSE"); err == nil {
			t.Fatal("expected forced audit failure")
		}
		assertQueue(t, f, f.q1, []string{"A", "B"})
		assertNoAudit(t, f)
	})

	t.Run("playlist-add", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.PlaylistAction(f.ctx, "P", "add", "C", 0); err == nil {
			t.Fatal("expected forced audit failure")
		}
		assertPlaylist(t, f, []string{"A", "B"})
		assertNoAudit(t, f)
	})

	t.Run("playlist-delete", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.PlaylistAction(f.ctx, "P", "delete", "", 0); err == nil {
			t.Fatal("expected forced audit failure")
		}
		assertPlaylist(t, f, []string{"A", "B"})
		assertNoAudit(t, f)
	})

	t.Run("source-queue-create", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.svc.EnsureSourceQueue(f.ctx, "album", "Album-X", "Album-X", []string{"A", "C"}, "A", false); err == nil {
			t.Fatal("expected forced audit failure")
		}
		var count int
		if err := f.svc.Store.DB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM working_queues").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("source queue leaked despite audit failure: %d queues", count)
		}
		assertNoAudit(t, f)
	})
}
