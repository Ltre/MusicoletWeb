package store

const schemaV1 = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR IGNORE INTO app_state(key,value) VALUES ('server_revision','0');

CREATE TABLE IF NOT EXISTS musicolet_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  version_number INTEGER NOT NULL UNIQUE,
  procedure_id TEXT NOT NULL UNIQUE,
  archive_sha256 TEXT NOT NULL,
  parser_version TEXT NOT NULL,
  snapshot_sha256 TEXT NOT NULL,
  git_commit TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS import_procedures (
  id TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK(status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT','COMMITTED','CANCELLED','FAILED')),
  base_version_id INTEGER REFERENCES musicolet_versions(id),
  archive_sha256 TEXT NOT NULL DEFAULT '',
  artifact_dir TEXT NOT NULL,
  parser_version TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL DEFAULT '',
  candidate_json BLOB,
  merged_json BLOB,
  last_analyzed_server_revision INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  committed_at TEXT,
  cancelled_at TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_import
ON import_procedures((1))
WHERE status IN ('PARSING','REVIEWING','RESOLVING','READY_TO_COMMIT');

CREATE TABLE IF NOT EXISTS import_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  procedure_id TEXT NOT NULL REFERENCES import_procedures(id),
  kind TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  sha256 TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS parser_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  procedure_id TEXT NOT NULL REFERENCES import_procedures(id),
  parser_version TEXT NOT NULL,
  status TEXT NOT NULL,
  warnings_json BLOB NOT NULL DEFAULT '[]',
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);
CREATE TABLE IF NOT EXISTS candidate_snapshots (
  procedure_id TEXT PRIMARY KEY REFERENCES import_procedures(id),
  sha256 TEXT NOT NULL,
  canonical_json BLOB NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS songs_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  path TEXT NOT NULL,
  core_json BLOB NOT NULL,
  favorite INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(version_id,path)
);
CREATE TABLE IF NOT EXISTS playlists_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  source_key TEXT NOT NULL,
  name TEXT NOT NULL,
  PRIMARY KEY(version_id,source_key)
);
CREATE TABLE IF NOT EXISTS playlist_items_snapshot (
  version_id INTEGER NOT NULL,
  source_key TEXT NOT NULL,
  song_path TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY(version_id,source_key,song_path),
  UNIQUE(version_id,source_key,position)
);
CREATE TABLE IF NOT EXISTS queues_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  source_key TEXT NOT NULL,
  name TEXT NOT NULL,
  position INTEGER NOT NULL,
  current_index INTEGER NOT NULL DEFAULT 0,
  progress_ms INTEGER NOT NULL DEFAULT 0,
  stop_path TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(version_id,source_key),
  UNIQUE(version_id,position)
);
CREATE TABLE IF NOT EXISTS queue_items_snapshot (
  version_id INTEGER NOT NULL,
  source_key TEXT NOT NULL,
  song_path TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY(version_id,source_key,song_path),
  UNIQUE(version_id,source_key,position)
);
CREATE TABLE IF NOT EXISTS favorites_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  song_path TEXT NOT NULL,
  PRIMARY KEY(version_id,song_path)
);
CREATE TABLE IF NOT EXISTS playcount_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  song_path TEXT NOT NULL,
  total INTEGER NOT NULL DEFAULT 0,
  last_played INTEGER NOT NULL DEFAULT 0,
  weekly_json BLOB NOT NULL DEFAULT '{}',
  monthly_json BLOB NOT NULL DEFAULT '{}',
  yearly_json BLOB NOT NULL DEFAULT '{}',
  PRIMARY KEY(version_id,song_path)
);
CREATE TABLE IF NOT EXISTS settings_snapshot (
  version_id INTEGER NOT NULL REFERENCES musicolet_versions(id),
  setting_key TEXT NOT NULL,
  value_json BLOB NOT NULL,
  PRIMARY KEY(version_id,setting_key)
);

CREATE TABLE IF NOT EXISTS songs (
  file_id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  core_json BLOB NOT NULL,
  favorite INTEGER NOT NULL DEFAULT 0,
  deleted INTEGER NOT NULL DEFAULT 0,
  has_server_changes INTEGER NOT NULL DEFAULT 0,
  source_version_id INTEGER REFERENCES musicolet_versions(id),
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS playlists (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  personal INTEGER NOT NULL DEFAULT 0,
  has_server_changes INTEGER NOT NULL DEFAULT 0,
  source_version_id INTEGER REFERENCES musicolet_versions(id)
);
CREATE TABLE IF NOT EXISTS playlist_items (
  playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  song_path TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY(playlist_id,song_path),
  UNIQUE(playlist_id,position)
);
CREATE TABLE IF NOT EXISTS queues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  position INTEGER NOT NULL UNIQUE,
  current_index INTEGER NOT NULL DEFAULT 0,
  progress_ms INTEGER NOT NULL DEFAULT 0,
  stop_path TEXT NOT NULL DEFAULT '',
  has_server_changes INTEGER NOT NULL DEFAULT 0,
  source_version_id INTEGER REFERENCES musicolet_versions(id)
);
CREATE TABLE IF NOT EXISTS queue_items (
  queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
  song_path TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY(queue_id,song_path),
  UNIQUE(queue_id,position)
);
CREATE TABLE IF NOT EXISTS playback_stats (
  song_path TEXT PRIMARY KEY,
  total INTEGER NOT NULL DEFAULT 0,
  previous_resolve_total INTEGER NOT NULL DEFAULT 0,
  last_played INTEGER NOT NULL DEFAULT 0,
  weekly_json BLOB NOT NULL DEFAULT '{}',
  monthly_json BLOB NOT NULL DEFAULT '{}',
  yearly_json BLOB NOT NULL DEFAULT '{}',
  source_version_id INTEGER REFERENCES musicolet_versions(id)
);
CREATE TABLE IF NOT EXISTS settings (
  setting_key TEXT PRIMARY KEY,
  value_json BLOB NOT NULL,
  source_version_id INTEGER REFERENCES musicolet_versions(id)
);
CREATE TABLE IF NOT EXISTS runtime_playback_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  queue_id INTEGER NOT NULL DEFAULT 0,
  song_path TEXT NOT NULL DEFAULT '',
  progress_ms INTEGER NOT NULL DEFAULT 0,
  playing INTEGER NOT NULL DEFAULT 0,
  shuffle INTEGER NOT NULL DEFAULT 0,
  repeat_mode TEXT NOT NULL DEFAULT 'list',
  speed REAL NOT NULL DEFAULT 1.0,
  updated_at_ms INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO runtime_playback_state(singleton) VALUES(1);

CREATE TABLE IF NOT EXISTS source_queue_links (
  source_type TEXT NOT NULL,
  source_key TEXT NOT NULL,
  queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
  PRIMARY KEY(source_type,source_key),
  UNIQUE(queue_id)
);
CREATE TABLE IF NOT EXISTS server_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  revision INTEGER NOT NULL UNIQUE,
  base_version_id INTEGER REFERENCES musicolet_versions(id),
  target_type TEXT NOT NULL,
  target_key TEXT NOT NULL,
  operation TEXT NOT NULL,
  before_json BLOB,
  after_json BLOB,
  git_commit TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS change_targets (
  change_id INTEGER NOT NULL REFERENCES server_changes(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_key TEXT NOT NULL,
  detail TEXT NOT NULL,
  PRIMARY KEY(change_id,target_type,target_key,detail)
);
CREATE TABLE IF NOT EXISTS semantic_diffs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  procedure_id TEXT NOT NULL REFERENCES import_procedures(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_key TEXT NOT NULL,
  change_kind TEXT NOT NULL,
  detail_json BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS merge_conflicts (
  id TEXT PRIMARY KEY,
  procedure_id TEXT NOT NULL REFERENCES import_procedures(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_key TEXT NOT NULL,
  reason TEXT NOT NULL,
  base_json BLOB,
  ours_json BLOB,
  theirs_json BLOB,
  stale INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS conflict_resolutions (
  conflict_id TEXT PRIMARY KEY REFERENCES merge_conflicts(id) ON DELETE CASCADE,
  choice TEXT NOT NULL CHECK(choice IN ('OURS','THEIRS','MANUAL')),
  resolved_json BLOB,
  patch_json BLOB NOT NULL DEFAULT '{}',
  server_revision INTEGER NOT NULL,
  stale INTEGER NOT NULL DEFAULT 0,
  resolved_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS git_commits (
  revision INTEGER PRIMARY KEY,
  commit_sha TEXT NOT NULL,
  parent_sha TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS search_history (
  query TEXT PRIMARY KEY,
  searched_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const schemaV2 = `ALTER TABLE musicolet_versions ADD COLUMN current_queue_index INTEGER NOT NULL DEFAULT -1`
