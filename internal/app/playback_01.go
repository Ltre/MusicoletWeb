package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"time"
)

func (s *Service) Playback(ctx context.Context) (PlaybackView, error) {
	var v PlaybackView
	var playing, shuffle int
	var qid sql.NullInt64
	e := s.Store.DB.QueryRowContext(ctx, "SELECT queue_id,playing,shuffle,loop_mode,speed FROM runtime_playback_state WHERE singleton=1").Scan(&qid, &playing, &shuffle, &v.LoopMode, &v.Speed)
	if e != nil {
		return v, e
	}
	v.Playing = playing != 0
	v.Shuffle = shuffle != 0
	if !qid.Valid {
		return v, nil
	}
	v.QueueID = qid.Int64
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", v.QueueID).Scan(&v.QueueName)
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(current_path,''),position_ms,COALESCE(stop_path,'') FROM queue_playback_state WHERE queue_id=?", v.QueueID).Scan(&v.Path, &v.PositionMS, &v.StopPath)
	if v.Path != "" {
		x := domain.Song{}
		if scanWorkingSong(s.Store.DB.QueryRowContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE path=? AND deleted=0", v.Path), &x) == nil {
			v.Song = &x
		}
	}
	return v, nil
}

func (s *Service) SetPlayback(ctx context.Context, qid int64, path string, pos int64, playing bool) error {
	if qid == 0 {
		return errors.New("queue_id required")
	}
	if path != "" {
		var ok int
		if e := s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_queue_items WHERE queue_id=? AND path=?)", qid, path).Scan(&ok); e != nil || ok == 0 {
			return errors.New("song is not in queue")
		}
	}
	return s.Store.Tx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO queue_playback_state(queue_id,current_path,position_ms,updated_at) VALUES(?,?,?,?) ON CONFLICT(queue_id) DO UPDATE SET current_path=excluded.current_path,position_ms=excluded.position_ms,updated_at=excluded.updated_at", qid, null(path), pos, db.NowMS())
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,playing=?,updated_at=? WHERE singleton=1", qid, boolInt(playing), db.NowMS())
		return e
	})
}

func (s *Service) SetPlaybackMode(ctx context.Context, shuffle bool, loop string, speed float64) error {
	if loop != "list" && loop != "single" && loop != "stop" {
		return errors.New("invalid loop mode")
	}
	if speed < 0.25 || speed > 2 {
		return errors.New("invalid speed")
	}
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE runtime_playback_state SET shuffle=?,loop_mode=?,speed=?,updated_at=? WHERE singleton=1", boolInt(shuffle), loop, speed, db.NowMS())
	return e
}

func (s *Service) SetStopTarget(ctx context.Context, qid int64, path string) error {
	if path != "" {
		var ok int
		_ = s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_queue_items WHERE queue_id=? AND path=?)", qid, path).Scan(&ok)
		if ok == 0 {
			return errors.New("stop target not in queue")
		}
	}
	_, e := s.Store.DB.ExecContext(ctx, "UPDATE queue_playback_state SET stop_path=?,updated_at=? WHERE queue_id=?", null(path), db.NowMS(), qid)
	return e
}

func (s *Service) EnsureSourceQueue(ctx context.Context, sourceType, sourceKey, name string, paths []string, playPath string, shuffleNew bool) (int64, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qid int64
	var qname string
	e := s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE source_type=? AND source_key=?", sourceType, sourceKey).Scan(&qid)
	created := false
	if e == sql.ErrNoRows {
		created = true
		candidate := name
		if candidate == "" {
			candidate = "Queue"
		}
		for n := 2; ; n++ {
			var exists int
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM working_queues WHERE name=?)", candidate).Scan(&exists)
			if exists == 0 {
				break
			}
			candidate = fmt.Sprintf("%s #%d", name, n)
		}
		r, e := s.Store.DB.ExecContext(ctx, "INSERT INTO working_queues(name,sort_position,source_type,source_key) VALUES(?,COALESCE((SELECT MAX(sort_position)+1 FROM working_queues),0),?,?)", candidate, sourceType, sourceKey)
		if e != nil {
			return 0, e
		}
		qid, _ = r.LastInsertId()
		qname = candidate
		items := unique(paths)
		if shuffleNew {
			deterministicShuffle(items, time.Now().UnixNano())
		}
		for i, p := range items {
			_, _ = s.Store.DB.ExecContext(ctx, "INSERT OR IGNORE INTO working_queue_items(queue_id,path,position) VALUES(?,?,?)", qid, p, i)
		}
	} else if e != nil {
		return 0, e
	} else {
		_ = s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname)
	}
	if playPath == "" {
		var p string
		_ = s.Store.DB.QueryRowContext(ctx, "SELECT path FROM working_queue_items WHERE queue_id=? ORDER BY position LIMIT 1", qid).Scan(&p)
		playPath = p
	}
	if e = s.SetPlayback(ctx, qid, playPath, 0, true); e != nil {
		return 0, e
	}
	if created {
		return qid, s.recordChangeLocked(ctx, "queue", qname, "CREATE_FROM_SOURCE", nil, map[string]any{"source_type": sourceType, "source_key": sourceKey, "name": qname}, [2]string{"song", playPath})
	}
	return qid, nil
}

func (s *Service) QueueAdd(ctx context.Context, qid int64, path string, next bool) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); e != nil {
		return e
	}
	before := queuePaths(ctx, s.Store.DB, qid)
	cur := ""
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(current_path,'') FROM queue_playback_state WHERE queue_id=?", qid).Scan(&cur)
	after := removeString(append([]string{}, before...), path)
	pos := len(after)
	if next {
		for i, p := range after {
			if p == cur {
				pos = i + 1
				break
			}
		}
	}
	after = insertString(after, path, pos)
	if e := rewriteQueue(ctx, s.Store.DB, qid, after); e != nil {
		return e
	}
	return s.recordChangeLocked(ctx, "queue", qname, "ADD_OR_MOVE", before, after, [2]string{"song", path})
}

func (s *Service) QueueRemove(ctx context.Context, qid int64, path string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); e != nil {
		return e
	}
	before := queuePaths(ctx, s.Store.DB, qid)
	after := removeString(append([]string{}, before...), path)
	if e := rewriteQueue(ctx, s.Store.DB, qid, after); e != nil {
		return e
	}
	_, _ = s.Store.DB.ExecContext(ctx, "UPDATE queue_playback_state SET stop_path=NULL WHERE queue_id=? AND stop_path=?", qid, path)
	return s.recordChangeLocked(ctx, "queue", qname, "REMOVE", before, after, [2]string{"song", path})
}
