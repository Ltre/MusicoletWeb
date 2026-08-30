//go:build integration

package app

import (
	"context"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestSongServerMutationRollsBackWhenAuditInsertFails(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(context.Context, *Service, *testing.T)
		mutate func(context.Context, *Service) error
		check  func(context.Context, *Service, *testing.T)
	}{
		{
			name: "favorite",
			mutate: func(ctx context.Context, svc *Service) error {
				return svc.SetFavorite(ctx, "/a.mp3", true)
			},
			check: func(ctx context.Context, svc *Service, t *testing.T) {
				var favorite, marked int
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_favorites WHERE path='/a.mp3')").Scan(&favorite); err != nil {
					t.Fatal(err)
				}
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT has_server_changes FROM working_songs WHERE path='/a.mp3'").Scan(&marked); err != nil {
					t.Fatal(err)
				}
				if favorite != 0 || marked != 0 {
					t.Fatalf("favorite mutation leaked after audit failure: favorite=%d marked=%d", favorite, marked)
				}
			},
		},
		{
			name: "metadata",
			mutate: func(ctx context.Context, svc *Service) error {
				return svc.UpdateMetadata(ctx, "/a.mp3", domain.Song{Title: "SERVER"})
			},
			check: func(ctx context.Context, svc *Service, t *testing.T) {
				var title string
				var marked int
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT title,has_server_changes FROM working_songs WHERE path='/a.mp3'").Scan(&title, &marked); err != nil {
					t.Fatal(err)
				}
				if title != "A" || marked != 0 {
					t.Fatalf("metadata mutation leaked after audit failure: title=%q marked=%d", title, marked)
				}
			},
		},
		{
			name: "delete",
			setup: func(ctx context.Context, svc *Service, t *testing.T) {
				if _, err := svc.Store.DB.ExecContext(ctx, "INSERT INTO working_favorites(path) VALUES('/a.mp3')"); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(ctx context.Context, svc *Service) error {
				return svc.DeleteSong(ctx, "/a.mp3")
			},
			check: func(ctx context.Context, svc *Service, t *testing.T) {
				var deleted, favorite, queueItems int
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT deleted FROM working_songs WHERE path='/a.mp3'").Scan(&deleted); err != nil {
					t.Fatal(err)
				}
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM working_favorites WHERE path='/a.mp3'").Scan(&favorite); err != nil {
					t.Fatal(err)
				}
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM working_queue_items WHERE path='/a.mp3'").Scan(&queueItems); err != nil {
					t.Fatal(err)
				}
				if deleted != 0 || favorite != 1 || queueItems != 1 {
					t.Fatalf("delete mutation leaked after audit failure: deleted=%d favorite=%d queueItems=%d", deleted, favorite, queueItems)
				}
			},
		},
		{
			name: "play-count",
			mutate: func(ctx context.Context, svc *Service) error {
				return svc.IncrementPlay(ctx, "/a.mp3", time.Unix(1234, 0))
			},
			check: func(ctx context.Context, svc *Service, t *testing.T) {
				var count, last int64
				var marked int
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT play_count,last_played_ms,has_server_changes FROM working_songs WHERE path='/a.mp3'").Scan(&count, &last, &marked); err != nil {
					t.Fatal(err)
				}
				var current int
				if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM working_current_counts WHERE path='/a.mp3'").Scan(&current); err != nil {
					t.Fatal(err)
				}
				if count != 0 || last != 0 || marked != 0 || current != 0 {
					t.Fatalf("play mutation leaked after audit failure: count=%d last=%d marked=%d currentRows=%d", count, last, marked, current)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc := newIntegrationService(t)
			snap := domain.EmptySnapshot()
			snap.Songs["/a.mp3"] = domain.Song{Path: "/a.mp3", Title: "A"}
			snap.Queues = []domain.Queue{{Name: "Q", Paths: []string{"/a.mp3"}}}
			_ = installVersion(t, svc, snap)
			if tc.setup != nil {
				tc.setup(ctx, svc, t)
			}

			if _, err := svc.Store.DB.ExecContext(ctx, `CREATE TRIGGER force_server_change_failure BEFORE INSERT ON server_changes BEGIN SELECT RAISE(ABORT, 'forced server_change failure'); END`); err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(ctx, svc); err == nil {
				t.Fatal("expected forced server_change insert failure")
			}
			tc.check(ctx, svc, t)
			var changes int
			if err := svc.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_changes").Scan(&changes); err != nil {
				t.Fatal(err)
			}
			if changes != 0 {
				t.Fatalf("failed mutation left %d server_changes", changes)
			}
		})
	}
}
