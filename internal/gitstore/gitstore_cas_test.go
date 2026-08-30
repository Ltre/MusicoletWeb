package gitstore

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitTreeRejectsStaleFirstParentOnExistingRef(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := s.CommitJSON("refs/heads/main", "one", []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CommitJSON("refs/heads/main", "two", []byte(`{"v":2}`), c1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CommitJSON("refs/heads/main", "stale", []byte(`{"v":3}`), c1)
	if err == nil || !strings.Contains(err.Error(), "commit first parent") {
		t.Fatalf("stale parent must be rejected, got %v", err)
	}
	if head := s.Head("refs/heads/main"); head != c2 {
		t.Fatalf("stale commit moved ref: got %s want %s", head, c2)
	}
}

func TestCommitTreeAllowsNewRefFromExistingParent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.CommitJSON("refs/heads/base", "base", []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CommitJSON("refs/heads/topic", "topic", []byte(`{"v":2}`), base)
	if err != nil {
		t.Fatal(err)
	}
	parents, err := s.Parents(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(parents) != 1 || parents[0] != base {
		t.Fatalf("topic parents=%v want [%s]", parents, base)
	}
}
