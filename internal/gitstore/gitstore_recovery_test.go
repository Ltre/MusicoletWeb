package gitstore

import (
	"path/filepath"
	"testing"
)

func TestCommitMatchesRequiresExactStateAndParents(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.git"))
	if err != nil {
		t.Fatal(err)
	}
	c1, err := s.CommitJSON("refs/heads/main", "one", []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.CommitMatches(c1, []byte(`{"v":1}`))
	if err != nil || !ok {
		t.Fatalf("root commit should match: ok=%v err=%v", ok, err)
	}
	if ok, err = s.CommitMatches(c1, []byte(`{"v":2}`)); err != nil || ok {
		t.Fatalf("different state must not match: ok=%v err=%v", ok, err)
	}
	c2, err := s.CommitJSON("refs/heads/main", "two", []byte(`{"v":2}`), c1)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err = s.CommitMatches(c2, []byte(`{"v":2}`), c1); err != nil || !ok {
		t.Fatalf("child commit should match exact parent: ok=%v err=%v", ok, err)
	}
	if ok, err = s.CommitMatches(c2, []byte(`{"v":2}`)); err != nil || ok {
		t.Fatalf("missing expected parent must not match: ok=%v err=%v", ok, err)
	}
}
