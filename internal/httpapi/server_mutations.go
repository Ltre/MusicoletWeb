package httpapi

import (
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/domain"
	"net/http"
	"time"
)

func (s *Server) sourcePlay(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourceType, SourceKey, Name, Path string
		Paths                             []string
		Shuffle                           bool
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	id, e := s.App.EnsureSourceQueue(r.Context(), in.SourceType, in.SourceKey, in.Name, in.Paths, in.Path, in.Shuffle)
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"queue_id": id})
}

func (s *Server) queueAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action, Path, Name string
		QueueID            int64
		Position           int
		Next               bool
		Order              []string
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	var e error
	switch in.Action {
	case "add":
		e = s.App.QueueAdd(r.Context(), in.QueueID, in.Path, in.Next)
	case "remove":
		e = s.App.QueueRemove(r.Context(), in.QueueID, in.Path)
	case "move":
		e = s.App.QueueMove(r.Context(), in.QueueID, in.Path, in.Position)
	case "delete":
		e = s.App.DeleteQueue(r.Context(), in.QueueID)
	case "rename":
		e = s.App.RenameQueue(r.Context(), in.QueueID, in.Name)
	case "move_queue":
		e = s.App.ReorderQueues(r.Context(), in.QueueID, in.Position)
	case "reorder":
		e = s.App.QueueReorderItems(r.Context(), in.QueueID, in.Order, "REORDER")
	case "reverse":
		q := append([]string(nil), in.Order...)
		for i, j := 0, len(q)-1; i < j; i, j = i+1, j-1 {
			q[i], q[j] = q[j], q[i]
		}
		e = s.App.QueueReorderItems(r.Context(), in.QueueID, q, "REVERSE")
	case "randomize":
		e = s.App.QueueReorderItems(r.Context(), in.QueueID, in.Order, "RANDOMIZE")
	default:
		e = fmt.Errorf("invalid action")
	}
	if e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) playlistAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, Action, Path string
		Position           int
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.PlaylistAction(r.Context(), in.Name, in.Action, in.Path, in.Position); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string
		On   bool
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.SetFavorite(r.Context(), in.Path, in.On); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string      `json:"path"`
		Song domain.Song `json:"song"`
	}
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.UpdateMetadata(r.Context(), in.Path, in.Song); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) songDelete(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path string }
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.DeleteSong(r.Context(), in.Path); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}

func (s *Server) songPlayed(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path string }
	if readJSON(w, r, &in) != nil {
		return
	}
	if e := s.App.IncrementPlay(r.Context(), in.Path, time.Now()); e != nil {
		fail(w, e)
		return
	}
	writeOK(w)
}
