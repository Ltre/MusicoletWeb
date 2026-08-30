package musicolet

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"os"
	"path/filepath"
	"sort"
)

func parseSongsDB(ctx context.Context, data []byte, s *domain.Snapshot) error {
	p, e := tempDB(data)
	if e != nil {
		return e
	}
	defer os.Remove(p)
	db, e := sql.Open("sqlite", "file:"+p+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	rows, e := db.QueryContext(ctx, "SELECT * FROM TABLE_SONGS")
	if e != nil {
		return e
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	vals := make([]any, len(cols))
	ptr := make([]any, len(cols))
	for i := range vals {
		ptr[i] = &vals[i]
	}
	for rows.Next() {
		if e = rows.Scan(ptr...); e != nil {
			return e
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		path := str(m["COL_PATH"])
		if path == "" {
			continue
		}
		song := songFromDBRow(m)
		raw, _ := json.Marshal(m)
		song.Raw = raw
		s.Songs[path] = song
		s.CurrentCounts[path] = domain.CurrentCounts{Week: num(m["COL_NUM_PLAYED_W"]), Month: num(m["COL_NUM_PLAYED_M"]), Year: num(m["COL_NUM_PLAYED_Y"])}
	}
	return rows.Err()
}

func songFromDBRow(m map[string]any) domain.Song {
	path := str(m["COL_PATH"])
	albumArtist := str(m["album_artist"])
	if albumArtist == "" {
		albumArtist = str(m["COL_ALBUM_ARTIST"]) // compatibility with older/export variants
	}
	trackNo, discNo := decodeTrackNo(num(m["COL_TRACK_NO"]))
	if trackNo == "" {
		trackNo = str(m["COL_TRACK"])
	}
	if discNo == "" {
		discNo = str(m["COL_DISC"])
	}
	song := domain.Song{
		Path: path, Title: str(m["COL_TITLE"]), Artist: str(m["COL_ARTIST"]), Album: str(m["COL_ALBUM"]),
		AlbumArtist: albumArtist, Composer: str(m["COL_COMPOSER"]), Genre: str(m["COL_GENRE"]),
		Lyrics: str(m["COL_LYRICS"]), TrackNo: trackNo, DiscNo: discNo, Year: str(m["COL_YEAR"]), Comment: str(m["COL_COMMENT"]),
		DurationMS: num(m["COL_DURATION"]), ModifiedMS: num(m["COL_DATE_MODIFIED"]), AddedMS: num(m["COL_DATE_ADDED"]),
		LastPlayedMS: secondPrecisionMS(num(m["COL_LAST_PLAYED"])), PlayCount: num(m["COL_NUM_PLAYED"]),
	}
	logPath := filepath.FromSlash(str(m["COL_LOGPATH"]))
	if logPath != "" && logPath != "." {
		song.FileName = filepath.Base(logPath)
		song.Folder = filepath.Dir(logPath)
	} else {
		song.FileName = filepath.Base(path)
		song.Folder = filepath.Dir(path)
	}
	return song
}

func decodeTrackNo(v int64) (track, disc string) {
	if v <= 0 {
		return "", ""
	}
	if v >= 1000 {
		d := v / 1000
		t := v % 1000
		if d > 0 {
			disc = fmt.Sprint(d)
		}
		if t > 0 {
			track = fmt.Sprint(t)
		}
		return track, disc
	}
	return fmt.Sprint(v), ""
}

func parseCountsDB(ctx context.Context, data []byte, key string, s *domain.Snapshot) error {
	p, e := tempDB(data)
	if e != nil {
		return e
	}
	defer os.Remove(p)
	db, e := sql.Open("sqlite", "file:"+p+"?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	rows, e := db.QueryContext(ctx, "SELECT COL_PATH,COL_NUM_PLAYED FROM TABLE_SONGS")
	if e != nil {
		return e
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var pth string
		var c int64
		if rows.Scan(&pth, &c) == nil {
			m[pth] = c
		}
	}
	if len(m) > 0 {
		s.PeriodCounts[key] = m
	}
	return nil
}

func parsePlaylistJSON(b []byte, name string, s *domain.Snapshot) error {
	var m map[string]any
	if e := json.Unmarshal(b, &m); e != nil {
		return e
	}
	paths := stringsSlice(m["S_P"])
	if len(paths) > 0 {
		s.Playlists = append(s.Playlists, domain.Playlist{Name: name, Paths: dedupe(paths)})
	}
	return nil
}

func parseFavoritesJSON(b []byte, s *domain.Snapshot) error {
	var m map[string]any
	if e := json.Unmarshal(b, &m); e != nil {
		return e
	}
	for _, p := range stringsSlice(m["S_P"]) {
		s.Favorites[p] = true
	}
	return nil
}

func parseQueuesJSON(b []byte, s *domain.Snapshot) error {
	var root map[string]any
	if e := json.Unmarshal(b, &root); e != nil {
		return e
	}
	arr, _ := root["S0_PQ"].([]any)
	current := int(num(root["S0_CPQ"]))
	if current >= 0 && current < len(arr) {
		s.CurrentQueueIndex = current
	}
	for _, x := range arr {
		m, _ := x.(map[string]any)
		q := domain.Queue{Name: str(m["S0_PQ_T"]), CurrentIndex: int(num(m["S0_PQ_CPS"])), PositionMS: num(m["S0_PQ_LKP"])}
		if oqs, ok := m["S0_PQ_OQS"].(map[string]any); ok {
			q.Paths = dedupe(stringsSlice(oqs["S_P"]))
		}
		if q.Name == "" {
			q.Name = fmt.Sprintf("Queue %d", len(s.Queues)+1)
		}
		s.Queues = append(s.Queues, q)
	}
	return nil
}

func stringsSlice(v any) []string {
	a, _ := v.([]any)
	r := make([]string, 0, len(a))
	for _, x := range a {
		if z := str(x); z != "" {
			r = append(r, z)
		}
	}
	return r
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	r := []string{}
	for _, x := range in {
		if !seen[x] {
			seen[x] = true
			r = append(r, x)
		}
	}
	return r
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}

func num(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case []byte:
		var n int64
		fmt.Sscan(string(x), &n)
		return n
	case string:
		var n int64
		fmt.Sscan(x, &n)
		return n
	}
	return 0
}

func secondPrecisionMS(v int64) int64 {
	if v == 0 {
		return 0
	}
	if v < 100000000000 {
		v *= 1000
	}
	return (v / 1000) * 1000
}

func CanonicalSnapshot(s domain.Snapshot) []byte {
	// Ordered playlist/queue members are semantic data; never sort their Paths.
	cp := s
	cp.RawFiles = nil
	cp.Playlists = append([]domain.Playlist(nil), s.Playlists...)
	cp.Queues = append([]domain.Queue(nil), s.Queues...)
	sort.Slice(cp.Playlists, func(i, j int) bool { return cp.Playlists[i].Name < cp.Playlists[j].Name })
	sort.Slice(cp.Queues, func(i, j int) bool { return cp.Queues[i].Name < cp.Queues[j].Name })
	b, _ := json.MarshalIndent(cp, "", "  ")
	return b
}
