//go:build integration

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ltre/MusicoletWeb/internal/app"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
)

func TestProcedureGetFailsClosedWhenAuditQueryFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	st, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	git, err := gitstore.Open(filepath.Join(root, "git", "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	svc := app.New(st, git, root)
	if _, err = st.DB.ExecContext(ctx, "INSERT INTO import_procedures(status,source_zip_path,source_zip_sha256,last_analyzed_server_head,created_at,updated_at) VALUES('READY_TO_COMMIT','fixture.zip','fixture',0,1,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB.ExecContext(ctx, "DROP TABLE semantic_diffs"); err != nil {
		t.Fatal(err)
	}

	s := &Server{App: svc}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/procedure", nil)
	s.procedureGet(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(rr.Body.String()), "semantic_diffs") {
		t.Fatalf("missing audit query error: %q", rr.Body.String())
	}
}
