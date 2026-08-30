package db

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct{ DB *sql.DB }

func Open(dataDir string) (*Store, error) {
	p := filepath.Join(dataDir, "musicolet.db")
	dsn := "file:" + filepath.ToSlash(p) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{db}
	if err = s.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
func (s *Store) LatestVersion(ctx context.Context) (id int64, snapshotID int64, versionNo int64, err error) {
	err = s.DB.QueryRowContext(ctx, "SELECT id,snapshot_id,version_no FROM musicolet_versions ORDER BY version_no DESC LIMIT 1").Scan(&id, &snapshotID, &versionNo)
	if err == sql.ErrNoRows {
		err = nil
	}
	return
}
func (s *Store) ActiveProcedure(ctx context.Context) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM import_procedures WHERE status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT','COMMITTING') ORDER BY id DESC LIMIT 1").Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
func (s *Store) ServerHead(ctx context.Context) (int64, error) {
	var x sql.NullInt64
	err := s.DB.QueryRowContext(ctx, "SELECT MAX(id) FROM server_changes").Scan(&x)
	if err != nil {
		return 0, err
	}
	if !x.Valid {
		return 0, nil
	}
	return x.Int64, nil
}
func EnsureDirs(dataDir string) error {
	for _, d := range []string{"imports", "cache", "git"} {
		if err := os.MkdirAll(filepath.Join(dataDir, d), 0o700); err != nil {
			return err
		}
	}
	return nil
}
func NowMS() int64 { return time.Now().UnixMilli() }
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}
func CheckAffected(r sql.Result) error {
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return fmt.Errorf("no rows affected")
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS snapshots(id INTEGER PRIMARY KEY AUTOINCREMENT, procedure_id INTEGER, state TEXT NOT NULL, parser_version TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS musicolet_versions(id INTEGER PRIMARY KEY AUTOINCREMENT, version_no INTEGER NOT NULL UNIQUE, snapshot_id INTEGER NOT NULL UNIQUE, source_zip_sha256 TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS import_procedures(id INTEGER PRIMARY KEY AUTOINCREMENT, status TEXT NOT NULL, base_version_id INTEGER, candidate_snapshot_id INTEGER, source_zip_path TEXT NOT NULL, source_zip_sha256 TEXT NOT NULL, last_analyzed_server_head INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, committed_at INTEGER, cancelled_at INTEGER);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_procedure ON import_procedures((1)) WHERE status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT','COMMITTING');
CREATE TABLE IF NOT EXISTS import_artifacts(id INTEGER PRIMARY KEY AUTOINCREMENT, procedure_id INTEGER NOT NULL, kind TEXT NOT NULL, path TEXT NOT NULL, sha256 TEXT, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS parser_runs(id INTEGER PRIMARY KEY AUTOINCREMENT, procedure_id INTEGER NOT NULL, parser_version TEXT NOT NULL, status TEXT NOT NULL, report_json TEXT, error_text TEXT, started_at INTEGER NOT NULL, finished_at INTEGER);
CREATE TABLE IF NOT EXISTS snapshot_settings(snapshot_id INTEGER NOT NULL, key TEXT NOT NULL, value_json TEXT NOT NULL, PRIMARY KEY(snapshot_id,key));
CREATE TABLE IF NOT EXISTS conflict_resolutions(id INTEGER PRIMARY KEY AUTOINCREMENT, conflict_id INTEGER NOT NULL, procedure_id INTEGER NOT NULL, target_type TEXT NOT NULL, target_key TEXT NOT NULL, resolution TEXT NOT NULL, server_head INTEGER NOT NULL, base_json TEXT, ours_json TEXT, theirs_json TEXT, result_json TEXT, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS resolution_patches(id INTEGER PRIMARY KEY AUTOINCREMENT, resolution_id INTEGER NOT NULL, patch_json TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS secure_settings(key TEXT PRIMARY KEY, ciphertext BLOB NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS snapshot_songs(snapshot_id INTEGER NOT NULL,path TEXT NOT NULL,title TEXT,artist TEXT,album TEXT,album_artist TEXT,composer TEXT,genre TEXT,lyrics TEXT,track_no TEXT,disc_no TEXT,year TEXT,comment TEXT,duration_ms INTEGER,file_name TEXT,folder TEXT,modified_ms INTEGER,added_ms INTEGER,last_played_ms INTEGER,play_count INTEGER,raw_json TEXT,PRIMARY KEY(snapshot_id,path));
CREATE TABLE IF NOT EXISTS snapshot_playlists(snapshot_id INTEGER NOT NULL,name TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(snapshot_id,name));
CREATE TABLE IF NOT EXISTS snapshot_playlist_items(snapshot_id INTEGER NOT NULL,playlist_name TEXT NOT NULL,path TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(snapshot_id,playlist_name,path));
CREATE TABLE IF NOT EXISTS snapshot_queues(snapshot_id INTEGER NOT NULL,name TEXT NOT NULL,position INTEGER NOT NULL,current_index INTEGER NOT NULL DEFAULT 0,position_ms INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(snapshot_id,name));
CREATE TABLE IF NOT EXISTS snapshot_queue_items(snapshot_id INTEGER NOT NULL,queue_name TEXT NOT NULL,path TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(snapshot_id,queue_name,path));
CREATE TABLE IF NOT EXISTS snapshot_favorites(snapshot_id INTEGER NOT NULL,path TEXT NOT NULL,PRIMARY KEY(snapshot_id,path));
CREATE TABLE IF NOT EXISTS snapshot_period_counts(snapshot_id INTEGER NOT NULL,period_key TEXT NOT NULL,path TEXT NOT NULL,count INTEGER NOT NULL,PRIMARY KEY(snapshot_id,period_key,path));
CREATE TABLE IF NOT EXISTS snapshot_current_counts(snapshot_id INTEGER NOT NULL,path TEXT NOT NULL,week_count INTEGER NOT NULL DEFAULT 0,month_count INTEGER NOT NULL DEFAULT 0,year_count INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(snapshot_id,path));
CREATE TABLE IF NOT EXISTS snapshot_raw_files(snapshot_id INTEGER NOT NULL,name TEXT NOT NULL,canonical_text TEXT NOT NULL,PRIMARY KEY(snapshot_id,name));
CREATE TABLE IF NOT EXISTS working_songs(file_id INTEGER PRIMARY KEY AUTOINCREMENT,path TEXT NOT NULL UNIQUE,title TEXT,artist TEXT,album TEXT,album_artist TEXT,composer TEXT,genre TEXT,lyrics TEXT,track_no TEXT,disc_no TEXT,year TEXT,comment TEXT,duration_ms INTEGER,file_name TEXT,folder TEXT,modified_ms INTEGER,added_ms INTEGER,last_played_ms INTEGER,play_count INTEGER,raw_json TEXT,has_server_changes INTEGER NOT NULL DEFAULT 0,deleted INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS working_playlists(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL UNIQUE,has_server_changes INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS working_playlist_items(playlist_id INTEGER NOT NULL,path TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(playlist_id,path),FOREIGN KEY(playlist_id) REFERENCES working_playlists(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS working_queues(id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL UNIQUE,sort_position INTEGER NOT NULL DEFAULT 0,source_type TEXT,source_key TEXT,has_server_changes INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_source ON working_queues(source_type,source_key) WHERE source_type IS NOT NULL AND source_type<>'' AND source_key IS NOT NULL AND source_key<>'';
CREATE TABLE IF NOT EXISTS working_queue_items(queue_id INTEGER NOT NULL,path TEXT NOT NULL,position INTEGER NOT NULL,PRIMARY KEY(queue_id,path),FOREIGN KEY(queue_id) REFERENCES working_queues(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS working_favorites(path TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS working_settings(key TEXT PRIMARY KEY, value_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS working_period_counts(period_key TEXT NOT NULL,path TEXT NOT NULL,count INTEGER NOT NULL,base_import_count INTEGER NOT NULL DEFAULT 0,last_resolve_count INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(period_key,path));
CREATE TABLE IF NOT EXISTS working_current_counts(path TEXT PRIMARY KEY,week_count INTEGER NOT NULL DEFAULT 0,month_count INTEGER NOT NULL DEFAULT 0,year_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS queue_playback_state(queue_id INTEGER PRIMARY KEY,current_path TEXT,position_ms INTEGER NOT NULL DEFAULT 0,stop_path TEXT,updated_at INTEGER NOT NULL,FOREIGN KEY(queue_id) REFERENCES working_queues(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS runtime_playback_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),queue_id INTEGER,playing INTEGER NOT NULL DEFAULT 0,shuffle INTEGER NOT NULL DEFAULT 0,loop_mode TEXT NOT NULL DEFAULT 'list',speed REAL NOT NULL DEFAULT 1.0,updated_at INTEGER NOT NULL);
INSERT OR IGNORE INTO runtime_playback_state(singleton,updated_at) VALUES(1,0);
CREATE TABLE IF NOT EXISTS server_changes(id INTEGER PRIMARY KEY AUTOINCREMENT,base_version_id INTEGER,target_type TEXT NOT NULL,target_key TEXT NOT NULL,operation TEXT NOT NULL,before_json TEXT,after_json TEXT,git_commit TEXT,active INTEGER NOT NULL DEFAULT 1,created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS change_targets(change_id INTEGER NOT NULL,target_type TEXT NOT NULL,target_key TEXT NOT NULL,PRIMARY KEY(change_id,target_type,target_key));
CREATE TABLE IF NOT EXISTS semantic_diffs(id INTEGER PRIMARY KEY AUTOINCREMENT,procedure_id INTEGER NOT NULL,target_type TEXT NOT NULL,target_key TEXT NOT NULL,operation TEXT NOT NULL,base_json TEXT,ours_json TEXT,theirs_json TEXT,conflict INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS merge_conflicts(id INTEGER PRIMARY KEY AUTOINCREMENT,procedure_id INTEGER NOT NULL,diff_id INTEGER,target_type TEXT NOT NULL,target_key TEXT NOT NULL,base_json TEXT,ours_json TEXT,theirs_json TEXT,status TEXT NOT NULL DEFAULT 'UNRESOLVED',resolved_server_head INTEGER,resolution TEXT,manual_json TEXT,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS commit_journal(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,procedure_id INTEGER,target_version_no INTEGER,state_json TEXT NOT NULL,source_parent TEXT,main_parent TEXT,source_commit TEXT,main_commit TEXT,status TEXT NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS share_records(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,target_key TEXT NOT NULL,token TEXT NOT NULL UNIQUE,created_at INTEGER NOT NULL);
`
