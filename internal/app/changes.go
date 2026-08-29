package app

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"time"
)

func (s *Service) SetFavorite(ctx context.Context, path string, on bool) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var before bool
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_favorites WHERE path=?)", path).Scan(&before)
	if on {
		_, _ = s.Store.DB.ExecContext(ctx, "INSERT OR IGNORE INTO working_favorites(path) VALUES(?)", path)
	} else {
		_, _ = s.Store.DB.ExecContext(ctx, "DELETE FROM working_favorites WHERE path=?", path)
	}
	return s.recordChangeLocked(ctx, "song", path, "FAVORITE", before, on)
}
func (s *Service) UpdateMetadata(ctx context.Context, path string, in domain.Song) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	before := domain.Song{}
	if e := scanWorkingSong(s.Store.DB.QueryRowContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE path=? AND deleted=0", path), &before); e != nil {
		return e
	}
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE working_songs SET title=?,artist=?,album=?,album_artist=?,composer=?,genre=?,lyrics=?,track_no=?,disc_no=?,year=?,comment=?,duration_ms=?,has_server_changes=1 WHERE path=?", in.Title, in.Artist, in.Album, in.AlbumArtist, in.Composer, in.Genre, in.Lyrics, in.TrackNo, in.DiscNo, in.Year, in.Comment, in.DurationMS, path)
	if e != nil {
		return e
	}
	in.Path = path
	return s.recordChangeLocked(ctx, "song", path, "METADATA", before, in)
}
func (s *Service) DeleteSong(ctx context.Context, path string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var before domain.Song
	if e := scanWorkingSong(s.Store.DB.QueryRowContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE path=? AND deleted=0", path), &before); e != nil {
		return e
	}
	if e := s.Store.Tx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.ExecContext(ctx, "DELETE FROM working_playlist_items WHERE path=?", path)
		_, _ = tx.ExecContext(ctx, "DELETE FROM working_queue_items WHERE path=?", path)
		_, _ = tx.ExecContext(ctx, "DELETE FROM working_favorites WHERE path=?", path)
		_, _ = tx.ExecContext(ctx, "UPDATE working_songs SET deleted=1,has_server_changes=1 WHERE path=?", path)
		return nil
	}); e != nil {
		return e
	}
	return s.recordChangeLocked(ctx, "song", path, "DELETE", before, nil)
}
func (s *Service) IncrementPlay(ctx context.Context, path string, at time.Time) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var old int64
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT play_count FROM working_songs WHERE path=? AND deleted=0", path).Scan(&old); e != nil {
		return e
	}
	stamp := at.Truncate(time.Second).UnixMilli()
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE working_songs SET play_count=play_count+1,last_played_ms=MAX(last_played_ms,?),has_server_changes=1 WHERE path=?", stamp, path)
	if e != nil {
		return e
	}
	yearKey := fmt.Sprintf("PCs_Y_%04d", at.Year())
	monthKey := fmt.Sprintf("PCs_M_%04d.%d", at.Year(), int(at.Month())-1)
	for _, key := range []string{yearKey, monthKey} {
		_, _ = s.Store.DB.ExecContext(ctx, "INSERT INTO working_period_counts(period_key,path,count,base_import_count,last_resolve_count) VALUES(?,?,1,0,0) ON CONFLICT(period_key,path) DO UPDATE SET count=count+1", key, path)
	}
	return s.recordChangeLocked(ctx, "song", path, "PLAY", old, old+1)
}
