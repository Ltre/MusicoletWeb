package musicolet

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"regexp"
	"strings"
)

type FileValidation struct {
	Name        string `json:"name"`
	ExpectedMD5 string `json:"expected_md5"`
	ActualMD5   string `json:"actual_md5"`
	OK          bool   `json:"ok"`
}
type ParseReport struct {
	Files                int              `json:"files"`
	Decrypted            int              `json:"decrypted"`
	PlainFallback        int              `json:"plain_fallback"`
	ManifestEntries      int              `json:"manifest_entries"`
	ManifestValidated    int              `json:"manifest_validated"`
	Songs                int              `json:"songs"`
	Playlists            int              `json:"playlists"`
	PlaylistItems        int              `json:"playlist_items"`
	OrphanPlaylistItems  int              `json:"orphan_playlist_items"`
	OrphanPlaylistPaths  int              `json:"orphan_playlist_paths"`
	Favorites            int              `json:"favorites"`
	OrphanFavorites      int              `json:"orphan_favorites"`
	Queues               int              `json:"queues"`
	QueueItems           int              `json:"queue_items"`
	OrphanQueueItems     int              `json:"orphan_queue_items"`
	CurrentQueueIndex    int              `json:"current_queue_index"`
	HistoricalPeriodSets int              `json:"historical_period_sets"`
	Validations          []FileValidation `json:"validations,omitempty"`
	UnknownFiles         []string         `json:"unknown_files,omitempty"`
}

var md5re = regexp.MustCompile(`(?i)^[0-9a-f]{32}$`)

func extractManifestMD5s(b []byte) map[string]string {
	out := map[string]string{}
	var v any
	if json.Unmarshal(b, &v) == nil {
		walkManifest(v, "", out)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && md5re.MatchString(fields[0]) {
			out[strings.TrimLeft(strings.Join(fields[1:], " "), "*")] = strings.ToLower(fields[0])
			continue
		}
		if len(fields) >= 2 && md5re.MatchString(fields[len(fields)-1]) {
			out[strings.Join(fields[:len(fields)-1], " ")] = strings.ToLower(fields[len(fields)-1])
		}
	}
	return out
}
func walkManifest(v any, key string, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		name := ""
		hash := ""
		for k, val := range x {
			lk := strings.ToLower(k)
			if lk == "name" || lk == "path" || lk == "file" {
				if s, ok := val.(string); ok {
					name = s
				}
			}
			if lk == "md5" || lk == "hash" {
				if s, ok := val.(string); ok && md5re.MatchString(s) {
					hash = strings.ToLower(s)
				}
			}
		}
		if name != "" && hash != "" {
			out[name] = hash
		}
		for k, val := range x {
			if s, ok := val.(string); ok && md5re.MatchString(s) && k != "hash" {
				out[k] = strings.ToLower(s)
			}
			walkManifest(val, k, out)
		}
	case []any:
		for _, v := range x {
			walkManifest(v, key, out)
		}
	}
}
func validateManifestFiles(manifest []byte, files map[string][]byte) ([]FileValidation, error) {
	ex := extractManifestMD5s(manifest)
	r := make([]FileValidation, 0, len(ex))
	for name, want := range ex {
		plain, ok := files[name]
		if !ok {
			return r, fmt.Errorf("manifest references missing file %q", name)
		}
		sum := md5.Sum(plain)
		got := hex.EncodeToString(sum[:])
		v := FileValidation{Name: name, ExpectedMD5: want, ActualMD5: got, OK: strings.EqualFold(want, got)}
		r = append(r, v)
		if !v.OK {
			return r, fmt.Errorf("manifest MD5 mismatch for %s", name)
		}
	}
	return r, nil
}

func summarizeSnapshot(report *ParseReport, snap domain.Snapshot) {
	report.Songs = len(snap.Songs)
	report.Playlists = len(snap.Playlists)
	report.Favorites = len(snap.Favorites)
	report.Queues = len(snap.Queues)
	report.CurrentQueueIndex = snap.CurrentQueueIndex
	report.HistoricalPeriodSets = len(snap.PeriodCounts)
	orphanPlaylistPaths := map[string]bool{}
	for _, pl := range snap.Playlists {
		report.PlaylistItems += len(pl.Paths)
		for _, p := range pl.Paths {
			if _, ok := snap.Songs[p]; !ok {
				report.OrphanPlaylistItems++
				orphanPlaylistPaths[p] = true
			}
		}
	}
	report.OrphanPlaylistPaths = len(orphanPlaylistPaths)
	for p := range snap.Favorites {
		if _, ok := snap.Songs[p]; !ok {
			report.OrphanFavorites++
		}
	}
	for _, q := range snap.Queues {
		report.QueueItems += len(q.Paths)
		for _, p := range q.Paths {
			if _, ok := snap.Songs[p]; !ok {
				report.OrphanQueueItems++
			}
		}
	}
}
