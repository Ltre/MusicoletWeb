package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Service) QueueMove(ctx context.Context, qid int64, path string, pos int) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); e != nil {
		return e
	}
	before := queuePaths(ctx, s.Store.DB, qid)
	after := removeString(append([]string{}, before...), path)
	if pos < 0 {
		pos = 0
	}
	if pos > len(after) {
		pos = len(after)
	}
	after = insertString(after, path, pos)
	if e := rewriteQueue(ctx, s.Store.DB, qid, after); e != nil {
		return e
	}
	return s.recordChangeLocked(ctx, "queue", qname, "MOVE", before, after, [2]string{"song", path})
}

func (s *Service) DeleteQueue(ctx context.Context, qid int64) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if e := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); e != nil {
		return e
	}
	pv, _ := s.Playback(ctx)
	before := queuePaths(ctx, s.Store.DB, qid)
	var next int64
	if pv.QueueID == qid {
		_ = s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE sort_position>(SELECT sort_position FROM working_queues WHERE id=?) ORDER BY sort_position LIMIT 1", qid).Scan(&next)
		if next == 0 {
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE id<>? ORDER BY sort_position LIMIT 1", qid).Scan(&next)
		}
	}
	if _, e := s.Store.DB.ExecContext(ctx, "DELETE FROM working_queues WHERE id=?", qid); e != nil {
		return e
	}
	if pv.QueueID == qid {
		if next != 0 {
			var p string
			var ms int64
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(current_path,''),position_ms FROM queue_playback_state WHERE queue_id=?", next).Scan(&p, &ms)
			_ = s.SetPlayback(ctx, next, p, ms, true)
		} else {
			_, _ = s.Store.DB.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=NULL,playing=0 WHERE singleton=1")
		}
	}
	return s.recordChangeLocked(ctx, "queue", qname, "DELETE", before, nil)
}

func (s *Service) PlaylistAction(ctx context.Context, name, op, path string, pos int) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var id int64
	e := s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_playlists WHERE name=?", name).Scan(&id)
	if e == sql.ErrNoRows && op == "create" {
		r, e := s.Store.DB.ExecContext(ctx, "INSERT INTO working_playlists(name,has_server_changes) VALUES(?,1)", name)
		if e != nil {
			return e
		}
		id, _ = r.LastInsertId()
		return s.recordChangeLocked(ctx, "playlist", name, "CREATE", nil, name)
	}
	if e != nil {
		return e
	}
	before := playlistPaths(ctx, s.Store.DB, id)
	after := append([]string{}, before...)
	switch op {
	case "add":
		after = removeString(after, path)
		after = append(after, path)
	case "remove":
		after = removeString(after, path)
	case "move":
		after = removeString(after, path)
		if pos < 0 {
			pos = 0
		}
		if pos > len(after) {
			pos = len(after)
		}
		after = insertString(after, path, pos)
	case "delete":
		_, e = s.Store.DB.ExecContext(ctx, "DELETE FROM working_playlists WHERE id=?", id)
		if e != nil {
			return e
		}
		return s.recordChangeLocked(ctx, "playlist", name, "DELETE", before, nil)
	default:
		return errors.New("invalid playlist operation")
	}
	if e = rewritePlaylist(ctx, s.Store.DB, id, after); e != nil {
		return e
	}
	return s.recordChangeLocked(ctx, "playlist", name, strings.ToUpper(op), before, after, [2]string{"song", path})
}

func unique(a []string) []string {
	m := map[string]bool{}
	r := []string{}
	for _, x := range a {
		if x != "" && !m[x] {
			m[x] = true
			r = append(r, x)
		}
	}
	return r
}

func deterministicShuffle(a []string, seed int64) {
	for i := len(a) - 1; i > 0; i-- {
		seed = seed*6364136223846793005 + 1
		j := int((seed >> 33) % int64(i+1))
		if j < 0 {
			j = -j
		}
		a[i], a[j] = a[j], a[i]
	}
}

func queuePaths(ctx context.Context, q *sql.DB, id int64) []string {
	rows, _ := q.QueryContext(ctx, "SELECT path FROM working_queue_items WHERE queue_id=? ORDER BY position", id)
	if rows == nil {
		return nil
	}
	defer rows.Close()
	var r []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		r = append(r, p)
	}
	return r
}

func playlistPaths(ctx context.Context, q *sql.DB, id int64) []string {
	rows, _ := q.QueryContext(ctx, "SELECT path FROM working_playlist_items WHERE playlist_id=? ORDER BY position", id)
	if rows == nil {
		return nil
	}
	defer rows.Close()
	var r []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		r = append(r, p)
	}
	return r
}

func rewriteQueue(ctx context.Context, q *sql.DB, id int64, a []string) error {
	tx, e := q.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM working_queue_items WHERE queue_id=?", id); e != nil {
		tx.Rollback()
		return e
	}
	for i, p := range unique(a) {
		if _, e = tx.ExecContext(ctx, "INSERT INTO working_queue_items VALUES(?,?,?)", id, p, i); e != nil {
			tx.Rollback()
			return e
		}
	}
	return tx.Commit()
}

func rewritePlaylist(ctx context.Context, q *sql.DB, id int64, a []string) error {
	tx, e := q.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM working_playlist_items WHERE playlist_id=?", id); e != nil {
		tx.Rollback()
		return e
	}
	for i, p := range unique(a) {
		if _, e = tx.ExecContext(ctx, "INSERT INTO working_playlist_items VALUES(?,?,?)", id, p, i); e != nil {
			tx.Rollback()
			return e
		}
	}
	_, _ = tx.ExecContext(ctx, "UPDATE working_playlists SET has_server_changes=1 WHERE id=?", id)
	return tx.Commit()
}

func removeString(a []string, x string) []string {
	r := a[:0]
	for _, v := range a {
		if v != x {
			r = append(r, v)
		}
	}
	return r
}

func insertString(a []string, x string, i int) []string {
	a = append(a, "")
	copy(a[i+1:], a[i:])
	a[i] = x
	return a
}
