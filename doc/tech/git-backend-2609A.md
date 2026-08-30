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

For bare repositories, three-tree merge uses `git read-tree -i -m` with a temporary `GIT_INDEX_FILE`. The `-i` flag is required because the audit repository intentionally has no work tree. This behavior is covered by the real-dependency CI test suite.

Git-level conflicts are not treated as Musicolet business conflicts. Song Core, Queue/Playlist ordered-member semantics, playback deltas and server-delete rules remain in the Go semantic merge layer.

## libgit2/git2go spike result

`Initial Development Plans.md` asked for an isolated libgit2/git2go viability spike before treating that stack as the preferred embedded Git backend.

The result is **do not adopt git2go as the production backend in the 2609A initial implementation**.

Reasons:

1. The local development execution environment has CGO enabled but no installed `libgit2.pc`/headers, so there is no existing native dependency that can simply be linked.
2. More importantly, the official git2go compatibility table still maps its latest stable module line `git2go/v34` to **libgit2 1.5**. The libgit2 project has continued through newer 1.x releases; current 1.8/1.9 maintenance releases include security fixes. Pinning MusicoletWeb to an older native libgit2 solely to satisfy an inactive/stale binding would be a regression, not a validation success.
3. The current Git CLI plumbing backend uses the host Git implementation, is isolated behind `internal/gitstore`, and already has real tests for merge-base, three-tree merge, stage 1/2/3 conflict index, tree creation, commit creation and ref updates.

Therefore the initial architecture keeps the Git CLI plumbing adapter. This is a deliberate post-spike choice, not an accidental fallback.

## Replacement boundary

A future embedded Git backend remains possible without changing the Musicolet data model or semantic merge engine. Reconsider replacement only when one of these conditions is true:

- git2go publishes a maintained binding for a current supported libgit2 line;
- another maintained Go binding provides the required merge/index/ref primitives;
- a direct libgit2 wrapper is introduced with a documented native dependency/update policy.

Any replacement must pass the same `internal/gitstore` behavior tests before becoming the default backend.

## References checked for the spike

- git2go repository/compatibility table: `https://github.com/libgit2/git2go`
- libgit2 releases: `https://github.com/libgit2/libgit2/releases`
