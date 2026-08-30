package app

import (
	"database/sql"
	"encoding/json"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/musicolet"
	"sync"
)

const ParserVersion = "2609A-2"

type Service struct{Store *db.Store;Git *gitstore.Store;DataDir string;Parser musicolet.Parser;mutation sync.Mutex}
type Procedure struct{ID int64 `json:"id"`;Status string `json:"status"`;BaseVersionID int64 `json:"base_version_id"`;CandidateSnapshotID int64 `json:"candidate_snapshot_id"`;ZipPath string `json:"zip_path"`;SHA256 string `json:"sha256"`;LastHead int64 `json:"last_server_head"`}
type Diff struct{ID int64 `json:"id"`;TargetType string `json:"target_type"`;TargetKey string `json:"target_key"`;Operation string `json:"operation"`;Base json.RawMessage `json:"base"`;Ours json.RawMessage `json:"ours"`;Theirs json.RawMessage `json:"theirs"`;Conflict bool `json:"conflict"`}
type ParserRun struct{ID int64 `json:"id"`;ProcedureID int64 `json:"procedure_id"`;ParserVersion string `json:"parser_version"`;Status string `json:"status"`;Report json.RawMessage `json:"report,omitempty"`;Error string `json:"error,omitempty"`;StartedAt int64 `json:"started_at"`;FinishedAt sql.NullInt64 `json:"-"`;FinishedAtMS *int64 `json:"finished_at,omitempty"`}
type ConflictRow struct{ID int64 `json:"id"`;TargetType string `json:"target_type"`;TargetKey string `json:"target_key"`;Status string `json:"status"`;Resolution string `json:"resolution"`;Base json.RawMessage `json:"base"`;Ours json.RawMessage `json:"ours"`;Theirs json.RawMessage `json:"theirs"`;Manual json.RawMessage `json:"manual"`;ResolvedHead sql.NullInt64 `json:"-"`}
func New(st *db.Store,git *gitstore.Store,dataDir string)*Service{return &Service{Store:st,Git:git,DataDir:dataDir}}
