package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/auth"
	"github.com/Ltre/MusicoletWeb/internal/config"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"github.com/Ltre/MusicoletWeb/internal/importer"
	"github.com/Ltre/MusicoletWeb/internal/media"
	merger "github.com/Ltre/MusicoletWeb/internal/merge"
	"github.com/Ltre/MusicoletWeb/internal/store"
	"github.com/Ltre/MusicoletWeb/internal/webui"
)

type Server struct {
	cfg    config.Config
	store  *store.Store
	auth   *auth.Manager
	hub    *agenthub.Hub
	cache  *media.Cache
	log    *slog.Logger
	static http.Handler
}

func New(cfg config.Config, st *store.Store, hub *agenthub.Hub, cache *media.Cache, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, auth: auth.New(cfg.AdminUsername, cfg.AdminPassword, cfg.TOTPSecret, cfg.SessionKey, cfg.SessionTTL, cfg.DevAuthEnabled), hub: hub, cache: cache, log: logger, static: webui.Handler()}
}

func (s *Server) Handler() http.Handler { return securityHeaders(http.HandlerFunc(s.route)) }

func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		s.health(w, r)
		return
	case r.URL.Path == "/agent/connect":
		s.hub.ServeConnect(w, r)
		return
	case r.URL.Path == "/api/auth/login":
		s.login(w, r)
		return
	case r.URL.Path == "/api/public/now-playing":
		s.publicNowPlaying(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		_, csrf, ok := s.auth.Authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrf)) != 1 {
			writeError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		s.api(w, r, csrf)
		return
	}
	s.static.ServeHTTP(w, r)
}

func (s *Server) api(w http.ResponseWriter, r *http.Request, csrf string) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	switch {
	case path == "auth/status" && r.Method == "GET":
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrfToken": csrf, "username": s.cfg.AdminUsername})
	case path == "auth/logout" && r.Method == "POST":
		s.auth.Logout(r)
		http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r)})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case path == "library" && r.Method == "GET":
		s.library(w, r)
	case path == "playback" && r.Method == "GET":
		st, err := s.store.Playback(r.Context())
		respond(w, st, err)
	case path == "playback" && r.Method == "PATCH":
		var st domain.PlaybackState
		if !decode(w, r, &st) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.UpdatePlayback(r.Context(), st))
	case path == "playback/complete" && r.Method == "POST":
		var body struct{ Path string }
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.RecordCompletedPlay(r.Context(), body.Path))
	case path == "agent/status" && r.Method == "GET":
		writeJSON(w, http.StatusOK, s.hub.Status())
	case path == "media/cache" && r.Method == "DELETE":
		respond(w, map[string]bool{"ok": true}, s.cache.Clear(r.URL.Query().Get("path")))
	case path == "media" && r.Method == "GET":
		s.serveMedia(w, r)
	case path == "imports" && r.Method == "GET":
		items, err := s.store.Procedures(r.Context())
		respond(w, items, err)
	case path == "imports" && r.Method == "POST":
		s.uploadImport(w, r)
	case strings.HasPrefix(path, "imports/"):
		s.importAction(w, r, strings.TrimPrefix(path, "imports/"))
	case path == "queues" && r.Method == "POST":
		s.createQueue(w, r)
	case path == "queues/order" && r.Method == "PUT":
		var body struct {
			IDs []int64 `json:"ids"`
		}
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.ReorderQueues(r.Context(), body.IDs))
	case strings.HasPrefix(path, "queues/"):
		s.queueAction(w, r, strings.TrimPrefix(path, "queues/"))
	case path == "playlists" && r.Method == "POST":
		var body struct {
			Name  string
			Paths []string
		}
		if !decode(w, r, &body) {
			return
		}
		id, err := s.store.CreatePlaylist(r.Context(), body.Name, body.Paths)
		respond(w, map[string]any{"id": id}, err)
	case strings.HasPrefix(path, "playlists/"):
		s.playlistAction(w, r, strings.TrimPrefix(path, "playlists/"))
	case strings.HasPrefix(path, "songs/"):
		s.songAction(w, r, strings.TrimPrefix(path, "songs/"))
	default:
		writeError(w, http.StatusNotFound, "API route not found")
	}
}

func (s *Server) playlistAction(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, 400, "invalid playlist id")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == "DELETE":
		respond(w, map[string]bool{"ok": true}, s.store.DeletePlaylist(r.Context(), id))
	case len(parts) == 2 && parts[1] == "items" && r.Method == "PUT":
		var body struct{ Paths []string }
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.SetPlaylistItems(r.Context(), id, body.Paths))
	default:
		writeError(w, 404, "playlist action not found")
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, 405, "method not allowed")
		return
	}
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeError(w, 503, "database unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "time": time.Now().UTC(), "agentOnline": s.hub.Online()})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, 405, "method not allowed")
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, 403, "cross-site login denied")
		return
	}
	var body struct{ Username, Password, TOTP string }
	if !decode(w, r, &body) {
		return
	}
	token, csrf, err := s.auth.Login(body.Username, body.Password, body.TOTP)
	if err != nil {
		time.Sleep(300 * time.Millisecond)
		writeError(w, 401, "invalid username, password or authenticator code")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/", Expires: time.Now().Add(s.cfg.SessionTTL), HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]any{"authenticated": true, "csrfToken": csrf})
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	snap, err := s.store.LoadWorking(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	versionID, version, err := s.store.CurrentVersion(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	revision, _ := s.store.ServerRevision(r.Context())
	playback, _ := s.store.Playback(r.Context())
	procedures, _ := s.store.Procedures(r.Context())
	writeJSON(w, 200, map[string]any{"snapshot": snap, "versionId": versionID, "version": version, "serverRevision": revision, "playback": playback, "procedures": procedures, "agent": s.hub.Status()})
}

func (s *Server) publicNowPlaying(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Playback(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	snap, err := s.store.LoadWorking(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	song := snap.Songs[st.SongPath]
	song.Path = ""
	writeJSON(w, 200, map[string]any{"song": song, "state": map[string]any{"playing": st.Playing, "progressMs": st.ProgressMS}})
}

func (s *Server) songAction(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	path, err := decodeHex(parts[0])
	if err != nil {
		writeError(w, 400, "invalid song key")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == "PATCH":
		var patch map[string]any
		if !decode(w, r, &patch) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.UpdateSong(r.Context(), path, patch))
	case len(parts) == 1 && r.Method == "DELETE":
		respond(w, map[string]bool{"ok": true}, s.store.DeleteSong(r.Context(), path))
	case len(parts) == 2 && parts[1] == "favorite" && r.Method == "PUT":
		var body struct {
			Favorite bool `json:"favorite"`
		}
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.ToggleFavorite(r.Context(), path, body.Favorite))
	case len(parts) == 2 && parts[1] == "changes" && r.Method == "GET":
		changes, changeErr := s.store.SongChanges(r.Context(), path)
		respond(w, changes, changeErr)
	default:
		writeError(w, 404, "song action not found")
	}
}

func (s *Server) createQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, SourceType, SourceKey string
		Paths                       []string
		Shuffle                     bool
	}
	if !decode(w, r, &body) {
		return
	}
	id, err := s.store.CreateQueue(r.Context(), body.Name, body.SourceType, body.SourceKey, body.Paths, body.Shuffle)
	respond(w, map[string]any{"id": id}, err)
}
func (s *Server) queueAction(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, 400, "invalid queue id")
		return
	}
	switch {
	case len(parts) == 1 && r.Method == "PATCH":
		var body struct{ Name string }
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.RenameQueue(r.Context(), id, body.Name))
	case len(parts) == 1 && r.Method == "DELETE":
		respond(w, map[string]bool{"ok": true}, s.store.DeleteQueue(r.Context(), id))
	case len(parts) == 2 && parts[1] == "items" && r.Method == "PUT":
		var body struct {
			Paths []string
			Mode  string
		}
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.SetQueueItems(r.Context(), id, body.Paths, body.Mode))
	case len(parts) == 2 && parts[1] == "stop" && r.Method == "PUT":
		var body struct{ Path string }
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.SetQueueStopTarget(r.Context(), id, body.Path))
	default:
		writeError(w, 404, "queue action not found")
	}
}

func (s *Server) uploadImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	file, header, err := r.FormFile("backup")
	if err != nil {
		writeError(w, 400, "backup multipart field is required")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		writeError(w, 400, "backup must be a ZIP file")
		return
	}
	id := randomID()
	dir := filepath.Join(s.cfg.DataDir, "imports", id)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	target := filepath.Join(dir, "original.zip")
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, h), file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.RemoveAll(dir)
		writeError(w, 500, "failed to archive upload")
		return
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if err = s.store.CreateProcedure(r.Context(), id, dir, importer.ParserVersion, hash, size); err != nil {
		// The upload is not part of a Procedure until CreateProcedure commits.
		// In particular, an active-Procedure 409 must not leave another private
		// backup ZIP in an untracked artifact directory.
		_ = os.RemoveAll(dir)
		writeError(w, 409, err.Error())
		return
	}
	go s.parseProcedure(id, target, dir)
	writeJSON(w, 202, map[string]any{"id": id, "status": "PARSING", "archiveSHA256": hash})
}

func (s *Server) parseProcedure(id, archive, dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	result, err := importer.ParseArchive(ctx, archive, dir)
	if err != nil {
		if s.procedureCancelled(id) {
			s.log.Info("cancelled import parser stopped", "procedure", id)
			return
		}
		s.log.Error("import parsing failed", "procedure", id, "error", err)
		_ = s.store.FailProcedure(context.Background(), id, err)
		return
	}
	if _, err = s.store.PutCandidate(ctx, id, result.Snapshot, result.Warnings); err != nil {
		if s.procedureCancelled(id) {
			s.log.Info("cancelled import result discarded", "procedure", id)
			return
		}
		s.log.Error("candidate persistence failed", "procedure", id, "error", err)
		_ = s.store.FailProcedure(context.Background(), id, err)
		return
	}
	if err = s.analyze(ctx, id); err != nil {
		s.log.Error("procedure analysis failed", "procedure", id, "error", err)
		_ = s.store.FailProcedure(context.Background(), id, err)
	}
}

func (s *Server) procedureCancelled(id string) bool {
	var status string
	err := s.store.DB.QueryRowContext(context.Background(), "SELECT status FROM import_procedures WHERE id=?", id).Scan(&status)
	return err == nil && status == "CANCELLED"
}

func (s *Server) analyze(ctx context.Context, id string) error {
	var status string
	if err := s.store.DB.QueryRowContext(ctx, "SELECT status FROM import_procedures WHERE id=?", id).Scan(&status); err != nil {
		return err
	}
	if status == "CANCELLED" || status == "FAILED" || status == "COMMITTED" {
		return nil
	}
	candidate, err := s.store.LoadCandidate(ctx, id)
	if err != nil {
		return err
	}
	var baseID int64
	if err = s.store.DB.QueryRowContext(ctx, "SELECT COALESCE(base_version_id,0) FROM import_procedures WHERE id=?", id).Scan(&baseID); err != nil {
		return err
	}
	if baseID == 0 {
		return nil
	}
	base, err := s.store.LoadVersion(ctx, baseID)
	if err != nil {
		return err
	}
	ours, err := s.store.LoadWorking(ctx)
	if err != nil {
		return err
	}
	revision, err := s.store.ServerRevision(ctx)
	if err != nil {
		return err
	}
	plan := merger.Analyze(base, ours, candidate)
	var baseDir, nextDir string
	_ = s.store.DB.QueryRowContext(ctx, "SELECT p.artifact_dir FROM musicolet_versions v JOIN import_procedures p ON p.id=v.procedure_id WHERE v.id=?", baseID).Scan(&baseDir)
	_ = s.store.DB.QueryRowContext(ctx, "SELECT artifact_dir FROM import_procedures WHERE id=?", id).Scan(&nextDir)
	if baseDir != "" && nextDir != "" {
		if rawDiffs, diffErr := importer.CompareCanonicalFiles(baseDir, nextDir); diffErr == nil {
			for _, d := range rawDiffs {
				plan.Diffs = append(plan.Diffs, merger.Diff{TargetType: "raw_file", TargetKey: d.File, ChangeKind: "CHARACTER_DIFF", Detail: d})
			}
		}
	}
	return s.store.SaveAnalysis(ctx, id, plan, revision)
}

func (s *Server) importAction(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case action == "" && r.Method == "GET":
		var p store.Procedure
		var artifactDir string
		if err := s.store.DB.QueryRowContext(r.Context(), "SELECT id,status,COALESCE(base_version_id,0),archive_sha256,parser_version,candidate_sha256,error,created_at,updated_at,last_analyzed_server_revision,artifact_dir FROM import_procedures WHERE id=?", id).Scan(&p.ID, &p.Status, &p.BaseVersionID, &p.ArchiveSHA256, &p.ParserVersion, &p.CandidateSHA256, &p.Error, &p.CreatedAt, &p.UpdatedAt, &p.ServerRevision, &artifactDir); err != nil {
			writeError(w, 404, "procedure not found")
			return
		}
		conflicts, _ := s.store.Conflicts(r.Context(), id)
		diffs := []map[string]any{}
		rows, _ := s.store.DB.QueryContext(r.Context(), "SELECT target_type,target_key,change_kind,detail_json FROM semantic_diffs WHERE procedure_id=? ORDER BY id", id)
		if rows != nil {
			for rows.Next() {
				var targetType, targetKey, kind string
				var detail json.RawMessage
				if rows.Scan(&targetType, &targetKey, &kind, &detail) == nil {
					diffs = append(diffs, map[string]any{"targetType": targetType, "targetKey": targetKey, "changeKind": kind, "detail": detail})
				}
			}
			rows.Close()
		}
		var validation any
		if raw, readErr := os.ReadFile(filepath.Join(artifactDir, "parser", "validation.json")); readErr == nil {
			_ = json.Unmarshal(raw, &validation)
		}
		writeJSON(w, 200, map[string]any{"procedure": p, "conflicts": conflicts, "diffs": diffs, "validation": validation})
	case action == "analyze" && r.Method == "POST":
		respond(w, map[string]bool{"ok": true}, s.analyze(r.Context(), id))
	case action == "resolve" && r.Method == "POST":
		var body struct {
			ConflictID, Choice string
			Resolved, Patch    json.RawMessage
		}
		if !decode(w, r, &body) {
			return
		}
		respond(w, map[string]bool{"ok": true}, s.store.ResolveConflict(r.Context(), id, body.ConflictID, body.Choice, body.Resolved, body.Patch))
	case action == "commit" && r.Method == "POST":
		respond(w, map[string]bool{"ok": true}, s.store.CommitProcedure(r.Context(), id))
	case action == "cancel" && r.Method == "POST":
		respond(w, map[string]bool{"ok": true}, s.store.CancelProcedure(r.Context(), id))
	default:
		writeError(w, 404, "import action not found")
	}
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, 400, "path is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	cached, err := s.cache.Ensure(ctx, path)
	if err != nil {
		writeError(w, 503, "当前歌曲未缓存，且无法从手机在线取得歌曲文件。")
		return
	}
	file, err := os.Open(cached)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=2592000")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self'; script-src 'self'; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := 500
		if errors.Is(err, os.ErrNotExist) {
			status = 404
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, value)
}
func randomID() string { var b [16]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func decodeHex(value string) (string, error) {
	raw, err := hex.DecodeString(value)
	return string(raw), err
}

var _ = fmt.Sprintf
