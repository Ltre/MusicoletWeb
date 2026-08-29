package gitstore

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCommitJSON(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("git unavailable")
	}
	s, e := Open(filepath.Join(t.TempDir(), "h.git"))
	if e != nil {
		t.Fatal(e)
	}
	c1, e := s.CommitJSON("refs/heads/main", "one", []byte(`{"a":1}`))
	if e != nil {
		t.Fatal(e)
	}
	c2, e := s.CommitJSON("refs/heads/main", "two", []byte(`{"a":2}`), c1)
	if e != nil {
		t.Fatal(e)
	}
	if c1 == c2 || s.Head("refs/heads/main") != c2 {
		t.Fatal("head not advanced")
	}
}
