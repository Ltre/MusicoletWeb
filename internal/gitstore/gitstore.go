package gitstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const zeroOID = "0000000000000000000000000000000000000000"

// Repository is a narrow adapter around Git plumbing. Business merge semantics
// intentionally never enter this package.
type Repository struct{ dir string }

type Prepared struct{ Commit, Parent string }

func Open(ctx context.Context, dir string) (*Repository, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	r := &Repository{dir: dir}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if _, err = r.run(ctx, nil, "init", "--bare", dir); err != nil {
			return nil, fmt.Errorf("initialize history repository: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Repository) Head(ctx context.Context) (string, error) {
	out, err := r.run(ctx, nil, "rev-parse", "--verify", "refs/heads/main")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Repository) Prepare(ctx context.Context, state []byte, message string) (Prepared, error) {
	parent, err := r.Head(ctx)
	if err != nil {
		return Prepared{}, err
	}
	blob, err := r.run(ctx, state, "hash-object", "-w", "--stdin")
	if err != nil {
		return Prepared{}, err
	}
	blobID := strings.TrimSpace(string(blob))
	treeLine := []byte("100644 blob " + blobID + "\tstate.json\n")
	tree, err := r.run(ctx, treeLine, "mktree")
	if err != nil {
		return Prepared{}, err
	}
	treeID := strings.TrimSpace(string(tree))
	args := []string{"commit-tree", treeID, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	commit, err := r.run(ctx, nil, args...)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Commit: strings.TrimSpace(string(commit)), Parent: parent}, nil
}

func (r *Repository) PrepareCommit(ctx context.Context, state []byte, message string) (commit, parent string, err error) {
	p, err := r.Prepare(ctx, state, message)
	return p.Commit, p.Parent, err
}
func (r *Repository) FinalizeCommit(ctx context.Context, commit, parent string) error {
	return r.Finalize(ctx, Prepared{Commit: commit, Parent: parent})
}

func (r *Repository) Finalize(ctx context.Context, p Prepared) error {
	old := p.Parent
	if old == "" {
		old = zeroOID
	}
	_, err := r.run(ctx, nil, "update-ref", "refs/heads/main", p.Commit, old)
	return err
}

func (r *Repository) ForceRef(ctx context.Context, commit string) error {
	if commit == "" {
		return nil
	}
	_, err := r.run(ctx, nil, "update-ref", "refs/heads/main", commit)
	return err
}

func (r *Repository) run(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--git-dir", r.dir}, args...)...)
	cmd.Stdin = bytes.NewReader(input)
	now := time.Now().UTC().Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=MusicoletWeb", "GIT_AUTHOR_EMAIL=history@musicolet.local", "GIT_COMMITTER_NAME=MusicoletWeb", "GIT_COMMITTER_EMAIL=history@musicolet.local", "GIT_AUTHOR_DATE="+now, "GIT_COMMITTER_DATE="+now)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
