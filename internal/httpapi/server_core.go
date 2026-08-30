package httpapi

import (
	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/app"
	"github.com/Ltre/MusicoletWeb/internal/auth"
	"github.com/Ltre/MusicoletWeb/internal/config"
	"github.com/Ltre/MusicoletWeb/internal/securestore"
	"net/http"
	"path/filepath"
)

type Server struct {
	Cfg    config.Config
	App    *app.Service
	Auth   *auth.Manager
	Hub    *agenthub.Hub
	Secure *securestore.Store
	mux    *http.ServeMux
}

func New(cfg config.Config, a *app.Service, am *auth.Manager, h *agenthub.Hub, sec *securestore.Store) *Server {
	s := &Server{Cfg: cfg, App: a, Auth: am, Hub: h, Secure: sec, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return securityHeaders(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "agent_online": s.Hub.Online()})
	})
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("POST /api/auth/logout", s.logout)
	s.mux.Handle("GET /api/me", s.protect(http.HandlerFunc(s.me)))
	s.mux.Handle("GET /api/library", s.protect(http.HandlerFunc(s.library)))
	s.mux.Handle("GET /api/playback", s.protect(http.HandlerFunc(s.playback)))
	s.mux.Handle("POST /api/playback", s.protectWrite(http.HandlerFunc(s.playbackSet)))
	s.mux.Handle("POST /api/playback/mode", s.protectWrite(http.HandlerFunc(s.playbackMode)))
	s.mux.Handle("POST /api/playback/stop-target", s.protectWrite(http.HandlerFunc(s.stopTarget)))
	s.mux.Handle("POST /api/source/play", s.protectWrite(http.HandlerFunc(s.sourcePlay)))
	s.mux.Handle("POST /api/queue/action", s.protectWrite(http.HandlerFunc(s.queueAction)))
	s.mux.Handle("POST /api/playlist/action", s.protectWrite(http.HandlerFunc(s.playlistAction)))
	s.mux.Handle("POST /api/song/favorite", s.protectWrite(http.HandlerFunc(s.favorite)))
	s.mux.Handle("POST /api/song/metadata", s.protectWrite(http.HandlerFunc(s.metadata)))
	s.mux.Handle("POST /api/song/delete", s.protectWrite(http.HandlerFunc(s.songDelete)))
	s.mux.Handle("POST /api/song/played", s.protectWrite(http.HandlerFunc(s.songPlayed)))
	s.mux.Handle("POST /api/admin/agent-token", s.protectWrite(http.HandlerFunc(s.agentTokenRotate)))
	s.mux.Handle("GET /api/procedure", s.protect(http.HandlerFunc(s.procedureGet)))
	s.mux.Handle("POST /api/procedure", s.protectWrite(http.HandlerFunc(s.procedureCreate)))
	s.mux.Handle("POST /api/procedure/action", s.protectWrite(http.HandlerFunc(s.procedureAction)))
	s.mux.Handle("POST /api/procedure/resolve", s.protectWrite(http.HandlerFunc(s.procedureResolve)))
	s.mux.Handle("GET /api/media", s.protect(http.HandlerFunc(s.media)))
	s.mux.HandleFunc("GET /api/public/now-playing", s.publicNow)
	s.mux.HandleFunc("GET /api/public/now-playing/media", s.publicMedia)
	s.mux.HandleFunc("GET /api/agent/connect", s.agentConnect)
	s.mux.HandleFunc("POST /api/agent/result/{id}", s.agentResult)
	s.mux.HandleFunc("GET /now-playing", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("web", "now-playing.html"))
	})
	s.mux.Handle("/", http.FileServer(http.Dir("web")))
}
