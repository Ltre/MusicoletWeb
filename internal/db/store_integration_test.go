//go:build integration

package db

import (
	"context"
	"testing"
)

func TestSchemaIntegration(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.DB.ExecContext(context.Background(), "INSERT INTO snapshots(state,parser_version,created_at) VALUES('CANDIDATE','test',1)"); e != nil {
		t.Fatal(e)
	}
}
