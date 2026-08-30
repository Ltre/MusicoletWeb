package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (s *Server) procedureGet(w http.ResponseWriter,r *http.Request){p,e:=s.App.ActiveProcedure(r.Context());if e!=nil{fail(w,e);return};if p==nil{writeJSON(w,200,map[string]any{"active":false});return};_=s.App.RefreshProcedure(r.Context(),p.ID);p,_=s.App.ActiveProcedure(r.Context());diffs,_:=s.App.ListDiffs(r.Context(),p.ID);conf,_:=s.App.ListConflicts(r.Context(),p.ID);history,_:=s.App.ListResolutionHistory(r.Context(),p.ID);parserRuns,_:=s.App.ListParserRuns(r.Context(),p.ID);writeJSON(w,200,map[string]any{"active":true,"procedure":p,"diffs":diffs,"conflicts":conf,"resolution_history":history,"parser_runs":parserRuns})}
func (s *Server) procedureCreate(w http.ResponseWriter,r *http.Request){r.Body=http.MaxBytesReader(w,r.Body,256<<20);if e:=r.ParseMultipartForm(260<<20);e!=nil{fail(w,e);return};f,_,e:=r.FormFile("backup");if e!=nil{fail(w,e);return};defer f.Close();b,e:=io.ReadAll(f);if e!=nil{fail(w,e);return};id,e:=s.App.CreateProcedure(r.Context(),b);if e!=nil{fail(w,e);return};writeJSON(w,201,map[string]any{"id":id})}
func (s *Server) procedureAction(w http.ResponseWriter,r *http.Request){var in struct{ID int64;Action string};if readJSON(w,r,&in)!=nil{return};var e error;switch in.Action{case "refresh":e=s.App.RefreshProcedure(r.Context(),in.ID);case "commit":e=s.App.CommitProcedure(r.Context(),in.ID);case "cancel":e=s.App.CancelProcedure(r.Context(),in.ID);default:e=fmt.Errorf("invalid action")};if e!=nil{fail(w,e);return};writeOK(w)}
func (s *Server) procedureResolve(w http.ResponseWriter,r *http.Request){var in struct{ConflictID int64 `json:"conflict_id"`;Resolution string `json:"resolution"`;Manual json.RawMessage `json:"manual"`};if readJSON(w,r,&in)!=nil{return};if e:=s.App.ResolveConflict(r.Context(),in.ConflictID,in.Resolution,in.Manual);e!=nil{fail(w,e);return};writeOK(w)}
