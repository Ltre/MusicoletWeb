package gitstore

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Store struct{ Dir string }

type IndexConflict struct {
	Mode  string `json:"mode"`
	SHA   string `json:"sha"`
	Path  string `json:"path"`
	Stage int    `json:"stage"`
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil { return nil, err }
	s := &Store{Dir: dir}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); os.IsNotExist(err) {
		if _, err = s.run(nil, "init", "--bare", dir); err != nil { return nil, err }
	}
	return s, nil
}

func (s *Store) run(env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"--git-dir", s.Dir}, args...)...)
	if len(env) > 0 { cmd.Env = append(os.Environ(), env...) }
	out, err := cmd.CombinedOutput()
	if err != nil { return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out))) }
	return bytes.TrimSpace(out), nil
}

func (s *Store) Head(ref string) string {
	out, err := s.run(nil, "rev-parse", "--verify", ref)
	if err != nil { return "" }
	return strings.TrimSpace(string(out))
}

func (s *Store) ReadState(ref string) ([]byte, error) {
	return s.run(nil, "show", ref+":state.json")
}

func (s *Store) CommitJSON(ref, msg string, data []byte, parents ...string) (string, error) {
	blob, err := s.hashObject(data); if err != nil { return "", err }
	treeInput := fmt.Sprintf("100644 blob %s\tstate.json\n", blob)
	cmd := exec.Command("git", "--git-dir", s.Dir, "mktree")
	cmd.Stdin = strings.NewReader(treeInput)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=MusicoletWeb", "GIT_AUTHOR_EMAIL=musicolet@localhost", "GIT_COMMITTER_NAME=MusicoletWeb", "GIT_COMMITTER_EMAIL=musicolet@localhost")
	out, err := cmd.CombinedOutput(); if err != nil { return "", fmt.Errorf("git mktree: %w: %s", err, out) }
	return s.CommitTree(ref, msg, strings.TrimSpace(string(out)), parents...)
}

func (s *Store) hashObject(data []byte) (string, error) {
	cmd := exec.Command("git", "--git-dir", s.Dir, "hash-object", "-w", "--stdin")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput(); if err != nil { return "", fmt.Errorf("git hash-object: %w: %s", err, out) }
	return strings.TrimSpace(string(out)), nil
}

func (s *Store) MergeBase(a, b string) (string, error) {
	out, err := s.run(nil, "merge-base", a, b)
	if err != nil { return "", err }
	return strings.TrimSpace(string(out)), nil
}

func (s *Store) Tree(commit string) (string, error) {
	out, err := s.run(nil, "rev-parse", commit+"^{tree}")
	if err != nil { return "", err }
	return strings.TrimSpace(string(out)), nil
}

func (s *Store) mergeIndex(base, ours, theirs string) (string, []IndexConflict, error) {
	idx, err := os.CreateTemp("", "musicolet-git-index-*")
	if err != nil { return "", nil, err }
	idxPath := idx.Name(); _ = idx.Close(); _ = os.Remove(idxPath); defer os.Remove(idxPath)
	env := []string{"GIT_INDEX_FILE=" + idxPath}
	if _, err = s.run(env, "read-tree", "-m", base, ours, theirs); err != nil { return "", nil, err }
	out, err := s.run(env, "ls-files", "-u")
	if err != nil { return "", nil, err }
	var conflicts []IndexConflict
	if strings.TrimSpace(string(out)) != "" {
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.Fields(line)
			if len(parts) < 4 { continue }
			stage, _ := strconv.Atoi(parts[2])
			path := strings.Join(parts[3:], " ")
			conflicts = append(conflicts, IndexConflict{Mode: parts[0], SHA: parts[1], Stage: stage, Path: path})
		}
		return "", conflicts, nil
	}
	tree, err := s.run(env, "write-tree")
	if err != nil { return "", nil, err }
	return strings.TrimSpace(string(tree)), nil, nil
}

func (s *Store) MergeTrees(base, ours, theirs string) (string, []IndexConflict, error) {
	bt, err := s.Tree(base); if err != nil { return "", nil, err }
	ot, err := s.Tree(ours); if err != nil { return "", nil, err }
	tt, err := s.Tree(theirs); if err != nil { return "", nil, err }
	return s.mergeIndex(bt, ot, tt)
}

func (s *Store) ConflictIndex(base, ours, theirs string) ([]IndexConflict, error) {
	_, c, err := s.MergeTrees(base, ours, theirs)
	return c, err
}

func (s *Store) CommitTree(ref, msg, tree string, parents ...string) (string, error) {
	args := []string{"commit-tree", tree}
	for _, p := range parents { if strings.TrimSpace(p) != "" { args = append(args, "-p", p) } }
	args = append(args, "-m", msg)
	env := []string{"GIT_AUTHOR_NAME=MusicoletWeb", "GIT_AUTHOR_EMAIL=musicolet@localhost", "GIT_COMMITTER_NAME=MusicoletWeb", "GIT_COMMITTER_EMAIL=musicolet@localhost"}
	out, err := s.run(env, args...); if err != nil { return "", err }
	commit := strings.TrimSpace(string(out))
	old := s.Head(ref)
	update := []string{"update-ref", ref, commit}
	if old != "" { update = append(update, old) }
	if _, err = s.run(nil, update...); err != nil { return "", err }
	return commit, nil
}
