# Git backend - 2609A initial implementation

## Purpose

MusicoletWeb keeps Git as the audit/version-topology layer and keeps Musicolet semantic merge in Go. Business code only depends on `internal/gitstore`; it does not shell out to Git directly.

## Initial backend

The 2609A branch uses Git CLI plumbing against a bare repository under `data/git/history.git`. The adapter implements:

- ref read and compare-and-swap update;
- blob/tree/commit creation through `hash-object`, `mktree`, `commit-tree`;
- source and server/main histories;
- multi-parent import commits;
- merge-base calculation;
- three-tree merge through an isolated temporary index;
- stage 1/2/3 conflict-index inspection;
- writing a merged tree when the Git-level merge is conflict-free.

Git-level conflicts are not treated as Musicolet business conflicts. Song Core, Queue/Playlist ordered-member semantics, playback deltas and server-delete rules remain in the Go semantic merge layer.

## libgit2/git2go spike result

`Initial Development Plans.md` preferred libgit2/git2go if the build/runtime dependency was viable. The current execution environment has CGO enabled but `pkg-config --modversion libgit2` reports no installed libgit2 package. Therefore a real git2go link/build test cannot honestly be marked as passed in this environment.

The adapter boundary deliberately allows replacing the CLI plumbing backend with git2go later without changing Musicolet semantic merge rules or the SQLite domain model. The current CLI backend already covers the Git capabilities required by the initial plan and has automated merge-base/conflict-index tests.
