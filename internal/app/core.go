package app

import (
	"database/sql"
	"encoding/json"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
	"sync"
)

const ParserVersion = "2609A-1"

type Service struct {
	Store    *db.Store
	Git      *gitstore.Store
	DataDir  string
	Parser   musicolet.Parser
	mutation sync.Mutex
}
type Procedure struct {
	ID                  int64  `json:"id"`
	Status              string `json:"status"`
	BaseVersionID       int64  `json:"base_version_id"`
	CandidateSnapshotID int64  `json:"candidate_snapshot_id"`
	ZipPath             string `json:"zip_path"`
	SHA256              string `json:"sha256"`
	LastHead            int64  `json:"last_server_head"`
}
type Diff struct {
	ID         int64           `json:"id"`
	TargetType string          `json:"target_type"`
	TargetKey  string          `json:"target_key"`
	Operation  string          `json:"operation"`
	Base       json.RawMessage `json:"base"`
	Ours       json.RawMessage `json:"ours"`
	Theirs     json.RawMessage `json:"theirs"`
	Conflict   bool            `json:"conflict"`
}
type ConflictRow struct {
	ID           int64           `json:"id"`
	TargetType   string          `json:"target_type"`
	TargetKey    string          `json:"target_key"`
	Status       string          `json:"status"`
	Resolution   string          `json:"resolution"`
	Base         json.RawMessage `json:"base"`
	Ours         json.RawMessage `json:"ours"`
	Theirs       json.RawMessage `json:"theirs"`
	Manual       json.RawMessage `json:"manual"`
	ResolvedHead sql.NullInt64   `json:"-"`
}

func New(st *db.Store, git *gitstore.Store, dataDir string) *Service {
	return &Service{Store: st, Git: git, DataDir: dataDir}
}
