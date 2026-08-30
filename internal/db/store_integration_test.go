//go:build integration

package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaIntegration(t *testing.T) {
	ctx := context.Background()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, e := s.DB.ExecContext(ctx, "INSERT INTO snapshots(state,parser_version,created_at) VALUES('VERSION','test',1)")
	if e != nil {
		t.Fatal(e)
	}
	sid, _ := r.LastInsertId()
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO snapshot_songs(snapshot_id,path,title) VALUES(?,?,?)", sid, "/a.mp3", "source"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO working_songs(path,title) VALUES(?,?)", "/a.mp3", "source"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, "UPDATE working_songs SET title='server' WHERE path='/a.mp3'"); e != nil {
		t.Fatal(e)
	}
	var title string
	if e = s.DB.QueryRowContext(ctx, "SELECT title FROM snapshot_songs WHERE snapshot_id=? AND path='/a.mp3'", sid).Scan(&title); e != nil || title != "source" {
		t.Fatalf("snapshot polluted: %q %v", title, e)
	}

	r, e = s.DB.ExecContext(ctx, "INSERT INTO working_queues(name) VALUES('Q')")
	if e != nil {
		t.Fatal(e)
	}
	qid, _ := r.LastInsertId()
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO working_queue_items(queue_id,path,position) VALUES(?,?,0)", qid, "/a.mp3"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO working_queue_items(queue_id,path,position) VALUES(?,?,1)", qid, "/a.mp3"); e == nil {
		t.Fatal("duplicate queue song must fail")
	}
}

func TestTransactionRollbackKeepsOrderedListAtomic(t *testing.T) {
	ctx := context.Background()
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r, _ := s.DB.ExecContext(ctx, "INSERT INTO working_queues(name) VALUES('Q')")
	qid, _ := r.LastInsertId()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = tx.ExecContext(ctx, "INSERT INTO working_queue_items(queue_id,path,position) VALUES(?,?,0)", qid, "A")
	_, e = tx.ExecContext(ctx, "INSERT INTO working_queue_items(queue_id,path,position) VALUES(?,?,1)", qid, "A")
	if e == nil {
		t.Fatal("expected constraint error")
	}
	_ = tx.Rollback()
	var n int
	if e = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM working_queue_items WHERE queue_id=?", qid).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 0 {
		t.Fatalf("partial ordered-list transaction leaked %d rows", n)
	}
}

func TestMigrationAddsCurrentQueueIndexToExistingSnapshotsTable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "musicolet.db")
	raw, e := sql.Open("sqlite", "file:"+filepath.ToSlash(p))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = raw.ExecContext(ctx, "CREATE TABLE snapshots(id INTEGER PRIMARY KEY AUTOINCREMENT, procedure_id INTEGER, state TEXT NOT NULL, parser_version TEXT NOT NULL, created_at INTEGER NOT NULL)"); e != nil {
		raw.Close()
		t.Fatal(e)
	}
	if e = raw.Close(); e != nil {
		t.Fatal(e)
	}

	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, e := s.DB.QueryContext(ctx, "PRAGMA table_info(snapshots)")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if e = rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); e != nil {
			t.Fatal(e)
		}
		if name == "current_queue_index" {
			found = true
		}
	}
	if !found {
		t.Fatal("current_queue_index migration missing")
	}
}
