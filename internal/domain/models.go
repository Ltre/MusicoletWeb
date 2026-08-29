package domain

import "encoding/json"

type Song struct {
	FileID           int64           `json:"file_id,omitempty"`
	Path             string          `json:"path"`
	Title            string          `json:"title"`
	Artist           string          `json:"artist"`
	Album            string          `json:"album"`
	AlbumArtist      string          `json:"album_artist"`
	Composer         string          `json:"composer"`
	Genre            string          `json:"genre"`
	Lyrics           string          `json:"lyrics"`
	TrackNo          string          `json:"track_no"`
	DiscNo           string          `json:"disc_no"`
	Year             string          `json:"year"`
	Comment          string          `json:"comment"`
	DurationMS       int64           `json:"duration_ms"`
	FileName         string          `json:"file_name"`
	Folder           string          `json:"folder"`
	ModifiedMS       int64           `json:"modified_ms"`
	AddedMS          int64           `json:"added_ms"`
	LastPlayedMS     int64           `json:"last_played_ms"`
	PlayCount        int64           `json:"play_count"`
	Raw              json.RawMessage `json:"raw,omitempty"`
	HasServerChanges bool            `json:"has_server_changes,omitempty"`
}

func (s Song) CoreKey() string {
	b, _ := json.Marshal(struct {
		Path, Title, Artist, Album, AlbumArtist, Composer, Genre, Lyrics, TrackNo, DiscNo, Year, Comment string
		DurationMS                                                                                       int64
	}{s.Path, s.Title, s.Artist, s.Album, s.AlbumArtist, s.Composer, s.Genre, s.Lyrics, s.TrackNo, s.DiscNo, s.Year, s.Comment, s.DurationMS})
	return string(b)
}

type Playlist struct {
	ID    int64    `json:"id,omitempty"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}
type Queue struct {
	ID           int64    `json:"id,omitempty"`
	Name         string   `json:"name"`
	Paths        []string `json:"paths"`
	CurrentIndex int      `json:"current_index"`
	PositionMS   int64    `json:"position_ms"`
	SourceType   string   `json:"source_type,omitempty"`
	SourceKey    string   `json:"source_key,omitempty"`
	StopPath     string   `json:"stop_path,omitempty"`
}
type Snapshot struct {
	Songs        map[string]Song             `json:"songs"`
	Playlists    []Playlist                  `json:"playlists"`
	Queues       []Queue                     `json:"queues"`
	Favorites    map[string]bool             `json:"favorites"`
	PeriodCounts map[string]map[string]int64 `json:"period_counts"`
	RawFiles     map[string]string           `json:"raw_files,omitempty"`
}

func EmptySnapshot() Snapshot {
	return Snapshot{Songs: map[string]Song{}, Favorites: map[string]bool{}, PeriodCounts: map[string]map[string]int64{}, RawFiles: map[string]string{}}
}
