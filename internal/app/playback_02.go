package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Ltre/MusicoletWeb/internal/db"
)

func (s *Service) QueueMove(ctx context.Context, qid int64, path string, pos int) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); err != nil {
		return err
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
	return s.applyChangeLocked(ctx, "queue", qname, "MOVE", before, after, func(tx *sql.Tx) error {
		return rewriteQueueTx(ctx, tx, qid, after)
	}, [2]string{"song", path})
}

func (s *Service) DeleteQueue(ctx context.Context, qid int64) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var qname string
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&qname); err != nil {
		return err
	}
	pv, _ := s.Playback(ctx)
	before := queuePaths(ctx, s.Store.DB, qid)
	var next int64
	var nextPath string
	var nextMS int64
	if pv.QueueID == qid {
		_ = s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE sort_position>(SELECT sort_position FROM working_queues WHERE id=?) ORDER BY sort_position LIMIT 1", qid).Scan(&next)
		if next == 0 {
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_queues WHERE id<>? ORDER BY sort_position LIMIT 1", qid).Scan(&next)
		}
		if next != 0 {
			_ = s.Store.DB.QueryRowContext(ctx, "SELECT COALESCE(current_path,''),position_ms FROM queue_playback_state WHERE queue_id=?", next).Scan(&nextPath, &nextMS)
		}
	}
	return s.applyChangeLocked(ctx, "queue", qname, "DELETE", before, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM working_queues WHERE id=?", qid); err != nil {
			return err
		}
		if pv.QueueID != qid {
			return nil
		}
		if next == 0 {
			_, err := tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=NULL,playing=0,updated_at=? WHERE singleton=1", db.NowMS())
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO queue_playback_state(queue_id,current_path,position_ms,updated_at) VALUES(?,?,?,?) ON CONFLICT(queue_id) DO UPDATE SET current_path=excluded.current_path,position_ms=excluded.position_ms,updated_at=excluded.updated_at", next, null(nextPath), nextMS, db.NowMS()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE runtime_playback_state SET queue_id=?,playing=1,updated_at=? WHERE singleton=1", next, db.NowMS())
		return err
	})
}

func (s *Service) PlaylistAction(ctx context.Context, name, op, path string, pos int) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var id int64
	err := s.Store.DB.QueryRowContext(ctx, "SELECT id FROM working_playlists WHERE name=?", name).Scan(&id)
	if err == sql.ErrNoRows && op == "create" {
		return s.applyChangeLocked(ctx, "playlist", name, "CREATE", nil, name, func(tx *sql.Tx) error {
			r, err := tx.ExecContext(ctx, "INSERT INTO working_playlists(name,has_server_changes) VALUES(?,0)", name)
			if err != nil {
				return err
			}
			id, err = r.LastInsertId()
			return err
		})
	}
	if err != nil {
		return err
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
		return s.applyChangeLocked(ctx, "playlist", name, "DELETE", before, nil, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM working_playlists WHERE id=?", id)
			return err
		})
	default:
		return errors.New("invalid playlist operation")
	}
	return s.applyChangeLocked(ctx, "playlist", name, strings.ToUpper(op), before, after, func(tx *sql.Tx) error {
		return rewritePlaylistTx(ctx, tx, id, after)
	}, [2]string{"song", path})
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
		_ = rows.Scan(&p)
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
		_ = rows.Scan(&p)
		r = append(r, p)
	}
	return r
}

func rewriteQueue(ctx context.Context, q *sql.DB, id int64, a []string) error {
	tx, err := q.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = rewriteQueueTx(ctx, tx, id, a); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func rewriteQueueTx(ctx context.Context, tx *sql.Tx, id int64, a []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM working_queue_items WHERE queue_id=?", id); err != nil {
		return err
	}
	for i, p := range unique(a) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO working_queue_items(queue_id,path,position) VALUES(?,?,?)", id, p, i); err != nil {
			return err
		}
	}
	return nil
}

func rewritePlaylist(ctx context.Context, q *sql.DB, id int64, a []string) error {
	tx, err := q.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = rewritePlaylistTx(ctx, tx, id, a); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func rewritePlaylistTx(ctx context.Context, tx *sql.Tx, id int64, a []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM working_playlist_items WHERE playlist_id=?", id); err != nil {
		return err
	}
	for i, p := range unique(a) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO working_playlist_items(playlist_id,path,position) VALUES(?,?,?)", id, p, i); err != nil {
			return err
		}
	}
	return nil
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

func (s *Service) RenameQueue(ctx context.Context, qid int64, newName string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("queue name required")
	}
	var old string
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&old); err != nil {
		return err
	}
	if old == newName {
		return nil
	}
	return s.applyChangeLocked(ctx, "queue", old, "RENAME", map[string]any{"name": old}, map[string]any{"name": newName}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE working_queues SET name=? WHERE id=?", newName, qid)
		return err
	}, [2]string{"queue", newName})
}

func (s *Service) ReorderQueues(ctx context.Context, qid int64, pos int) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	rows, err := s.Store.DB.QueryContext(ctx, "SELECT id,name FROM working_queues ORDER BY sort_position,id")
	if err != nil {
		return err
	}
	type entry struct {
		id   int64
		name string
	}
	var a []entry
	for rows.Next() {
		var x entry
		if err = rows.Scan(&x.id, &x.name); err != nil {
			rows.Close()
			return err
		}
		a = append(a, x)
	}
	rows.Close()
	idx := -1
	for i, x := range a {
		if x.id == qid {
			idx = i
			break
		}
	}
	if idx < 0 {
		return sql.ErrNoRows
	}
	before := make([]string, len(a))
	for i, x := range a {
		before[i] = x.name
	}
	x := a[idx]
	a = append(a[:idx], a[idx+1:]...)
	if pos < 0 {
		pos = 0
	}
	if pos > len(a) {
		pos = len(a)
	}
	a = append(a, entry{})
	copy(a[pos+1:], a[pos:])
	a[pos] = x
	after := make([]string, len(a))
	for i, item := range a {
		after[i] = item.name
	}
	return s.applyChangeLocked(ctx, "queue_order", "all", "MOVE_QUEUE", before, after, func(tx *sql.Tx) error {
		for i, item := range a {
			if _, err := tx.ExecContext(ctx, "UPDATE working_queues SET sort_position=? WHERE id=?", i, item.id); err != nil {
				return err
			}
		}
		return nil
	}, [2]string{"queue", x.name})
}

func (s *Service) QueueReorderItems(ctx context.Context, qid int64, order []string, operation string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	var name string
	if err := s.Store.DB.QueryRowContext(ctx, "SELECT name FROM working_queues WHERE id=?", qid).Scan(&name); err != nil {
		return err
	}
	before := queuePaths(ctx, s.Store.DB, qid)
	order = unique(order)
	if len(before) != len(order) {
		return errors.New("queue reorder must contain every queue song exactly once")
	}
	set := map[string]bool{}
	for _, p := range before {
		set[p] = true
	}
	for _, p := range order {
		if !set[p] {
			return errors.New("queue reorder contains unknown song")
		}
	}
	if operation == "" {
		operation = "REORDER"
	}
	return s.applyChangeLocked(ctx, "queue", name, operation, before, order, func(tx *sql.Tx) error {
		return rewriteQueueTx(ctx, tx, qid, order)
	})
}
