//go:build integration

package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

const (
	scaleSongs         = 2000
	scalePlaylists     = 55
	scalePlaylistItems = 400
	scaleQueues        = 8
	scaleQueueItems    = 700
)

func TestInitialPlanScaleCandidateAndSemanticDiff(t *testing.T) {
	ctx := context.Background()
	svc := newIntegrationService(t)
	base := makeScaleSnapshot(false)
	incoming := makeScaleSnapshot(true)

	start := time.Now()
	vid := installVersion(t, svc, base)
	pid := createCandidateProcedure(t, svc, vid, incoming)
	elapsed := time.Since(start)

	p, err := svc.GetProcedure(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "READY_TO_COMMIT" {
		t.Fatalf("scale candidate should be conflict-free and ready, got %s", p.Status)
	}
	diffs, err := svc.ListDiffs(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) < scalePlaylists+scaleQueues {
		t.Fatalf("scale semantic diff unexpectedly small: %d", len(diffs))
	}

	working, err := svc.WorkingSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(working.Songs) != scaleSongs || len(working.Playlists) != scalePlaylists || len(working.Queues) != scaleQueues {
		t.Fatalf("base working scale mismatch: songs=%d playlists=%d queues=%d", len(working.Songs), len(working.Playlists), len(working.Queues))
	}
	playlistItems := 0
	for _, pl := range working.Playlists {
		playlistItems += len(pl.Paths)
	}
	queueItems := 0
	for _, q := range working.Queues {
		queueItems += len(q.Paths)
	}
	if playlistItems < 20000 || queueItems < 5000 {
		t.Fatalf("fixture is below initial-plan scale: playlistItems=%d queueItems=%d", playlistItems, queueItems)
	}

	// This is intentionally a broad regression ceiling, not a microbenchmark.
	// It catches accidental O(n^2)-style explosions while remaining tolerant of
	// shared GitHub runners and slower development machines.
	if elapsed > 60*time.Second {
		t.Fatalf("initial-plan scale pipeline took %s (>60s)", elapsed)
	}
	t.Logf("initial-plan scale: %d songs, %d playlists/%d items, %d queues/%d items; V1+Candidate+Analyze=%s; diffs=%d", scaleSongs, scalePlaylists, playlistItems, scaleQueues, queueItems, elapsed, len(diffs))
}

func makeScaleSnapshot(incoming bool) domain.Snapshot {
	s := domain.EmptySnapshot()
	paths := make([]string, scaleSongs)
	for i := 0; i < scaleSongs; i++ {
		path := fmt.Sprintf("/Music/Artist-%02d/track-%04d.mp3", i%40, i)
		paths[i] = path
		title := fmt.Sprintf("Track %04d", i)
		playCount := int64(i % 17)
		if incoming && i%250 == 0 {
			title += " phone-edit"
		}
		if incoming && i%100 == 0 {
			playCount++
		}
		s.Songs[path] = domain.Song{
			Path:         path,
			Title:        title,
			Artist:       fmt.Sprintf("Artist %02d", i%40),
			AlbumArtist:  fmt.Sprintf("Album Artist %02d", i%20),
			Album:        fmt.Sprintf("Album %03d", i%100),
			Composer:     fmt.Sprintf("Composer %02d", i%30),
			Genre:        fmt.Sprintf("Genre %02d", i%10),
			DurationMS:   int64(120000 + i%180000),
			FileName:     fmt.Sprintf("track-%04d.mp3", i),
			Folder:       fmt.Sprintf("/Music/Artist-%02d", i%40),
			AddedMS:      int64(1700000000000 + i*1000),
			ModifiedMS:   int64(1700000000000 + i*2000),
			LastPlayedMS: int64(1700000000000 + i*3000),
			PlayCount:    playCount,
		}
		if i%20 == 0 {
			s.Favorites[path] = true
		}
		s.CurrentCounts[path] = domain.CurrentCounts{Week: int64(i % 5), Month: int64(i % 11), Year: int64(i % 17)}
	}

	for p := 0; p < scalePlaylists; p++ {
		items := make([]string, scalePlaylistItems)
		for j := 0; j < scalePlaylistItems; j++ {
			items[j] = paths[(p*31+j*7)%scaleSongs]
		}
		if incoming {
			first := items[0]
			copy(items, items[1:])
			items[len(items)-1] = first
		}
		s.Playlists = append(s.Playlists, domain.Playlist{Name: fmt.Sprintf("Playlist %02d", p), Paths: items})
	}

	for q := 0; q < scaleQueues; q++ {
		items := make([]string, scaleQueueItems)
		for j := 0; j < scaleQueueItems; j++ {
			items[j] = paths[(q*43+j*13)%scaleSongs]
		}
		if incoming && q%2 == 0 {
			items[0], items[len(items)-1] = items[len(items)-1], items[0]
		}
		s.Queues = append(s.Queues, domain.Queue{Name: fmt.Sprintf("Queue %02d", q), Paths: items, CurrentIndex: q % scaleQueueItems, PositionMS: int64(q * 1000)})
	}
	return s
}
