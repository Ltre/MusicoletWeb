//go:build integration

package musicolet

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func TestRealMusicoletBackup20260830(t *testing.T) {
	p := os.Getenv("MUSICOLET_REAL_BACKUP")
	if p == "" {
		t.Skip("set MUSICOLET_REAL_BACKUP to the 2026-08-30 Musicolet backup ZIP")
	}
	work := t.TempDir()
	snap, report, err := (Parser{}).ParseZipWithReport(context.Background(), p, work)
	if err != nil {
		t.Fatal(err)
	}
	want := ParseReport{
		Files: 89, Decrypted: 88, PlainFallback: 1,
		ManifestEntries: 87, ManifestValidated: 87,
		Songs: 6653, Playlists: 54, PlaylistItems: 29282,
		OrphanPlaylistItems: 85, OrphanPlaylistPaths: 21,
		Favorites: 5527, OrphanFavorites: 31,
		Queues: 14, QueueItems: 15780, OrphanQueueItems: 0,
		CurrentQueueIndex: 13, HistoricalPeriodSets: 23,
	}
	assertReportCounts(t, report, want)
	if report.CurrentQueueIndex < 0 || report.CurrentQueueIndex >= len(snap.Queues) {
		t.Fatalf("invalid current queue index %d for %d queues", report.CurrentQueueIndex, len(snap.Queues))
	}
	if snap.Queues[report.CurrentQueueIndex].Name != "至喜²｜H↑" {
		t.Fatalf("unexpected current queue: %q", snap.Queues[report.CurrentQueueIndex].Name)
	}
}

func TestRealMusicoletBackupDelta(t *testing.T) {
	basePath := os.Getenv("MUSICOLET_REAL_BASE_BACKUP")
	incomingPath := os.Getenv("MUSICOLET_REAL_BACKUP")
	if basePath == "" || incomingPath == "" {
		t.Skip("set MUSICOLET_REAL_BASE_BACKUP and MUSICOLET_REAL_BACKUP")
	}
	base, err := (Parser{}).ParseZip(context.Background(), basePath, filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := (Parser{}).ParseZip(context.Background(), incomingPath, filepath.Join(t.TempDir(), "incoming"))
	if err != nil {
		t.Fatal(err)
	}
	stats := compareRealSnapshots(base, incoming)
	if stats.AddedSongs != 1 || stats.DeletedSongs != 0 || stats.CoreChangedSongs != 0 || stats.PlayStatChangedSongs != 40 {
		t.Fatalf("unexpected song delta: %#v", stats)
	}
	if stats.ChangedPlaylists != 11 || stats.ChangedQueues != 1 || stats.FavoriteAdds != 0 || stats.FavoriteDeletes != 0 {
		t.Fatalf("unexpected relation delta: %#v", stats)
	}
}

type realDeltaStats struct {
	AddedSongs, DeletedSongs, CoreChangedSongs, PlayStatChangedSongs int
	ChangedPlaylists, ChangedQueues, FavoriteAdds, FavoriteDeletes int
}

func compareRealSnapshots(base, incoming domain.Snapshot) realDeltaStats {
	var r realDeltaStats
	for p, n := range incoming.Songs {
		o, ok := base.Songs[p]
		if !ok {
			r.AddedSongs++
			continue
		}
		if o.CoreKey() != n.CoreKey() {
			r.CoreChangedSongs++
		}
		if o.PlayCount != n.PlayCount || o.LastPlayedMS != n.LastPlayedMS || base.CurrentCounts[p] != incoming.CurrentCounts[p] {
			r.PlayStatChangedSongs++
		}
	}
	for p := range base.Songs {
		if _, ok := incoming.Songs[p]; !ok {
			r.DeletedSongs++
		}
	}
	r.ChangedPlaylists = changedOrderedLists(playlistPaths(base), playlistPaths(incoming))
	r.ChangedQueues = changedOrderedLists(queuePaths(base), queuePaths(incoming))
	for p := range incoming.Favorites {
		if !base.Favorites[p] {
			r.FavoriteAdds++
		}
	}
	for p := range base.Favorites {
		if !incoming.Favorites[p] {
			r.FavoriteDeletes++
		}
	}
	return r
}

func playlistPaths(s domain.Snapshot) map[string][]string {
	m := map[string][]string{}
	for _, x := range s.Playlists {
		m[x.Name] = x.Paths
	}
	return m
}
func queuePaths(s domain.Snapshot) map[string][]string {
	m := map[string][]string{}
	for _, x := range s.Queues {
		m[x.Name] = x.Paths
	}
	return m
}
func changedOrderedLists(a, b map[string][]string) int {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	ks := make([]string, 0, len(keys))
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	n := 0
	for _, k := range ks {
		if !sameStrings(a[k], b[k]) {
			n++
		}
	}
	return n
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertReportCounts(t *testing.T, got, want ParseReport) {
	t.Helper()
	if got.Files != want.Files || got.Decrypted != want.Decrypted || got.PlainFallback != want.PlainFallback ||
		got.ManifestEntries != want.ManifestEntries || got.ManifestValidated != want.ManifestValidated ||
		got.Songs != want.Songs || got.Playlists != want.Playlists || got.PlaylistItems != want.PlaylistItems ||
		got.OrphanPlaylistItems != want.OrphanPlaylistItems || got.OrphanPlaylistPaths != want.OrphanPlaylistPaths ||
		got.Favorites != want.Favorites || got.OrphanFavorites != want.OrphanFavorites ||
		got.Queues != want.Queues || got.QueueItems != want.QueueItems || got.OrphanQueueItems != want.OrphanQueueItems ||
		got.CurrentQueueIndex != want.CurrentQueueIndex || got.HistoricalPeriodSets != want.HistoricalPeriodSets {
		t.Fatalf("real backup report mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}
