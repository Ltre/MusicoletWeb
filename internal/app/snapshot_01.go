package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"github.com/Ltre/MusicoletWeb/internal/merge"
)

func (s *Service) saveSnapshot(ctx context.Context, pid int64, state string, snap domain.Snapshot) (int64, error) {
	var sid int64
	err := s.Store.Tx(ctx, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, "INSERT INTO snapshots(procedure_id,state,parser_version,created_at,current_queue_index) VALUES(?,?,?,?,?)", pid, state, ParserVersion, db.NowMS(), snap.CurrentQueueIndex)
		if e != nil {
			return e
		}
		sid, e = r.LastInsertId()
		if e != nil {
			return e
		}
		return insertSnapshot(ctx, tx, sid, snap)
	})
	return sid, err
}

func (s *Service) loadSnapshot(ctx context.Context, sid int64) (domain.Snapshot, error) {
	snap := domain.EmptySnapshot()
	_ = s.Store.DB.QueryRowContext(ctx, "SELECT current_queue_index FROM snapshots WHERE id=?", sid).Scan(&snap.CurrentQueueIndex)
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,'') FROM snapshot_songs WHERE snapshot_id=?", sid)
	if e != nil {
		return snap, e
	}
	for rows.Next() {
		var x domain.Song
		var raw string
		if e = rows.Scan(&x.Path, &x.Title, &x.Artist, &x.Album, &x.AlbumArtist, &x.Composer, &x.Genre, &x.Lyrics, &x.TrackNo, &x.DiscNo, &x.Year, &x.Comment, &x.DurationMS, &x.FileName, &x.Folder, &x.ModifiedMS, &x.AddedMS, &x.LastPlayedMS, &x.PlayCount, &raw); e != nil {
			rows.Close()
			return snap, e
		}
		x.Raw = []byte(raw)
		snap.Songs[x.Path] = x
	}
	rows.Close()
	snap.Playlists, _ = loadLists(ctx, s.Store.DB, "snapshot_playlists", "snapshot_playlist_items", "snapshot_id", sid)
	qs, _ := loadQueues(ctx, s.Store.DB, sid)
	snap.Queues = qs
	fr, _ := s.Store.DB.QueryContext(ctx, "SELECT path FROM snapshot_favorites WHERE snapshot_id=?", sid)
	if fr != nil {
		for fr.Next() {
			var p string
			fr.Scan(&p)
			snap.Favorites[p] = true
		}
		fr.Close()
	}
	cr, _ := s.Store.DB.QueryContext(ctx, "SELECT period_key,path,count FROM snapshot_period_counts WHERE snapshot_id=?", sid)
	if cr != nil {
		for cr.Next() {
			var k, p string
			var c int64
			cr.Scan(&k, &p, &c)
			if snap.PeriodCounts[k] == nil {
				snap.PeriodCounts[k] = map[string]int64{}
			}
			snap.PeriodCounts[k][p] = c
		}
		cr.Close()
	}
	ccr, _ := s.Store.DB.QueryContext(ctx, "SELECT path,week_count,month_count,year_count FROM snapshot_current_counts WHERE snapshot_id=?", sid)
	if ccr != nil {
		for ccr.Next() {
			var p string
			var c domain.CurrentCounts
			ccr.Scan(&p, &c.Week, &c.Month, &c.Year)
			snap.CurrentCounts[p] = c
		}
		ccr.Close()
	}
	sr, _ := s.Store.DB.QueryContext(ctx, "SELECT key,value_json FROM snapshot_settings WHERE snapshot_id=?", sid)
	if sr != nil {
		for sr.Next() {
			var k, v string
			sr.Scan(&k, &v)
			snap.Settings[k] = json.RawMessage(v)
		}
		sr.Close()
	}
	rr, _ := s.Store.DB.QueryContext(ctx, "SELECT name,canonical_text FROM snapshot_raw_files WHERE snapshot_id=?", sid)
	if rr != nil {
		for rr.Next() {
			var n, t string
			rr.Scan(&n, &t)
			snap.RawFiles[n] = t
		}
		rr.Close()
	}
	return snap, nil
}

func (s *Service) loadWorking(ctx context.Context) (domain.Snapshot, error) {
	snap := domain.EmptySnapshot()
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT file_id,path,title,artist,album,album_artist,composer,genre,lyrics,track_no,disc_no,year,comment,duration_ms,file_name,folder,modified_ms,added_ms,last_played_ms,play_count,COALESCE(raw_json,''),has_server_changes FROM working_songs WHERE deleted=0")
	if e != nil {
		return snap, e
	}
	for rows.Next() {
		var x domain.Song
		var raw string
		var ch int
		rows.Scan(&x.FileID, &x.Path, &x.Title, &x.Artist, &x.Album, &x.AlbumArtist, &x.Composer, &x.Genre, &x.Lyrics, &x.TrackNo, &x.DiscNo, &x.Year, &x.Comment, &x.DurationMS, &x.FileName, &x.Folder, &x.ModifiedMS, &x.AddedMS, &x.LastPlayedMS, &x.PlayCount, &raw, &ch)
		x.Raw = []byte(raw)
		x.HasServerChanges = ch != 0
		snap.Songs[x.Path] = x
	}
	rows.Close()
	pls, _ := loadWorkingPlaylists(ctx, s.Store.DB)
	snap.Playlists = pls
	qs, _ := loadWorkingQueues(ctx, s.Store.DB)
	snap.Queues = qs
	fr, _ := s.Store.DB.QueryContext(ctx, "SELECT path FROM working_favorites")
	if fr != nil {
		for fr.Next() {
			var p string
			fr.Scan(&p)
			snap.Favorites[p] = true
		}
		fr.Close()
	}
	cr, _ := s.Store.DB.QueryContext(ctx, "SELECT period_key,path,count FROM working_period_counts")
	if cr != nil {
		for cr.Next() {
			var k, p string
			var c int64
			cr.Scan(&k, &p, &c)
			if snap.PeriodCounts[k] == nil {
				snap.PeriodCounts[k] = map[string]int64{}
			}
			snap.PeriodCounts[k][p] = c
		}
		cr.Close()
	}
	ccr, _ := s.Store.DB.QueryContext(ctx, "SELECT path,week_count,month_count,year_count FROM working_current_counts")
	if ccr != nil {
		for ccr.Next() {
			var p string
			var c domain.CurrentCounts
			ccr.Scan(&p, &c.Week, &c.Month, &c.Year)
			snap.CurrentCounts[p] = c
		}
		ccr.Close()
	}
	sr, _ := s.Store.DB.QueryContext(ctx, "SELECT key,value_json FROM working_settings")
	if sr != nil {
		for sr.Next() {
			var k, v string
			sr.Scan(&k, &v)
			snap.Settings[k] = json.RawMessage(v)
		}
		sr.Close()
	}
	return snap, nil
}

func (s *Service) resolveSnapshot(ctx context.Context, pid int64, base, ours, theirs domain.Snapshot) (domain.Snapshot, error) {
	res := domain.EmptySnapshot()
	confs, _ := s.ListConflicts(ctx, pid)
	cm := map[string]ConflictRow{}
	for _, c := range confs {
		cm[c.TargetType+"\x00"+c.TargetKey] = c
	}
	for _, path := range unionSongKeys(base.Songs, ours.Songs, theirs.Songs) {
		b, o, t := ptr(base.Songs, path), ptr(ours.Songs, path), ptr(theirs.Songs, path)
		d := merge.MergeSong(b, o, t)
		var x *domain.Song
		if d.Conflict == nil {
			x = d.Result
		} else {
			c := cm["song\x00"+path]
			x = chooseSong(c, o, t)
		}
		if x != nil {
			z := *x
			if b != nil && t != nil && o != nil {
				z.PlayCount = o.PlayCount + (t.PlayCount - b.PlayCount)
				if t.LastPlayedMS > z.LastPlayedMS {
					z.LastPlayedMS = t.LastPlayedMS
				}
			}
			key := z.Path
			if key == "" {
				key = path
				z.Path = key
			}
			if _, exists := res.Songs[key]; exists && key != path {
				return domain.Snapshot{}, fmt.Errorf("manual song resolution collides with existing path %s", key)
			}
			res.Songs[key] = z
		}
	}
	res.Favorites = mergeFavorites(base.Favorites, ours.Favorites, theirs.Favorites)
	res.Playlists = resolveLists("playlist", base.Playlists, ours.Playlists, theirs.Playlists, cm)
	res.Queues = resolveQueues(base.Queues, ours.Queues, theirs.Queues, cm)
	res.Queues = reorderResolvedQueues(res.Queues, base.Queues, ours.Queues, theirs.Queues, cm)
	for _, p := range unionCurrentCountPaths(base.CurrentCounts, ours.CurrentCounts, theirs.CurrentCounts) {
		if _, ok := res.Songs[p]; !ok {
			continue
		}
		b := base.CurrentCounts[p]
		o := ours.CurrentCounts[p]
		t := theirs.CurrentCounts[p]
		res.CurrentCounts[p] = domain.CurrentCounts{Week: o.Week + (t.Week - b.Week), Month: o.Month + (t.Month - b.Month), Year: o.Year + (t.Year - b.Year)}
	}
	res.Settings = mergeSettings(base.Settings, ours.Settings, theirs.Settings)
	for _, k := range unionPeriodKeys(base.PeriodCounts, ours.PeriodCounts, theirs.PeriodCounts) {
		res.PeriodCounts[k] = map[string]int64{}
		for _, p := range unionCountPaths(base.PeriodCounts[k], ours.PeriodCounts[k], theirs.PeriodCounts[k]) {
			if _, ok := res.Songs[p]; !ok {
				continue
			}
			b := base.PeriodCounts[k][p]
			o := ours.PeriodCounts[k][p]
			t := theirs.PeriodCounts[k][p]
			res.PeriodCounts[k][p] = o + (t - b)
		}
	}
	return res, nil
}

func (s *Service) WorkingSnapshot(ctx context.Context) (domain.Snapshot, error) {
	return s.loadWorking(ctx)
}

func (s *Service) RecordChange(ctx context.Context, targetType, targetKey, operation string, before, after any, targets ...[2]string) error {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	return s.recordChangeLocked(ctx, targetType, targetKey, operation, before, after, targets...)
}
