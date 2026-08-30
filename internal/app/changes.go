package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func (s *Service) SetFavorite(ctx context.Context, path string, on bool) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var before bool
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_favorites WHERE path=?)", path).Scan(&before); err != nil {
		return err
	}
	return s.applyChangeLocked(ctx, "song", path, "FAVORITE", before, on, func(tx *sql.Tx) error {
		if on {
			_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO working_favorites(path) VALUES(?)", path)
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM working_favorites WHERE path=?", path)
		return err
	})
}

func (s *Service) UpdateMetadata(ctx context.Context, path string, in domain.Song) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	before := domain.Song{}
	if err := scanWorkingSong(s.Store.DB.QueryRowContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE path=? AND deleted=0", path), &before); err != nil {
		return err
	}
	in.Path = path
	return s.applyChangeLocked(ctx, "song", path, "METADATA", before, in, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE working_songs SET title=?,artist=?,album=?,album_artist=?,composer=?,genre=?,lyrics=?,track_no=?,disc_no=?,year=?,comment=?,duration_ms=?,has_server_changes=1 WHERE path=? AND deleted=0", in.Title, in.Artist, in.Album, in.AlbumArtist, in.Composer, in.Genre, in.Lyrics, in.TrackNo, in.DiscNo, in.Year, in.Comment, in.DurationMS, path)
		return err
	})
}

func (s *Service) DeleteSong(ctx context.Context, path string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var before domain.Song
	if err := scanWorkingSong(s.Store.DB.QueryRowContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE path=? AND deleted=0", path), &before); err != nil {
		return err
	}
	return s.applyChangeLocked(ctx, "song", path, "DELETE", before, nil, func(tx *sql.Tx) error {
		stmts := []string{
			"DELETE FROM working_playlist_items WHERE path=?",
			"DELETE FROM working_queue_items WHERE path=?",
			"DELETE FROM working_favorites WHERE path=?",
			"DELETE FROM working_period_counts WHERE path=?",
			"DELETE FROM working_current_counts WHERE path=?",
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt, path); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE working_songs SET deleted=1,has_server_changes=1 WHERE path=?", path)
		return err
	})
}

func (s *Service) IncrementPlay(ctx context.Context, path string, at time.Time) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var old int64
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT play_count FROM working_songs WHERE path=? AND deleted=0", path).Scan(&old); err != nil {
		return err
	}
	stamp := at.Truncate(time.Second).UnixMilli()
	return s.applyChangeLocked(ctx, "song", path, "PLAY", old, old+1, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE working_songs SET play_count=play_count+1,last_played_ms=MAX(last_played_ms,?),has_server_changes=1 WHERE path=? AND deleted=0", stamp, path); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO working_current_counts(path,week_count,month_count,year_count) VALUES(?,1,1,1) ON CONFLICT(path) DO UPDATE SET week_count=week_count+1,month_count=month_count+1,year_count=year_count+1", path)
		return err
	})
}
