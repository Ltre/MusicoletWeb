package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (s *Server) procedureGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.App.ActiveProcedure(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if p == nil {
		writeJSON(w, 200, map[string]any{"active": false})
		return
	}
	if err = s.App.RefreshProcedure(r.Context(), p.ID); err != nil {
		fail(w, err)
		return
	}
	p, err = s.App.ActiveProcedure(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if p == nil {
		writeJSON(w, 200, map[string]any{"active": false})
		return
	}
	diffs, err := s.App.ListDiffs(r.Context(), p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	conflicts, err := s.App.ListConflicts(r.Context(), p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	history, err := s.App.ListResolutionHistory(r.Context(), p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	parserRuns, err := s.App.ListParserRuns(r.Context(), p.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"active":             true,
		"procedure":          p,
		"diffs":              diffs,
		"conflicts":          conflicts,
		"resolution_history": history,
		"parser_runs":         parserRuns,
	})
}

func (s *Server) procedureCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	if err := r.ParseMultipartForm(260 << 20); err != nil {
		fail(w, err)
		return
	}
	f, _, err := r.FormFile("backup")
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		fail(w, err)
		return
	}
	id, err := s.App.CreateProcedure(r.Context(), b)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) procedureAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     int64
		Action string
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	var err error
	switch in.Action {
	case "refresh":
		err = s.App.RefreshProcedure(r.Context(), in.ID)
	case "commit":
		err = s.App.CommitProcedure(r.Context(), in.ID)
	case "cancel":
		err = s.App.CancelProcedure(r.Context(), in.ID)
	default:
		err = fmt.Errorf("invalid action")
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) procedureResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConflictID int64           `json:"conflict_id"`
		Resolution string          `json:"resolution"`
		Manual     json.RawMessage `json:"manual"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if err := s.App.ResolveConflict(r.Context(), in.ConflictID, in.Resolution, in.Manual); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}
