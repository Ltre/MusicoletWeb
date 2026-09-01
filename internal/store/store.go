package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB       *sql.DB
	history  History
	mutation sync.Mutex
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + url.PathEscape(filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL"} {
		if _, err = db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err = db.Exec(schemaV1); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if _, err = db.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES(1)"); err != nil {
		db.Close()
		return nil, err
	}
	var hasV2 int
	if err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=2").Scan(&hasV2); err != nil {
		db.Close()
		return nil, err
	}
	if hasV2 == 0 {
		var columnExists bool
		rows, columnErr := db.Query("PRAGMA table_info(musicolet_versions)")
		if columnErr != nil {
			db.Close()
			return nil, columnErr
		}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, dataType string
			var defaultValue any
			if columnErr = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); columnErr != nil {
				rows.Close()
				db.Close()
				return nil, columnErr
			}
			columnExists = columnExists || name == "current_queue_index"
		}
		rows.Close()
		if !columnExists {
			if _, err = db.Exec(schemaV2); err != nil {
				db.Close()
				return nil, fmt.Errorf("migrate schema v2: %w", err)
			}
		}
		if _, err = db.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES(2)"); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) ServerRevision(ctx context.Context) (int64, error) {
	var raw string
	err := s.DB.QueryRowContext(ctx, "SELECT value FROM app_state WHERE key='server_revision'").Scan(&raw)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func nextRevision(ctx context.Context, tx *sql.Tx) (int64, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM app_state WHERE key='server_revision'").Scan(&raw); err != nil {
		return 0, err
	}
	rev, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	rev++
	_, err = tx.ExecContext(ctx, "UPDATE app_state SET value=? WHERE key='server_revision'", strconv.FormatInt(rev, 10))
	return rev, err
}

func (s *Store) CurrentVersion(ctx context.Context) (id, number int64, err error) {
	err = s.DB.QueryRowContext(ctx, "SELECT id,version_number FROM musicolet_versions ORDER BY version_number DESC LIMIT 1").Scan(&id, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return
}

func (s *Store) LoadVersion(ctx context.Context, versionID int64) (domain.Snapshot, error) {
	return loadSnapshot(ctx, s.DB, true, versionID)
}

func (s *Store) LoadWorking(ctx context.Context) (domain.Snapshot, error) {
	return loadSnapshot(ctx, s.DB, false, 0)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadSnapshot(ctx context.Context, q queryer, historical bool, versionID int64) (domain.Snapshot, error) {
	snap := domain.NewSnapshot()
	var rows *sql.Rows
	var err error
	if historical {
		rows, err = q.QueryContext(ctx, "SELECT path,core_json,favorite FROM songs_snapshot WHERE version_id=? ORDER BY path", versionID)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT file_id,path,core_json,favorite,deleted,has_server_changes FROM songs ORDER BY path")
	}
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var song domain.Song
		var raw []byte
		var fav int
		if historical {
			err = rows.Scan(&song.Path, &raw, &fav)
		} else {
			var deleted, changed int
			err = rows.Scan(&song.ID, &song.Path, &raw, &fav, &deleted, &changed)
			song.Deleted, song.HasServerChanges = deleted != 0, changed != 0
		}
		if err != nil {
			rows.Close()
			return snap, err
		}
		if err = json.Unmarshal(raw, &song); err != nil {
			rows.Close()
			return snap, err
		}
		song.Favorite = fav != 0
		snap.Songs[song.Path] = song
	}
	if err = rows.Close(); err != nil {
		return snap, err
	}

	if historical {
		rows, err = q.QueryContext(ctx, "SELECT source_key,name FROM playlists_snapshot WHERE version_id=? ORDER BY source_key", versionID)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT id,source_key,name,has_server_changes FROM playlists ORDER BY id", versionID)
	}
	if err != nil {
		return snap, err
	}
	var playlists []domain.OrderedList
	for rows.Next() {
		var list domain.OrderedList
		if historical {
			err = rows.Scan(&list.SourceKey, &list.Name)
		} else {
			var changed int
			err = rows.Scan(&list.ID, &list.SourceKey, &list.Name, &changed)
			list.HasServerChanges = changed != 0
		}
		if err != nil {
			rows.Close()
			return snap, err
		}
		playlists = append(playlists, list)
	}
	if err = rows.Close(); err != nil {
		return snap, err
	}
	for i := range playlists {
		if historical {
			playlists[i].Paths, err = listItems(ctx, q, "playlist_items_snapshot", "source_key", playlists[i].SourceKey, versionID)
		} else {
			playlists[i].Paths, err = listItems(ctx, q, "playlist_items", "playlist_id", playlists[i].ID, 0)
		}
		if err != nil {
			return snap, err
		}
	}
	snap.Playlists = playlists

	if historical {
		rows, err = q.QueryContext(ctx, "SELECT source_key,name,position,current_index,progress_ms,stop_path FROM queues_snapshot WHERE version_id=? ORDER BY position", versionID)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT id,source_key,name,position,current_index,progress_ms,stop_path,has_server_changes FROM queues ORDER BY position")
	}
	if err != nil {
		return snap, err
	}
	var queues []domain.Queue
	for rows.Next() {
		var queue domain.Queue
		if historical {
			err = rows.Scan(&queue.SourceKey, &queue.Name, &queue.Position, &queue.Current, &queue.ProgressMS, &queue.StopPath)
		} else {
			var changed int
			err = rows.Scan(&queue.ID, &queue.SourceKey, &queue.Name, &queue.Position, &queue.Current, &queue.ProgressMS, &queue.StopPath, &changed)
			queue.HasServerChanges = changed != 0
		}
		if err != nil {
			rows.Close()
			return snap, err
		}
		queues = append(queues, queue)
	}
	if err = rows.Close(); err != nil {
		return snap, err
	}
	for i := range queues {
		if historical {
			queues[i].Paths, err = listItems(ctx, q, "queue_items_snapshot", "source_key", queues[i].SourceKey, versionID)
		} else {
			queues[i].Paths, err = listItems(ctx, q, "queue_items", "queue_id", queues[i].ID, 0)
		}
		if err != nil {
			return snap, err
		}
	}
	snap.Queues = queues
	if historical {
		if err = q.QueryRowContext(ctx, "SELECT current_queue_index FROM musicolet_versions WHERE id=?", versionID).Scan(&snap.CurrentQueueIndex); err != nil {
			return snap, err
		}
	} else {
		var currentQueueID int64
		if err = q.QueryRowContext(ctx, "SELECT queue_id FROM runtime_playback_state WHERE singleton=1").Scan(&currentQueueID); err != nil {
			return snap, err
		}
		snap.CurrentQueueIndex = -1
		for i := range queues {
			if queues[i].ID == currentQueueID {
				snap.CurrentQueueIndex = i
				break
			}
		}
	}

	if historical {
		rows, err = q.QueryContext(ctx, "SELECT song_path,total,last_played,weekly_json,monthly_json,yearly_json FROM playcount_snapshot WHERE version_id=?", versionID)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT song_path,total,last_played,weekly_json,monthly_json,yearly_json FROM playback_stats")
	}
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var stat domain.PlaybackStats
		var weekly, monthly, yearly []byte
		if err = rows.Scan(&stat.Path, &stat.Total, &stat.LastPlayed, &weekly, &monthly, &yearly); err != nil {
			rows.Close()
			return snap, err
		}
		_ = json.Unmarshal(weekly, &stat.Weekly)
		_ = json.Unmarshal(monthly, &stat.Monthly)
		_ = json.Unmarshal(yearly, &stat.Yearly)
		snap.Stats[stat.Path] = stat
	}
	rows.Close()

	if historical {
		rows, err = q.QueryContext(ctx, "SELECT setting_key,value_json FROM settings_snapshot WHERE version_id=? ORDER BY setting_key", versionID)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT setting_key,value_json FROM settings ORDER BY setting_key")
	}
	if err != nil {
		return snap, err
	}
	for rows.Next() {
		var key string
		var value []byte
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return snap, err
		}
		snap.Settings[key] = append(json.RawMessage(nil), value...)
	}
	if err = rows.Close(); err != nil {
		return snap, err
	}
	return snap, nil
}

func listItems(ctx context.Context, q queryer, table, key string, value any, versionID int64) ([]string, error) {
	query := "SELECT song_path FROM " + table + " WHERE " + key + "=?"
	args := []any{value}
	if versionID > 0 {
		query += " AND version_id=?"
		args = append(args, versionID)
	}
	query += " ORDER BY position"
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func marshalStable(snap domain.Snapshot) ([]byte, string, error) {
	snap.Normalize()
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func (s *Store) PutCandidate(ctx context.Context, procedureID string, snap domain.Snapshot, warnings []string) (string, error) {
	raw, hash, err := marshalStable(snap)
	if err != nil {
		return "", err
	}
	warningsRaw, _ := json.Marshal(warnings)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM import_procedures WHERE id=?", procedureID).Scan(&status); err != nil {
		return "", err
	}
	if status != "PARSING" {
		return "", fmt.Errorf("procedure is no longer parsing: %s", status)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO candidate_snapshots(procedure_id,sha256,canonical_json) VALUES(?,?,?) ON CONFLICT(procedure_id) DO UPDATE SET sha256=excluded.sha256,canonical_json=excluded.canonical_json", procedureID, hash, raw); err != nil {
		return "", err
	}
	res, err := tx.ExecContext(ctx, "UPDATE import_procedures SET status='REVIEWING',candidate_sha256=?,candidate_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='PARSING'", hash, raw, procedureID)
	if err != nil {
		return "", err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil || n != 1 {
		if rowsErr != nil {
			return "", rowsErr
		}
		return "", errors.New("procedure is no longer parsing")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE parser_runs SET status='SUCCEEDED',warnings_json=?,finished_at=CURRENT_TIMESTAMP WHERE procedure_id=? AND finished_at IS NULL", warningsRaw, procedureID); err != nil {
		return "", err
	}
	return hash, tx.Commit()
}

func (s *Store) LoadCandidate(ctx context.Context, procedureID string) (domain.Snapshot, error) {
	var raw []byte
	if err := s.DB.QueryRowContext(ctx, "SELECT canonical_json FROM candidate_snapshots WHERE procedure_id=?", procedureID).Scan(&raw); err != nil {
		return domain.Snapshot{}, err
	}
	var snap domain.Snapshot
	err := json.Unmarshal(raw, &snap)
	return snap, err
}

func (s *Store) CreateProcedure(ctx context.Context, id, artifactDir, parserVersion, archiveHash string, archiveSize int64) error {
	baseID, _, err := s.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO import_procedures(id,status,base_version_id,archive_sha256,artifact_dir,parser_version) VALUES(?,'PARSING',NULLIF(?,0),?,?,?)", id, baseID, archiveHash, artifactDir, parserVersion); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "one_active_import") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errors.New("an unfinished import procedure already exists")
		}
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO import_artifacts(procedure_id,kind,relative_path,sha256,size_bytes) VALUES(?,?,?,?,?)", id, "ORIGINAL_ZIP", "original.zip", archiveHash, archiveSize); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO parser_runs(procedure_id,parser_version,status) VALUES(?,?,'RUNNING')", id, parserVersion); err != nil {
		return err
	}
	return tx.Commit()
}

type Procedure struct {
	ID, Status, ArchiveSHA256, ParserVersion, CandidateSHA256, Error, CreatedAt, UpdatedAt string
	BaseVersionID                                                                          int64
	ServerRevision                                                                         int64
}

func (s *Store) Procedures(ctx context.Context) ([]Procedure, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,status,COALESCE(base_version_id,0),archive_sha256,parser_version,candidate_sha256,error,created_at,updated_at,last_analyzed_server_revision FROM import_procedures ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Procedure
	for rows.Next() {
		var p Procedure
		if err = rows.Scan(&p.ID, &p.Status, &p.BaseVersionID, &p.ArchiveSHA256, &p.ParserVersion, &p.CandidateSHA256, &p.Error, &p.CreatedAt, &p.UpdatedAt, &p.ServerRevision); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) FailProcedure(ctx context.Context, id string, parseErr error) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE import_procedures SET status='FAILED',error=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT')", parseErr.Error(), id)
	return err
}
func (s *Store) CancelProcedure(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE import_procedures SET status='CANCELLED',cancelled_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT')", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("procedure is not active")
	}
	return nil
}

func SnapshotHash(snap domain.Snapshot) (string, error) {
	_, hash, err := marshalStable(snap)
	return hash, err
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ = time.Now
