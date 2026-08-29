package app

import (
	"context"
	"database/sql"
	"github.com/Ltre/MusicoletWeb/internal/domain"
)

func loadWorkingPlaylists(ctx context.Context, q *sql.DB) ([]domain.Playlist, error) {
	rows, e := q.QueryContext(ctx, "SELECT id,name FROM working_playlists ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []domain.Playlist
	for rows.Next() {
		var id int64
		var x domain.Playlist
		rows.Scan(&id, &x.Name)
		x.ID = id
		ir, _ := q.QueryContext(ctx, "SELECT path FROM working_playlist_items WHERE playlist_id=? ORDER BY position", id)
		if ir != nil {
			for ir.Next() {
				var p string
				ir.Scan(&p)
				x.Paths = append(x.Paths, p)
			}
			ir.Close()
		}
		out = append(out, x)
	}
	return out, nil
}

func loadWorkingQueues(ctx context.Context, q *sql.DB) ([]domain.Queue, error) {
	rows, e := q.QueryContext(ctx, "SELECT id,name,COALESCE(source_type,''),COALESCE(source_key,'') FROM working_queues ORDER BY sort_position,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []domain.Queue
	for rows.Next() {
		var id int64
		var x domain.Queue
		rows.Scan(&id, &x.Name, &x.SourceType, &x.SourceKey)
		x.ID = id
		ir, _ := q.QueryContext(ctx, "SELECT path FROM working_queue_items WHERE queue_id=? ORDER BY position", id)
		if ir != nil {
			for ir.Next() {
				var p string
				ir.Scan(&p)
				x.Paths = append(x.Paths, p)
			}
			ir.Close()
		}
		var current string
		_ = q.QueryRowContext(ctx, "SELECT COALESCE(current_path,''),position_ms,COALESCE(stop_path,'') FROM queue_playback_state WHERE queue_id=?", id).Scan(&current, &x.PositionMS, &x.StopPath)
		for i, path := range x.Paths {
			if path == current {
				x.CurrentIndex = i
				break
			}
		}
		out = append(out, x)
	}
	return out, nil
}

type PlaybackView struct {
	QueueID    int64        `json:"queue_id"`
	QueueName  string       `json:"queue_name"`
	Path       string       `json:"path"`
	PositionMS int64        `json:"position_ms"`
	Playing    bool         `json:"playing"`
	Shuffle    bool         `json:"shuffle"`
	LoopMode   string       `json:"loop_mode"`
	Speed      float64      `json:"speed"`
	StopPath   string       `json:"stop_path"`
	Song       *domain.Song `json:"song,omitempty"`
}
