package gitstore

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	Dir string
	mu  sync.Mutex
}

func Open(dir string) (*Store, error) {
	s := &Store{Dir: dir}
	if _, e := os.Stat(filepath.Join(dir, "HEAD")); os.IsNotExist(e) {
		if e = os.MkdirAll(dir, 0o700); e != nil {
			return nil, e
		}
		if _, e = s.run("init", "--bare", dir); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) run(args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=MusicoletWeb", "GIT_AUTHOR_EMAIL=musicoletweb@local", "GIT_COMMITTER_NAME=MusicoletWeb", "GIT_COMMITTER_EMAIL=musicoletweb@local")
	o, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, e, o)
	}
	return strings.TrimSpace(string(o)), nil
}
func (s *Store) Head(ref string) string {
	o, e := s.run("--git-dir", s.Dir, "rev-parse", "--verify", ref)
	if e != nil {
		return ""
	}
	return o
}
func (s *Store) CommitJSON(ref, msg string, data []byte, parents ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.Command("git", "--git-dir", s.Dir, "hash-object", "-w", "--stdin")
	cmd.Stdin = bytes.NewReader(data)
	o, e := cmd.CombinedOutput()
	if e != nil {
		return "", e
	}
	blob := strings.TrimSpace(string(o))
	treeSpec := fmt.Sprintf("100644 blob %s\tstate.json\n", blob)
	cmd = exec.Command("git", "--git-dir", s.Dir, "mktree")
	cmd.Stdin = strings.NewReader(treeSpec)
	o, e = cmd.CombinedOutput()
	if e != nil {
		return "", e
	}
	tree := strings.TrimSpace(string(o))
	args := []string{"--git-dir", s.Dir, "commit-tree", tree, "-m", msg}
	for _, p := range parents {
		if p != "" {
			args = append(args, "-p", p)
		}
	}
	commit, e := s.run(args...)
	if e != nil {
		return "", e
	}
	old := s.Head(ref)
	up := []string{"--git-dir", s.Dir, "update-ref", ref, commit}
	if old != "" {
		up = append(up, old)
	}
	if _, e = s.run(up...); e != nil {
		return "", e
	}
	return commit, nil
}

func (s *Store) ReadState(ref string) ([]byte, error) {
	c := exec.Command("git", "--git-dir", s.Dir, "show", ref+":state.json")
	o, e := c.Output()
	if e != nil {
		return nil, e
	}
	return o, nil
}
