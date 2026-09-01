package domain

import (
	"encoding/json"
	"sort"
)

// Song is one Musicolet path-addressed song data block. ID is only a server-side
// technical identifier; Path remains the business key inside a source snapshot.
type Song struct {
	ID               int64  `json:"id,omitempty"`
	Path             string `json:"path"`
	Title            string `json:"title"`
	Artist           string `json:"artist"`
	Album            string `json:"album"`
	AlbumArtist      string `json:"albumArtist"`
	Composer         string `json:"composer"`
	Genre            string `json:"genre"`
	Lyrics           string `json:"lyrics"`
	Comment          string `json:"comment"`
	TrackNo          string `json:"trackNo"`
	DiscNo           string `json:"discNo"`
	Year             int64  `json:"year"`
	DurationMS       int64  `json:"durationMs"`
	DateAddedMS      int64  `json:"dateAddedMs"`
	DateModifiedMS   int64  `json:"dateModifiedMs"`
	Bitrate          int64  `json:"bitrate"`
	SampleRate       int64  `json:"sampleRate"`
	BitDepth         int64  `json:"bitDepth"`
	Format           string `json:"format"`
	Codec            string `json:"codec"`
	Channels         int64  `json:"channels"`
	FileSize         int64  `json:"fileSize"`
	Favorite         bool   `json:"favorite"`
	Deleted          bool   `json:"deleted,omitempty"`
	HasServerChanges bool   `json:"hasServerChanges,omitempty"`
}

type PlaybackStats struct {
	Path       string           `json:"path"`
	Total      int64            `json:"total"`
	Weekly     map[string]int64 `json:"weekly,omitempty"`
	Monthly    map[string]int64 `json:"monthly,omitempty"`
	Yearly     map[string]int64 `json:"yearly,omitempty"`
	LastPlayed int64            `json:"lastPlayed"`
}

type OrderedList struct {
	ID               int64    `json:"id,omitempty"`
	SourceKey        string   `json:"sourceKey"`
	Name             string   `json:"name"`
	Paths            []string `json:"paths"`
	HasServerChanges bool     `json:"hasServerChanges,omitempty"`
}

type Queue struct {
	OrderedList
	Position   int    `json:"position"`
	Current    int    `json:"current"`
	ProgressMS int64  `json:"progressMs"`
	StopPath   string `json:"stopPath,omitempty"`
}

type PlaybackState struct {
	QueueID     int64   `json:"queueId"`
	SongPath    string  `json:"songPath"`
	ProgressMS  int64   `json:"progressMs"`
	Playing     bool    `json:"playing"`
	Shuffle     bool    `json:"shuffle"`
	RepeatMode  string  `json:"repeatMode"`
	Speed       float64 `json:"speed"`
	UpdatedAtMS int64   `json:"updatedAtMs"`
}

type Snapshot struct {
	Songs             map[string]Song            `json:"songs"`
	Playlists         []OrderedList              `json:"playlists"`
	Queues            []Queue                    `json:"queues"`
	Stats             map[string]PlaybackStats   `json:"stats"`
	Settings          map[string]json.RawMessage `json:"settings,omitempty"`
	CurrentQueueIndex int                        `json:"currentQueueIndex"`
	Warnings          []string                   `json:"warnings,omitempty"`
}

func NewSnapshot() Snapshot {
	return Snapshot{
		Songs: make(map[string]Song), Stats: make(map[string]PlaybackStats),
		Settings: make(map[string]json.RawMessage), CurrentQueueIndex: -1,
	}
}

// Normalize makes snapshot JSON stable and enforces the no-duplicate member rule.
func (s *Snapshot) Normalize() {
	for path, song := range s.Songs {
		song.Path = path
		s.Songs[path] = song
	}
	for i := range s.Playlists {
		s.Playlists[i].Paths = unique(s.Playlists[i].Paths)
	}
	for i := range s.Queues {
		s.Queues[i].Paths = unique(s.Queues[i].Paths)
		s.Queues[i].Position = i
		if len(s.Queues[i].Paths) == 0 {
			s.Queues[i].Current = 0
		} else if s.Queues[i].Current >= len(s.Queues[i].Paths) {
			s.Queues[i].Current = len(s.Queues[i].Paths) - 1
		}
	}
	sort.SliceStable(s.Playlists, func(i, j int) bool { return s.Playlists[i].SourceKey < s.Playlists[j].SourceKey })
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s Song) Core() Song {
	s.ID = 0
	s.Favorite = false
	s.Deleted = false
	s.HasServerChanges = false
	return s
}
