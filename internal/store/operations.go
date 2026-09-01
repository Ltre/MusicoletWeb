package store

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/domain"
	merger "github.com/Ltre/MusicoletWeb/internal/merge"
)

type History interface {
	PrepareCommit(context.Context, []byte, string) (commit, parent string, err error)
	FinalizeCommit(context.Context, string, string) error
}

func (s *Store) SetHistory(history History) { s.history = history }

type Change struct {
	TargetType, TargetKey, Operation, Detail string
	Before, After                            any
}

func (s *Store) applyChange(ctx context.Context, change Change, mutate func(*sql.Tx) error) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	before, _ := json.Marshal(change.Before)
	after, _ := json.Marshal(change.After)
	if err = mutate(tx); err != nil {
		return err
	}
	// Any M advances the server head. Active procedures must be re-analyzed
	// before commit; precise resolution staleness is decided by comparing the
	// prior and newly analyzed OURS values in SaveAnalysis.
	if _, err = tx.ExecContext(ctx, "UPDATE import_procedures SET status='REVIEWING',updated_at=CURRENT_TIMESTAMP WHERE status IN ('RESOLVING','READY_TO_COMMIT')"); err != nil {
		return err
	}
	rev, err := nextRevision(ctx, tx)
	if err != nil {
		return err
	}
	snap, err := loadSnapshot(ctx, tx, false, 0)
	if err != nil {
		return err
	}
	state, _, err := marshalStable(snap)
	if err != nil {
		return err
	}
	commit, parent := "", ""
	if s.history != nil {
		commit, parent, err = s.history.PrepareCommit(ctx, state, fmt.Sprintf("M%d %s %s", rev, change.Operation, change.TargetKey))
		if err != nil {
			return err
		}
	}
	var baseID any = nil
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM musicolet_versions ORDER BY version_number DESC LIMIT 1").Scan(&id); err == nil {
		baseID = id
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO server_changes(revision,base_version_id,target_type,target_key,operation,before_json,after_json,git_commit) VALUES(?,?,?,?,?,?,?,?)", rev, baseID, change.TargetType, change.TargetKey, change.Operation, before, after, commit)
	if err != nil {
		return err
	}
	changeID, _ := res.LastInsertId()
	if _, err = tx.ExecContext(ctx, "INSERT INTO change_targets(change_id,target_type,target_key,detail) VALUES(?,?,?,?)", changeID, change.TargetType, change.TargetKey, change.Detail); err != nil {
		return err
	}
	for _, path := range relatedSongPaths(change) {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO change_targets(change_id,target_type,target_key,detail) VALUES(?,'song',?,?)", changeID, path, change.TargetType+":"+change.Detail); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE songs SET has_server_changes=1 WHERE path=?", path); err != nil {
			return err
		}
	}
	if commit != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO git_commits(revision,commit_sha,parent_sha,message) VALUES(?,?,?,?)", rev, commit, parent, fmt.Sprintf("M%d %s", rev, change.Operation)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.history != nil && commit != "" {
		if err = s.history.FinalizeCommit(ctx, commit, parent); err != nil {
			return fmt.Errorf("database committed but Git ref finalization needs recovery: %w", err)
		}
	}
	return nil
}

func relatedSongPaths(change Change) []string {
	values := map[string]bool{}
	add := func(raw any) {
		if paths, ok := raw.([]string); ok {
			for _, path := range paths {
				values[path] = true
			}
		}
	}
	if change.TargetType == "queue" || change.TargetType == "playlist" {
		add(change.Before)
		add(change.After)
	}
	if change.TargetType == "playback_stats" {
		values[change.TargetKey] = true
	}
	out := make([]string, 0, len(values))
	for path := range values {
		if path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Store) UpdateSong(ctx context.Context, path string, patch map[string]any) error {
	current, err := s.songByPath(ctx, s.DB, path)
	if err != nil {
		return err
	}
	updated := current
	raw, _ := json.Marshal(patch)
	var typed domain.Song
	if err = json.Unmarshal(raw, &typed); err != nil {
		return err
	}
	if _, ok := patch["title"]; ok {
		updated.Title = typed.Title
	}
	if _, ok := patch["artist"]; ok {
		updated.Artist = typed.Artist
	}
	if _, ok := patch["album"]; ok {
		updated.Album = typed.Album
	}
	if _, ok := patch["albumArtist"]; ok {
		updated.AlbumArtist = typed.AlbumArtist
	}
	if _, ok := patch["composer"]; ok {
		updated.Composer = typed.Composer
	}
	if _, ok := patch["genre"]; ok {
		updated.Genre = typed.Genre
	}
	if _, ok := patch["lyrics"]; ok {
		updated.Lyrics = typed.Lyrics
	}
	if _, ok := patch["comment"]; ok {
		updated.Comment = typed.Comment
	}
	if _, ok := patch["trackNo"]; ok {
		updated.TrackNo = typed.TrackNo
	}
	if _, ok := patch["discNo"]; ok {
		updated.DiscNo = typed.DiscNo
	}
	if _, ok := patch["year"]; ok {
		updated.Year = typed.Year
	}
	updated.Path = current.Path
	updated.ID = current.ID
	updated.HasServerChanges = true
	return s.applyChange(ctx, Change{"song", path, "UPDATE_METADATA", "song_core", current, updated}, func(tx *sql.Tx) error {
		core, _ := json.Marshal(updated.Core())
		_, err := tx.ExecContext(ctx, "UPDATE songs SET core_json=?,has_server_changes=1,updated_at=CURRENT_TIMESTAMP WHERE path=? AND deleted=0", core, path)
		return err
	})
}

func (s *Store) ToggleFavorite(ctx context.Context, path string, favorite bool) error {
	song, err := s.songByPath(ctx, s.DB, path)
	if err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"song", path, "SET_FAVORITE", "favorite", song.Favorite, favorite}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE songs SET favorite=?,has_server_changes=1,updated_at=CURRENT_TIMESTAMP WHERE path=? AND deleted=0", boolInt(favorite), path)
		return err
	})
}

func (s *Store) DeleteSong(ctx context.Context, path string) error {
	song, err := s.songByPath(ctx, s.DB, path)
	if err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"song", path, "DELETE_SERVER_SONG", "deleted", song, nil}, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE songs SET deleted=1,has_server_changes=1,favorite=0,updated_at=CURRENT_TIMESTAMP WHERE path=?", path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM playlist_items WHERE song_path=?", path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM queue_items WHERE song_path=?", path); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM playback_stats WHERE song_path=?", path)
		return err
	})
}

func (s *Store) SongChanges(ctx context.Context, path string) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT c.revision,c.target_type,c.target_key,c.operation,t.detail,c.created_at,c.git_commit
FROM change_targets t
JOIN server_changes c ON c.id=t.change_id
WHERE t.target_type='song' AND t.target_key=? AND c.active=1
ORDER BY c.revision DESC`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var revision int64
		var targetType, targetKey, operation, detail, createdAt, commit string
		if err = rows.Scan(&revision, &targetType, &targetKey, &operation, &detail, &createdAt, &commit); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"revision": revision, "targetType": targetType, "targetKey": targetKey, "operation": operation, "detail": detail, "createdAt": createdAt, "gitCommit": commit})
	}
	return out, rows.Err()
}

func (s *Store) songByPath(ctx context.Context, q queryer, path string) (domain.Song, error) {
	var id int64
	var raw []byte
	var fav, deleted, changed int
	if err := q.QueryRowContext(ctx, "SELECT file_id,core_json,favorite,deleted,has_server_changes FROM songs WHERE path=?", path).Scan(&id, &raw, &fav, &deleted, &changed); err != nil {
		return domain.Song{}, err
	}
	var song domain.Song
	if err := json.Unmarshal(raw, &song); err != nil {
		return song, err
	}
	song.ID = id
	song.Favorite = fav != 0
	song.Deleted = deleted != 0
	song.HasServerChanges = changed != 0
	return song, nil
}

func (s *Store) CreateQueue(ctx context.Context, name, sourceType, sourceKey string, paths []string, shuffle bool) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Queue"
	}
	if sourceType != "" && sourceKey != "" {
		var existing int64
		err := s.DB.QueryRowContext(ctx, "SELECT queue_id FROM source_queue_links WHERE source_type=? AND source_key=?", sourceType, sourceKey).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	paths = dedupe(paths)
	if shuffle {
		randomShuffle(paths)
	}
	var newID int64
	queueKey, err := randomSourceKey("server:queue:")
	if err != nil {
		return 0, err
	}
	err = s.applyChange(ctx, Change{"queue", queueKey, "CREATE_QUEUE", "members", nil, paths}, func(tx *sql.Tx) error {
		if sourceType != "" && sourceKey != "" {
			var existing int64
			if err := tx.QueryRowContext(ctx, "SELECT queue_id FROM source_queue_links WHERE source_type=? AND source_key=?", sourceType, sourceKey).Scan(&existing); err == nil {
				newID = existing
				// Association reuse is identity reuse, not reconstruction. In
				// particular, a previously shuffled/reordered Queue must keep its
				// order and per-Queue memory point.
				return nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		name = uniqueQueueName(ctx, tx, name)
		var pos int
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(position),-1)+1 FROM queues").Scan(&pos); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO queues(source_key,name,position,has_server_changes) VALUES(?,?,?,1)", queueKey, name, pos)
		if err != nil {
			return err
		}
		newID, _ = res.LastInsertId()
		if err = replaceQueueItems(ctx, tx, newID, paths, false); err != nil {
			return err
		}
		if sourceType != "" && sourceKey != "" {
			_, err = tx.ExecContext(ctx, "INSERT INTO source_queue_links(source_type,source_key,queue_id) VALUES(?,?,?)", sourceType, sourceKey, newID)
		}
		return err
	})
	return newID, err
}

func (s *Store) ReorderQueues(ctx context.Context, ids []int64) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM queues ORDER BY position")
	if err != nil {
		return err
	}
	var before []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		before = append(before, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if len(ids) != len(before) {
		return errors.New("queue order must contain every queue exactly once")
	}
	seen := map[int64]bool{}
	allowed := map[int64]bool{}
	for _, id := range before {
		allowed[id] = true
	}
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return errors.New("queue order contains an unknown or duplicate queue")
		}
		seen[id] = true
	}
	return s.applyChange(ctx, Change{"queue_order", "all", "REORDER_QUEUES", "positions", before, ids}, func(tx *sql.Tx) error {
		// Move through a disjoint temporary range to satisfy UNIQUE(position)
		// throughout the transaction.
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE queues SET position=? WHERE id=?", -1-i, id); err != nil {
				return err
			}
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE queues SET position=?,has_server_changes=1 WHERE id=?", i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) RenameQueue(ctx context.Context, id int64, name string) error {
	var old, sourceKey string
	if err := s.DB.QueryRowContext(ctx, "SELECT name,source_key FROM queues WHERE id=?", id).Scan(&old, &sourceKey); err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"queue", sourceKey, "RENAME_QUEUE", "name", old, name}, func(tx *sql.Tx) error {
		name = uniqueQueueNameExcept(ctx, tx, strings.TrimSpace(name), id)
		_, err := tx.ExecContext(ctx, "UPDATE queues SET name=?,has_server_changes=1 WHERE id=?", name, id)
		return err
	})
}

func (s *Store) DeleteQueue(ctx context.Context, id int64) error {
	var oldPosition int
	var sourceKey string
	if err := s.DB.QueryRowContext(ctx, "SELECT position,source_key FROM queues WHERE id=?", id).Scan(&oldPosition, &sourceKey); err != nil {
		return err
	}
	items, err := listItems(ctx, s.DB, "queue_items", "queue_id", id, 0)
	if err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"queue", sourceKey, "DELETE_QUEUE", "queue", items, nil}, func(tx *sql.Tx) error {
		var playing int64
		_ = tx.QueryRowContext(ctx, "SELECT queue_id FROM runtime_playback_state WHERE singleton=1").Scan(&playing)
		if _, err := tx.ExecContext(ctx, "DELETE FROM queues WHERE id=?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE queues SET position=position-1 WHERE position>?", oldPosition); err != nil {
			return err
		}
		if playing == id {
			var nextID int64
			err := tx.QueryRowContext(ctx, "SELECT id FROM queues ORDER BY CASE WHEN position>=? THEN 0 ELSE 1 END,position LIMIT 1", oldPosition).Scan(&nextID)
			if errors.Is(err, sql.ErrNoRows) {
				nextID = 0
			} else if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,song_path=COALESCE((SELECT song_path FROM queue_items WHERE queue_id=? ORDER BY position LIMIT 1 OFFSET COALESCE((SELECT current_index FROM queues WHERE id=?),0)),''),progress_ms=COALESCE((SELECT progress_ms FROM queues WHERE id=?),0),playing=CASE WHEN ?=0 THEN 0 ELSE playing END WHERE singleton=1", nextID, nextID, nextID, nextID, nextID)
			return err
		}
		return nil
	})
}

func (s *Store) SetQueueItems(ctx context.Context, id int64, paths []string, mode string) error {
	paths = dedupe(paths)
	var sourceKey string
	if err := s.DB.QueryRowContext(ctx, "SELECT source_key FROM queues WHERE id=?", id).Scan(&sourceKey); err != nil {
		return err
	}
	before, err := listItems(ctx, s.DB, "queue_items", "queue_id", id, 0)
	if err != nil {
		return err
	}
	current := append([]string(nil), before...)
	switch mode {
	case "next":
		var currentIndex int
		if err := s.DB.QueryRowContext(ctx, "SELECT current_index FROM queues WHERE id=?", id).Scan(&currentIndex); err != nil {
			return err
		}
		for i := len(paths) - 1; i >= 0; i-- {
			removeString(&current, paths[i])
			current = insertString(current, paths[i], currentIndex+1)
		}
	case "tail":
		for _, path := range paths {
			removeString(&current, path)
			current = append(current, path)
		}
	default:
		current = paths
	}
	return s.applyChange(ctx, Change{"queue", sourceKey, "SET_QUEUE_ITEMS", "members", before, current}, func(tx *sql.Tx) error {
		if err := replaceQueueItems(ctx, tx, id, current, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE queues SET has_server_changes=1,stop_path=CASE WHEN stop_path<>'' AND NOT EXISTS(SELECT 1 FROM queue_items WHERE queue_id=? AND song_path=stop_path) THEN '' ELSE stop_path END WHERE id=?", id, id)
		return err
	})
}

func (s *Store) CreatePlaylist(ctx context.Context, name string, paths []string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("playlist name is required")
	}
	paths = dedupe(paths)
	var id int64
	key, err := randomSourceKey("server:playlist:")
	if err != nil {
		return 0, err
	}
	err = s.applyChange(ctx, Change{"playlist", key, "CREATE_PLAYLIST", "members", nil, paths}, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM playlists WHERE name=?", name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("a playlist with this name already exists")
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO playlists(source_key,name,personal,has_server_changes) VALUES(?,?,1,1)", key, name)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return replacePlaylistItems(ctx, tx, id, paths)
	})
	return id, err
}

func (s *Store) SetPlaylistItems(ctx context.Context, id int64, paths []string) error {
	paths = dedupe(paths)
	var sourceKey string
	if err := s.DB.QueryRowContext(ctx, "SELECT source_key FROM playlists WHERE id=?", id).Scan(&sourceKey); err != nil {
		return err
	}
	before, err := listItems(ctx, s.DB, "playlist_items", "playlist_id", id, 0)
	if err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"playlist", sourceKey, "SET_PLAYLIST_ITEMS", "members", before, paths}, func(tx *sql.Tx) error {
		if err := replacePlaylistItems(ctx, tx, id, paths); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE playlists SET has_server_changes=1 WHERE id=?", id)
		return err
	})
}

func (s *Store) DeletePlaylist(ctx context.Context, id int64) error {
	var personal int
	var sourceKey string
	if err := s.DB.QueryRowContext(ctx, "SELECT personal,source_key FROM playlists WHERE id=?", id).Scan(&personal, &sourceKey); err != nil {
		return err
	}
	if personal == 0 {
		return errors.New("system-derived playlists cannot be deleted")
	}
	items, err := listItems(ctx, s.DB, "playlist_items", "playlist_id", id, 0)
	if err != nil {
		return err
	}
	return s.applyChange(ctx, Change{"playlist", sourceKey, "DELETE_PLAYLIST", "playlist", items, nil}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM playlists WHERE id=?", id)
		return err
	})
}

func (s *Store) UpdatePlayback(ctx context.Context, state domain.PlaybackState) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	state.UpdatedAtMS = time.Now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,song_path=?,progress_ms=?,playing=?,shuffle=?,repeat_mode=?,speed=?,updated_at_ms=? WHERE singleton=1", state.QueueID, state.SongPath, state.ProgressMS, boolInt(state.Playing), boolInt(state.Shuffle), state.RepeatMode, state.Speed, state.UpdatedAtMS); err != nil {
		return err
	}
	if state.QueueID != 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE queues SET current_index=COALESCE((SELECT position FROM queue_items WHERE queue_id=? AND song_path=?),current_index),progress_ms=? WHERE id=?", state.QueueID, state.SongPath, state.ProgressMS, state.QueueID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetQueueStopTarget(ctx context.Context, id int64, path string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if path != "" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM queue_items WHERE queue_id=? AND song_path=?", id, path).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("stop target must belong to the queue")
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE queues SET stop_path=? WHERE id=?", path, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordCompletedPlay(ctx context.Context, path string) error {
	if _, err := s.songByPath(ctx, s.DB, path); err != nil {
		return err
	}
	now := time.Now()
	weekYear, week := now.ISOWeek()
	weekKey := fmt.Sprintf("%04d.%02d", weekYear, week)
	monthKey := now.Format("2006.1")
	yearKey := now.Format("2006")
	return s.applyChange(ctx, Change{"playback_stats", path, "PLAY_COMPLETED", "counts", nil, map[string]any{"delta": 1, "lastPlayed": now.Unix()}}, func(tx *sql.Tx) error {
		var total, last int64
		var wRaw, mRaw, yRaw []byte
		err := tx.QueryRowContext(ctx, "SELECT total,last_played,weekly_json,monthly_json,yearly_json FROM playback_stats WHERE song_path=?", path).Scan(&total, &last, &wRaw, &mRaw, &yRaw)
		if errors.Is(err, sql.ErrNoRows) {
			wRaw = []byte("{}")
			mRaw = []byte("{}")
			yRaw = []byte("{}")
			total = 0
		} else if err != nil {
			return err
		}
		w, m, y := map[string]int64{}, map[string]int64{}, map[string]int64{}
		_ = json.Unmarshal(wRaw, &w)
		_ = json.Unmarshal(mRaw, &m)
		_ = json.Unmarshal(yRaw, &y)
		w[weekKey]++
		m[monthKey]++
		y[yearKey]++
		wRaw, _ = json.Marshal(w)
		mRaw, _ = json.Marshal(m)
		yRaw, _ = json.Marshal(y)
		_, err = tx.ExecContext(ctx, "INSERT INTO playback_stats(song_path,total,previous_resolve_total,last_played,weekly_json,monthly_json,yearly_json) VALUES(?,?,0,?,?,?,?) ON CONFLICT(song_path) DO UPDATE SET total=excluded.total,last_played=excluded.last_played,weekly_json=excluded.weekly_json,monthly_json=excluded.monthly_json,yearly_json=excluded.yearly_json", path, total+1, maxInt64(last, now.Unix()), wRaw, mRaw, yRaw)
		return err
	})
}

func (s *Store) Playback(ctx context.Context) (domain.PlaybackState, error) {
	var st domain.PlaybackState
	var playing, shuffle int
	err := s.DB.QueryRowContext(ctx, "SELECT queue_id,song_path,progress_ms,playing,shuffle,repeat_mode,speed,updated_at_ms FROM runtime_playback_state WHERE singleton=1").Scan(&st.QueueID, &st.SongPath, &st.ProgressMS, &playing, &shuffle, &st.RepeatMode, &st.Speed, &st.UpdatedAtMS)
	st.Playing = playing != 0
	st.Shuffle = shuffle != 0
	return st, err
}

func replaceQueueItems(ctx context.Context, tx *sql.Tx, id int64, paths []string, moveExisting bool) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM queue_items WHERE queue_id=?", id); err != nil {
		return err
	}
	for i, path := range dedupe(paths) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO queue_items(queue_id,song_path,position) VALUES(?,?,?)", id, path, i); err != nil {
			return err
		}
	}
	return nil
}
func replacePlaylistItems(ctx context.Context, tx *sql.Tx, id int64, paths []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM playlist_items WHERE playlist_id=?", id); err != nil {
		return err
	}
	for i, path := range dedupe(paths) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO playlist_items(playlist_id,song_path,position) VALUES(?,?,?)", id, path, i); err != nil {
			return err
		}
	}
	return nil
}
func uniqueQueueName(ctx context.Context, tx *sql.Tx, name string) string {
	return uniqueQueueNameExcept(ctx, tx, name, 0)
}
func uniqueQueueNameExcept(ctx context.Context, tx *sql.Tx, name string, except int64) string {
	if name == "" {
		name = "Queue"
	}
	candidate := name
	for n := 2; ; n++ {
		var count int
		_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM queues WHERE name=? AND id<>?", candidate, except).Scan(&count)
		if count == 0 {
			return candidate
		}
		candidate = fmt.Sprintf("%s #%d", name, n)
	}
}
func dedupe(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}
func randomShuffle(paths []string) {
	rand.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
}

func randomSourceKey(prefix string) (string, error) {
	var value [12]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func removeString(values *[]string, target string) {
	out := (*values)[:0]
	for _, v := range *values {
		if v != target {
			out = append(out, v)
		}
	}
	*values = out
}
func insertString(values []string, value string, index int) []string {
	if index < 0 {
		index = 0
	}
	if index > len(values) {
		index = len(values)
	}
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func (s *Store) SaveAnalysis(ctx context.Context, procedureID string, plan merger.Plan, revision int64) error {
	mergedRaw, _ := json.Marshal(plan.Merged)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentStatus string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM import_procedures WHERE id=?", procedureID).Scan(&currentStatus); err != nil {
		return err
	}
	if currentStatus != "REVIEWING" && currentStatus != "RESOLVING" && currentStatus != "READY_TO_COMMIT" {
		return fmt.Errorf("procedure is no longer reviewable: %s", currentStatus)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM semantic_diffs WHERE procedure_id=?", procedureID); err != nil {
		return err
	}
	for _, d := range plan.Diffs {
		raw, _ := json.Marshal(d.Detail)
		if _, err = tx.ExecContext(ctx, "INSERT INTO semantic_diffs(procedure_id,target_type,target_key,change_kind,detail_json) VALUES(?,?,?,?,?)", procedureID, d.TargetType, d.TargetKey, d.ChangeKind, raw); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, c := range plan.Conflicts {
		seen[c.ID] = true
		b, _ := json.Marshal(c.Base)
		o, _ := json.Marshal(c.Ours)
		t, _ := json.Marshal(c.Theirs)
		var oldOurs []byte
		oldErr := tx.QueryRowContext(ctx, "SELECT ours_json FROM merge_conflicts WHERE id=? AND procedure_id=?", c.ID, procedureID).Scan(&oldOurs)
		stale := oldErr == nil && string(oldOurs) != string(o)
		if stale {
			if _, err = tx.ExecContext(ctx, "UPDATE conflict_resolutions SET stale=1 WHERE conflict_id=?", c.ID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO merge_conflicts(id,procedure_id,target_type,target_key,reason,base_json,ours_json,theirs_json,stale) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET target_type=excluded.target_type,target_key=excluded.target_key,reason=excluded.reason,base_json=excluded.base_json,ours_json=excluded.ours_json,theirs_json=excluded.theirs_json,stale=MAX(merge_conflicts.stale,excluded.stale)", c.ID, procedureID, c.TargetType, c.TargetKey, c.Reason, b, o, t, boolInt(stale)); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM merge_conflicts WHERE procedure_id=?", procedureID)
	if err != nil {
		return err
	}
	var obsolete []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if !seen[id] {
			obsolete = append(obsolete, id)
		}
	}
	rows.Close()
	for _, id := range obsolete {
		if _, err = tx.ExecContext(ctx, "DELETE FROM merge_conflicts WHERE id=?", id); err != nil {
			return err
		}
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts c LEFT JOIN conflict_resolutions r ON r.conflict_id=c.id WHERE c.procedure_id=? AND (r.conflict_id IS NULL OR r.stale=1 OR c.stale=1)", procedureID).Scan(&unresolved); err != nil {
		return err
	}
	status := "READY_TO_COMMIT"
	if unresolved > 0 {
		status = "RESOLVING"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE import_procedures SET status=?,merged_json=?,last_analyzed_server_revision=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('REVIEWING','RESOLVING','READY_TO_COMMIT')", status, mergedRaw, revision, procedureID); err != nil {
		return err
	}
	return tx.Commit()
}

type ConflictRow struct {
	ID, TargetType, TargetKey, Reason, Choice string
	Base, Ours, Theirs, Resolved              json.RawMessage
	Stale                                     bool
}

func (s *Store) Conflicts(ctx context.Context, procedureID string) ([]ConflictRow, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT c.id,c.target_type,c.target_key,c.reason,c.base_json,c.ours_json,c.theirs_json,COALESCE(r.choice,''),COALESCE(r.resolved_json,'null'),(c.stale OR COALESCE(r.stale,0)) FROM merge_conflicts c LEFT JOIN conflict_resolutions r ON r.conflict_id=c.id WHERE c.procedure_id=? ORDER BY c.target_type,c.target_key", procedureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConflictRow
	for rows.Next() {
		var row ConflictRow
		var stale int
		if err = rows.Scan(&row.ID, &row.TargetType, &row.TargetKey, &row.Reason, &row.Base, &row.Ours, &row.Theirs, &row.Choice, &row.Resolved, &stale); err != nil {
			return nil, err
		}
		row.Stale = stale != 0
		out = append(out, row)
	}
	return out, rows.Err()
}
func (s *Store) ResolveConflict(ctx context.Context, procedureID, conflictID, choice string, resolved, patch json.RawMessage) error {
	choice = strings.ToUpper(choice)
	if choice != "OURS" && choice != "THEIRS" && choice != "MANUAL" {
		return errors.New("choice must be OURS, THEIRS or MANUAL")
	}
	var exists int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts WHERE id=? AND procedure_id=?", conflictID, procedureID).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return sql.ErrNoRows
	}
	rev, err := s.ServerRevision(ctx)
	if err != nil {
		return err
	}
	if len(resolved) == 0 {
		resolved = []byte("null")
	}
	if len(patch) == 0 {
		patch = []byte("{}")
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO conflict_resolutions(conflict_id,choice,resolved_json,patch_json,server_revision,stale) VALUES(?,?,?,?,?,0) ON CONFLICT(conflict_id) DO UPDATE SET choice=excluded.choice,resolved_json=excluded.resolved_json,patch_json=excluded.patch_json,server_revision=excluded.server_revision,stale=0,resolved_at=CURRENT_TIMESTAMP", conflictID, choice, resolved, patch, rev)
	if err != nil {
		return err
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE merge_conflicts SET stale=0 WHERE id=?", conflictID); err != nil {
		return err
	}
	var unresolved int
	if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM merge_conflicts c LEFT JOIN conflict_resolutions r ON r.conflict_id=c.id WHERE c.procedure_id=? AND (r.conflict_id IS NULL OR r.stale=1 OR c.stale=1)", procedureID).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved == 0 {
		_, err = s.DB.ExecContext(ctx, "UPDATE import_procedures SET status='READY_TO_COMMIT',updated_at=CURRENT_TIMESTAMP WHERE id=?", procedureID)
	}
	return err
}

func (s *Store) CommitProcedure(ctx context.Context, procedureID string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	sourceLinks, err := s.sourceQueueLinks(ctx)
	if err != nil {
		return err
	}
	var status, archiveHash, parserVersion string
	var baseID, analyzedRevision int64
	var candidateRaw, mergedRaw []byte
	if err = s.DB.QueryRowContext(ctx, "SELECT status,COALESCE(base_version_id,0),archive_sha256,parser_version,candidate_json,merged_json,last_analyzed_server_revision FROM import_procedures WHERE id=?", procedureID).Scan(&status, &baseID, &archiveHash, &parserVersion, &candidateRaw, &mergedRaw, &analyzedRevision); err != nil {
		return err
	}
	currentRevision, err := s.ServerRevision(ctx)
	if err != nil {
		return err
	}
	if currentRevision != analyzedRevision && baseID != 0 {
		return errors.New("server state changed after analysis; re-analyze before commit")
	}
	if status != "READY_TO_COMMIT" && !(baseID == 0 && status == "REVIEWING") {
		return errors.New("procedure is not ready to commit")
	}
	var source, working domain.Snapshot
	if err = json.Unmarshal(candidateRaw, &source); err != nil {
		return err
	}
	if baseID == 0 {
		working = source
	} else if err = json.Unmarshal(mergedRaw, &working); err != nil {
		return err
	}
	if baseID != 0 {
		conflicts, err := s.Conflicts(ctx, procedureID)
		if err != nil {
			return err
		}
		for _, c := range conflicts {
			if c.Choice == "" || c.Stale {
				return errors.New("all conflicts must have a fresh resolution")
			}
			applyResolution(&working, c)
		}
	}
	// Playback state is never an import conflict. Re-read the latest per-Queue
	// memory after applying content resolutions so a long-running Procedure
	// cannot roll progress/current/stop-target back to analysis-time values.
	currentWorking, err := s.LoadWorking(ctx)
	if err != nil {
		return err
	}
	currentPlayback, err := s.Playback(ctx)
	if err != nil {
		return err
	}
	currentQueueSource := ""
	for _, queue := range currentWorking.Queues {
		if queue.ID == currentPlayback.QueueID {
			currentQueueSource = queue.SourceKey
			break
		}
	}
	queueState := make(map[string]domain.Queue, len(currentWorking.Queues))
	for _, queue := range currentWorking.Queues {
		queueState[queue.SourceKey] = queue
	}
	for i := range working.Queues {
		if current, ok := queueState[working.Queues[i].SourceKey]; ok {
			working.Queues[i].Current = current.Current
			working.Queues[i].ProgressMS = current.ProgressMS
			working.Queues[i].StopPath = current.StopPath
		}
	}
	state, _, err := marshalStable(working)
	if err != nil {
		return err
	}
	commit, parent := "", ""
	if s.history != nil {
		commit, parent, err = s.history.PrepareCommit(ctx, state, "Import procedure "+procedureID)
		if err != nil {
			return err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version_number),0)+1 FROM musicolet_versions").Scan(&version); err != nil {
		return err
	}
	sourceHash, _ := SnapshotHash(source)
	res, err := tx.ExecContext(ctx, "INSERT INTO musicolet_versions(version_number,procedure_id,archive_sha256,parser_version,snapshot_sha256,git_commit,current_queue_index) VALUES(?,?,?,?,?,?,?)", version, procedureID, archiveHash, parserVersion, sourceHash, commit, source.CurrentQueueIndex)
	if err != nil {
		return err
	}
	versionID, _ := res.LastInsertId()
	if err = persistVersion(ctx, tx, versionID, source); err != nil {
		return err
	}
	if err = replaceWorking(ctx, tx, versionID, working); err != nil {
		return err
	}
	for _, link := range sourceLinks {
		var queueID int64
		if scanErr := tx.QueryRowContext(ctx, "SELECT id FROM queues WHERE source_key=?", link.QueueSourceKey).Scan(&queueID); errors.Is(scanErr, sql.ErrNoRows) {
			continue
		} else if scanErr != nil {
			return scanErr
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO source_queue_links(source_type,source_key,queue_id) VALUES(?,?,?)", link.SourceType, link.SourceKey, queueID); err != nil {
			return err
		}
	}
	if baseID == 0 {
		index := source.CurrentQueueIndex
		if index < 0 || index >= len(working.Queues) {
			index = 0
		}
		if len(working.Queues) > 0 {
			queue := working.Queues[index]
			var queueID int64
			if err = tx.QueryRowContext(ctx, "SELECT id FROM queues WHERE source_key=?", queue.SourceKey).Scan(&queueID); err != nil {
				return err
			}
			path := ""
			if len(queue.Paths) > 0 {
				current := queue.Current
				if current < 0 || current >= len(queue.Paths) {
					current = 0
				}
				path = queue.Paths[current]
			}
			if _, err = tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,song_path=?,progress_ms=?,playing=0,shuffle=0,repeat_mode='list',speed=1,updated_at_ms=? WHERE singleton=1", queueID, path, queue.ProgressMS, time.Now().UnixMilli()); err != nil {
				return err
			}
		}
	} else {
		var queueID int64
		if currentQueueSource != "" {
			_ = tx.QueryRowContext(ctx, "SELECT id FROM queues WHERE source_key=?", currentQueueSource).Scan(&queueID)
		}
		if queueID == 0 {
			_ = tx.QueryRowContext(ctx, "SELECT id FROM queues ORDER BY position LIMIT 1").Scan(&queueID)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,song_path=CASE WHEN ?=0 THEN '' ELSE song_path END,progress_ms=?,playing=CASE WHEN ?=0 THEN 0 ELSE playing END,shuffle=?,repeat_mode=?,speed=?,updated_at_ms=? WHERE singleton=1", queueID, queueID, currentPlayback.ProgressMS, queueID, boolInt(currentPlayback.Shuffle), currentPlayback.RepeatMode, currentPlayback.Speed, time.Now().UnixMilli()); err != nil {
			return err
		}
	}
	rev, err := nextRevision(ctx, tx)
	if err != nil {
		return err
	}
	if commit != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO git_commits(revision,commit_sha,parent_sha,message) VALUES(?,?,?,?)", rev, commit, parent, "Import "+procedureID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE import_procedures SET status='COMMITTED',committed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=?", procedureID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.history != nil && commit != "" {
		return s.history.FinalizeCommit(ctx, commit, parent)
	}
	return nil
}

type sourceQueueLink struct {
	SourceType, SourceKey, QueueSourceKey string
}

func (s *Store) sourceQueueLinks(ctx context.Context) ([]sourceQueueLink, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT l.source_type,l.source_key,q.source_key FROM source_queue_links l JOIN queues q ON q.id=l.queue_id ORDER BY l.source_type,l.source_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sourceQueueLink
	for rows.Next() {
		var link sourceQueueLink
		if err = rows.Scan(&link.SourceType, &link.SourceKey, &link.QueueSourceKey); err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func applyResolution(working *domain.Snapshot, c ConflictRow) {
	var raw json.RawMessage
	switch c.Choice {
	case "OURS":
		raw = c.Ours
	case "THEIRS":
		raw = c.Theirs
	case "MANUAL":
		raw = c.Resolved
	}
	switch c.TargetType {
	case "song":
		if string(raw) == "null" {
			delete(working.Songs, c.TargetKey)
			return
		}
		var song domain.Song
		if json.Unmarshal(raw, &song) == nil {
			working.Songs[c.TargetKey] = song
		}
	case "favorite":
		var value bool
		if json.Unmarshal(raw, &value) == nil {
			song := working.Songs[c.TargetKey]
			song.Favorite = value
			working.Songs[c.TargetKey] = song
		}
	case "playlist":
		if string(raw) == "null" {
			removeList(&working.Playlists, c.TargetKey)
			return
		}
		var list domain.OrderedList
		if json.Unmarshal(raw, &list) == nil {
			replaceList(&working.Playlists, list)
		}
	case "queue":
		if string(raw) == "null" {
			removeQueue(&working.Queues, c.TargetKey)
			return
		}
		// Queue playback position/progress/stop-target are server-owned. Only
		// replace the ordered source list during an import resolution.
		var list domain.OrderedList
		if json.Unmarshal(raw, &list) == nil {
			for i := range working.Queues {
				if working.Queues[i].SourceKey == list.SourceKey {
					working.Queues[i].OrderedList = list
					return
				}
			}
			working.Queues = append(working.Queues, domain.Queue{OrderedList: list})
		}
	case "setting":
		if string(raw) == "null" {
			delete(working.Settings, c.TargetKey)
			return
		}
		working.Settings[c.TargetKey] = append(json.RawMessage(nil), raw...)
	}
}

func removeList(lists *[]domain.OrderedList, key string) {
	for i := range *lists {
		if (*lists)[i].SourceKey == key {
			*lists = append((*lists)[:i], (*lists)[i+1:]...)
			return
		}
	}
}

func removeQueue(queues *[]domain.Queue, key string) {
	for i := range *queues {
		if (*queues)[i].SourceKey == key {
			*queues = append((*queues)[:i], (*queues)[i+1:]...)
			return
		}
	}
}
func replaceList(lists *[]domain.OrderedList, value domain.OrderedList) {
	for i := range *lists {
		if (*lists)[i].SourceKey == value.SourceKey {
			(*lists)[i] = value
			return
		}
	}
	*lists = append(*lists, value)
}
func replaceQueue(queues *[]domain.Queue, value domain.Queue) {
	for i := range *queues {
		if (*queues)[i].SourceKey == value.SourceKey {
			(*queues)[i] = value
			return
		}
	}
	*queues = append(*queues, value)
}

func persistVersion(ctx context.Context, tx *sql.Tx, id int64, snap domain.Snapshot) error {
	for _, path := range sortedKeys(snap.Songs) {
		song := snap.Songs[path]
		core, _ := json.Marshal(song.Core())
		if _, err := tx.ExecContext(ctx, "INSERT INTO songs_snapshot(version_id,path,core_json,favorite) VALUES(?,?,?,?)", id, path, core, boolInt(song.Favorite)); err != nil {
			return err
		}
	}
	for _, list := range snap.Playlists {
		if _, err := tx.ExecContext(ctx, "INSERT INTO playlists_snapshot(version_id,source_key,name) VALUES(?,?,?)", id, list.SourceKey, list.Name); err != nil {
			return err
		}
		for i, path := range dedupe(list.Paths) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO playlist_items_snapshot(version_id,source_key,song_path,position) VALUES(?,?,?,?)", id, list.SourceKey, path, i); err != nil {
				return err
			}
		}
	}
	for i, q := range snap.Queues {
		if _, err := tx.ExecContext(ctx, "INSERT INTO queues_snapshot(version_id,source_key,name,position,current_index,progress_ms,stop_path) VALUES(?,?,?,?,?,?,?)", id, q.SourceKey, q.Name, i, q.Current, q.ProgressMS, q.StopPath); err != nil {
			return err
		}
		for p, path := range dedupe(q.Paths) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO queue_items_snapshot(version_id,source_key,song_path,position) VALUES(?,?,?,?)", id, q.SourceKey, path, p); err != nil {
				return err
			}
		}
	}
	for path, st := range snap.Stats {
		w, _ := json.Marshal(st.Weekly)
		m, _ := json.Marshal(st.Monthly)
		y, _ := json.Marshal(st.Yearly)
		if _, err := tx.ExecContext(ctx, "INSERT INTO playcount_snapshot(version_id,song_path,total,last_played,weekly_json,monthly_json,yearly_json) VALUES(?,?,?,?,?,?,?)", id, path, st.Total, st.LastPlayed, w, m, y); err != nil {
			return err
		}
	}
	for key, value := range snap.Settings {
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings_snapshot(version_id,setting_key,value_json) VALUES(?,?,?)", id, key, value); err != nil {
			return err
		}
	}
	return nil
}

func replaceWorking(ctx context.Context, tx *sql.Tx, versionID int64, snap domain.Snapshot) error {
	for _, table := range []string{"playlist_items", "queue_items", "source_queue_links", "playlists", "queues", "playback_stats", "settings", "songs"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	for _, path := range sortedKeys(snap.Songs) {
		song := snap.Songs[path]
		core, _ := json.Marshal(song.Core())
		if _, err := tx.ExecContext(ctx, "INSERT INTO songs(path,core_json,favorite,deleted,has_server_changes,source_version_id) VALUES(?,?,?,?,?,?)", path, core, boolInt(song.Favorite), boolInt(song.Deleted), boolInt(song.HasServerChanges), versionID); err != nil {
			return err
		}
	}
	for _, list := range snap.Playlists {
		res, err := tx.ExecContext(ctx, "INSERT INTO playlists(source_key,name,personal,has_server_changes,source_version_id) VALUES(?,?,1,?,?)", list.SourceKey, list.Name, boolInt(list.HasServerChanges), versionID)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		for i, path := range dedupe(list.Paths) {
			if _, err = tx.ExecContext(ctx, "INSERT INTO playlist_items(playlist_id,song_path,position) VALUES(?,?,?)", id, path, i); err != nil {
				return err
			}
		}
	}
	for i, q := range snap.Queues {
		res, err := tx.ExecContext(ctx, "INSERT INTO queues(source_key,name,position,current_index,progress_ms,stop_path,has_server_changes,source_version_id) VALUES(?,?,?,?,?,?,?,?)", q.SourceKey, q.Name, i, q.Current, q.ProgressMS, q.StopPath, boolInt(q.HasServerChanges), versionID)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		for p, path := range dedupe(q.Paths) {
			if _, err = tx.ExecContext(ctx, "INSERT INTO queue_items(queue_id,song_path,position) VALUES(?,?,?)", id, path, p); err != nil {
				return err
			}
		}
	}
	for path, st := range snap.Stats {
		w, _ := json.Marshal(st.Weekly)
		m, _ := json.Marshal(st.Monthly)
		y, _ := json.Marshal(st.Yearly)
		if _, err := tx.ExecContext(ctx, "INSERT INTO playback_stats(song_path,total,previous_resolve_total,last_played,weekly_json,monthly_json,yearly_json,source_version_id) VALUES(?,?,?,?,?,?,?,?)", path, st.Total, st.Total, st.LastPlayed, w, m, y, versionID); err != nil {
			return err
		}
	}
	for key, value := range snap.Settings {
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings(setting_key,value_json,source_version_id) VALUES(?,?,?)", key, value, versionID); err != nil {
			return err
		}
	}
	return nil
}
