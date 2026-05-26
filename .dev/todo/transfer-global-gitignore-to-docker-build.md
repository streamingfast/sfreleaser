# Transfer Global Git Ignores to Docker Build Container

mode: feature
state: review
root_git: /Users/maoueh/work/sf/sfreleaser
worktree: /Users/maoueh/work/sf/sfreleaser/.worktrees/feature/transfer-global-gitignore-to-docker-build
branch: feature/transfer-global-gitignore-to-docker-build
target_branch: master

> **Resume protocol:** read **Dev Feedback** and the **State Tracker** below first, then jump to the
> step marked `Current`. Ensure that you are in the correct worktree and branch according to preamble here. Update current with Developer feedback and update the tracker after every meaningful change.
> Do not mutate completed steps; append a new entry instead.

---

## Initial Description

Implement a new feature so that global Git ignores are also transferred in the Docker container used for performing build/release.

Context: `sfreleaser` runs builds/releases inside a Docker container. Git supports a user-level "global" ignore file (typically configured via `core.excludesFile` in `~/.gitconfig`, often pointing at `~/.gitignore` or `~/.config/git/ignore`). Today those global ignores are not propagated into the build container, so files the user expects Git to ignore on the host may not be ignored inside the container during release builds. The feature should detect the user's global gitignore on the host and make it available inside the build container so Git inside the container honors the same global ignore rules.

## Dev Feedback

```
GIT_CONFIG_COUNT=1
...
```

That seems brittle for the future if we need to add more config tweak. Can you collect all git override needed and in this function since we would know how many there is, we could build the proper Git override set. Do this for future proofing but ensure we use a very minimal code to implement this.

## Spec & Implementation

### Summary

When `sfreleaser` invokes the `goreleaser/goreleaser-cross` Docker image to build or release, propagate the host user's global Git excludes file into the container and configure Git inside the container so it honors those rules. This makes Goreleaser's dirty-tree / archiving / packaging behavior consistent with what the user observes on the host (e.g. `.DS_Store`, editor swap files, local tool caches listed in the user's global ignore are not flagged as untracked or accidentally packaged).

### Scope

**In scope:**
- Detect the host's effective global Git excludes file using the same precedence Git itself uses.
- Mount that file read-only into the build/release container at a fixed, well-known path.
- Configure Git inside the container so it loads that file as `core.excludesFile`, regardless of the in-container `HOME` or any `git config` defaults the image ships with.
- Apply the change for both `sfreleaser build` and `sfreleaser release github` flows (both go through `goreleaseDockerCommand` in `cmd/sfreleaser/release_github.go`, so a single change covers both).
- Gracefully no-op (with a debug log) when no global excludes file is configured / present on the host.
- Debug-log the detected source path and the mount target so users can troubleshoot via `DLOG`.

**Out of scope:**
- Propagating other parts of the host `~/.gitconfig` (user.name, signing keys, aliases, etc.). Only the excludes file is in scope.
- Propagating per-repo `.git/info/exclude`. The repo itself is already bind-mounted into the container at `/go/src/work`, so `.git/info/exclude` rides along for free.
- A new user-facing flag to override the path. Stated assumption: not needed for a first cut; can be added later if friction appears.
- Windows-host path translation. `sfreleaser` already assumes a POSIX-like host (`/var/run/docker.sock`, `cli.WorkingDirectory()` mounted directly), so we inherit that constraint.

### Design

**Where the change lives.** All Docker invocations go through `goreleaseDockerCommand` in `cmd/sfreleaser/release_github.go`. It is the single integration point. We add logic there to:
1. Resolve the host's global excludes file path.
2. If found, append two pieces to the `docker run` argument list:
   - A read-only bind mount: `-v <host-path>:/etc/sfreleaser/gitignore_global:ro`.
   - Three env vars that make any `git` invocation inside the container honor that file:
     - `GIT_CONFIG_COUNT=1`
     - `GIT_CONFIG_KEY_0=core.excludesFile`
     - `GIT_CONFIG_VALUE_0=/etc/sfreleaser/gitignore_global`
3. If not found, skip silently (debug log only).

The `GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_VALUE_<n>` env-var family has been supported since Git 2.31 (March 2021) and applies the override to every `git` child process — including the ones Goreleaser spawns to read tags, detect dirty state, archive files, etc. This is more robust than writing to `/root/.gitconfig` or relying on `HOME=/root` semantics inside the image.

**Detection algorithm (mirrors Git's own resolution order).**

Implemented as a new helper, e.g. `resolveHostGlobalGitIgnore() (string, bool)`, in `cmd/sfreleaser/git.go`:

1. Run `git config --global --get core.excludesFile` (use `maybeResultOf` so a missing key is non-fatal).
   - If the result is non-empty: expand a leading `~/` to the user's home dir (via `os.UserHomeDir`); expand `$HOME` if present.
2. If step 1 produced no value, fall back to the XDG default:
   - If `XDG_CONFIG_HOME` env var is set: `$XDG_CONFIG_HOME/git/ignore`.
   - Otherwise: `<home>/.config/git/ignore`.
3. `os.Stat` the resolved path. If it does not exist or is not a regular file, return `("", false)`.
4. Return the absolute, cleaned path (`filepath.Abs` + `filepath.Clean`) and `true`.

Debug-log the outcome with `zlog.Debug`, including the host path and which step matched. On any unexpected error (e.g. `os.UserHomeDir` fails), log at debug level and return `false` — never abort the build for an optional convenience.

**Argument construction.** Today `goreleaseDockerCommand` builds `arguments` like:

```
docker run --platform <p> --rm -e CGO_ENABLED=1 --env-file <env> \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v <cwd>:/go/src/work \
  -w /go/src/work \
  [optional Go cache mount] \
  <image> <goreleaser args...>
```

We extend it with (only when detection succeeded):

```
  -v <host-global-ignore>:/etc/sfreleaser/gitignore_global:ro \
  -e GIT_CONFIG_COUNT=1 \
  -e GIT_CONFIG_KEY_0=core.excludesFile \
  -e GIT_CONFIG_VALUE_0=/etc/sfreleaser/gitignore_global \
```

Insertion point: right after the existing repo bind mount, before the optional Go cache block — keeps related mounts/env vars grouped logically and unaffected by the `dockerExtraArguments` append that comes later.

**Why `/etc/sfreleaser/gitignore_global` as the in-container path.** It is a stable, namespaced path that:
- Is unlikely to collide with anything in `goreleaser/goreleaser-cross`.
- Does not depend on the in-container `HOME` (root) so it works even if the image ever switches base user.
- Communicates ownership ("sfreleaser put this here") to anyone debugging the container.

**Read-only mount.** The file is mounted `:ro` — the build process must never modify the user's host-level config.

### Implementation Plan

Each step below is sized as a single commit.

1. **Add the detection helper.** In `cmd/sfreleaser/git.go`, add `resolveHostGlobalGitIgnore() (path string, ok bool)`:
   - Calls `maybeResultOf("git config --global --get core.excludesFile")` and trims the result.
   - Expands a leading `~` / `~/` and any `$HOME` reference using `os.UserHomeDir`.
   - Falls back to `$XDG_CONFIG_HOME/git/ignore` (or `<home>/.config/git/ignore` when unset).
   - Stats the path; returns `(absPath, true)` only on a regular existing file.
   - Logs each branch outcome via `zlog.Debug`.

2. **Add unit tests for the helper.** In a new `cmd/sfreleaser/git_test.go` (or extend existing test file if one is added), test:
   - `~`-prefixed path is expanded relative to `HOME`.
   - `$HOME`-prefixed path is expanded.
   - Non-existing path returns `("", false)`.
   - Directory (not file) returns `("", false)`.
   - XDG fallback is used when `core.excludesFile` is not set (drive via setting `XDG_CONFIG_HOME` to a temp dir containing `git/ignore`).
   Use `t.Setenv` for env manipulation. Follow project convention: `github.com/stretchr/testify/require`, `logging.PackageLogger` registered in a shared `log_test.go` if not present.
   Note: tests must not depend on the runner's real `git config --global` state. Mock by setting `HOME` and `XDG_CONFIG_HOME` to temp dirs and pointing `GIT_CONFIG_GLOBAL` (an environment override Git honors) at a controlled file — or, if mocking the `git` invocation is awkward, refactor the helper to accept the raw `core.excludesFile` value as a parameter and split the IO bits into a thin wrapper that is exercised by an integration-style test only.

3. **Wire detection into the docker command.** In `cmd/sfreleaser/release_github.go::goreleaseDockerCommand`, after the existing repo `-v <cwd>:/go/src/work` mount, call the helper and, when ok, append the four arguments described in the Design section. Use the same `-v <src>:<dst>:ro` / `-e KEY=VALUE` string style already used in the function for consistency.

4. **(Optional, recommended) Extract a small helper for clarity.** Introduce `appendGlobalGitIgnoreMount(arguments []string) []string` co-located with `goreleaseDockerCommand` to keep its body readable. Pure refactor inside the same file; no behavior change beyond step 3.

5. **Update `CHANGELOG.md`.** Per project convention (Keep a Changelog + the algorithm in the repo root `CLAUDE.md`):
   - Most recent header is `## v0.13.1` (versioned, and `git tag v0.13.1` exists per `git log`). Start a new `## Unreleased` section above it.
   - Under `### Added`, add:
     `Mount the host user's global Git excludes file (resolved from core.excludesFile or $XDG_CONFIG_HOME/git/ignore) into the goreleaser-cross container and configure Git inside the container to honor it, so dirty-tree detection and archive contents match what the user sees on the host.`

6. **Run the gate commands.** `go test ./...` then `gofmt -l .` (must be empty). No `go build` to a tracked path — use `./devel/sfreleaser --help` (or a dry-run build) for manual smoke if needed, per repo `CLAUDE.md`.

### Decisions & Assumptions

| Decision / Assumption | Rationale |
|---|---|
| Use `GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_0` / `GIT_CONFIG_VALUE_0` env vars to force `core.excludesFile` inside the container. | Works regardless of in-container `HOME` and any existing `/root/.gitconfig` shipped in the image; supported by Git ≥ 2.31, which `goreleaser-cross:v1.25` comfortably exceeds. Cleaner than synthesizing a `.gitconfig` to mount. |
| Mount the file at the fixed path `/etc/sfreleaser/gitignore_global` (read-only). | Namespaced, stable, image-agnostic, signals ownership, and is independent of the container user's `HOME`. Read-only protects host config from container writes. |
| Detection mirrors Git's own resolution: `core.excludesFile` first, then `$XDG_CONFIG_HOME/git/ignore` (with `~/.config/git/ignore` as the fallback default). | Matches user expectations; what works on the host now works in the container without further configuration. |
| Expand `~` and `$HOME` in the value returned by `git config --global --get core.excludesFile`. | Git itself expands `~` in `core.excludesFile`; we must do the same to resolve the host path before mounting. |
| Silently no-op (debug log only) when no global excludes file exists. | Optional convenience; should never block a release for users who do not configure one. |
| No new CLI flag (e.g. `--global-gitignore`) in this iteration. | The detection covers all standard setups. A flag can be added as a follow-up if a real need surfaces. |
| Per-repo `.git/info/exclude` is intentionally NOT handled specially. | The whole repo (including `.git/`) is already bind-mounted at `/go/src/work`, so this file naturally rides along. |
| Single integration point: `goreleaseDockerCommand`. | Both `sfreleaser build` and `sfreleaser release github` route through it; a single change covers both flows. |
| Host OS is POSIX-like (macOS / Linux). | Project already makes the same assumption (`/var/run/docker.sock` mount, direct cwd bind, no path-style translation). Not regressing. |
| Tests must not rely on the developer's real `~/.gitconfig`. | Use `t.Setenv` for `HOME` / `XDG_CONFIG_HOME` / `GIT_CONFIG_GLOBAL`, or refactor pure logic to be parameter-driven and unit-test it directly. |

### Potential Follow-ups (not in this task)

- `--global-gitignore <path>` flag to override detection.
- Propagate select pieces of host `~/.gitconfig` (e.g. `user.name`, `user.email`) for cases where Goreleaser would benefit from the author identity.
- Mirror the same mechanism for any other Docker-based hook the project may add in the future.

## State Tracker

**Last Updated:** 2026-05-26
**Current Step:** Phase 7 — Dev feedback round 1 applied; awaiting re-review.
**Status:** Refactored env-var emission to collect overrides into a slice and materialize `GIT_CONFIG_COUNT` / `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_VALUE_<n>` dynamically. `go test ./...` passes, `gofmt -l .` clean. State remains `review`.

| Step | Status | Notes |
|---|---|---|
| Phase 1 — Contextual Understanding | Done | Read `release_github.go::goreleaseDockerCommand` (single docker-run integration point), `build.go`, `release.go`, `git.go`, `bashism.go`, `models.go`. Confirmed `goreleaser/goreleaser-cross:v1.25` is the image; only one place constructs the `docker run` argv. |
| Phase 2 — Gap Analysis | Done | Critical gaps: detection precedence, in-container path strategy, override mechanism, no-op semantics. All resolved via stated assumptions; none judged blocking enough to require a question round given the small surface area. |
| Phase 3 — Challenging Dialogue | Skipped (with stated assumptions) | Initial description + additional context provided by the loop driver covered the ambiguous points. All open choices are documented in Decisions & Assumptions for the developer to push back on via Dev Feedback if desired. |
| Phase 4 — Specification Writing | Done | Spec, plan, and decision table written. |
| Phase 5 — Spec Review | Done | User signalled acceptance by moving the task from `review` to `ready` for implementation. |
| Phase 6 — Implementation Step 1 (helper) | Done | `resolveHostGlobalGitIgnore` added in `cmd/sfreleaser/git.go` (split into small private helpers: `resolveFromGitConfig`, `resolveFromXDGFallback`, `expandHomeReferences`, `existingRegularFile`). Commit: `Add resolveHostGlobalGitIgnore helper`. |
| Phase 6 — Implementation Step 2 (tests) | Done | `cmd/sfreleaser/git_test.go` added with 8 cases covering the full precedence chain. Uses `GIT_CONFIG_GLOBAL` + `GIT_CONFIG_SYSTEM=/dev/null` for isolation from the runner's real git config. Commit: `Add unit tests for resolveHostGlobalGitIgnore`. |
| Phase 6 — Implementation Step 3+4 (wiring + extracted helper) | Done | Added `containerGlobalGitIgnorePath` const + `appendGlobalGitIgnoreMount` helper in `cmd/sfreleaser/release_github.go`; wired into `goreleaseDockerCommand` right after the repo bind mount. Commit: `Mount host global gitignore into goreleaser-cross container`. |
| Phase 6 — Implementation Step 5 (changelog) | Done | New `## Unreleased` section above `## v0.13.1` (which is tagged) with an `### Added` entry. Commit: `Document global gitignore mount under Unreleased`. |
| Phase 6 — Implementation Step 6 (gates) | Done | `go test ./...` → all pass. `gofmt -l .` → empty. |
| Phase 7 — Dev Feedback Round 1 | Done | Reviewer flagged the hard-coded `GIT_CONFIG_COUNT=1` / `KEY_0` / `VALUE_0` triplet as brittle for future overrides. Refactored `appendGlobalGitIgnoreMount` into two layers: (a) `collectGlobalGitIgnoreOverride` (and any future `collect…` helper) appends to a shared `[]gitConfigOverride` slice plus any related mount, (b) the new top-level `appendGitConfigOverrides` materializes the slice into the `GIT_CONFIG_COUNT` + indexed `KEY_<n>`/`VALUE_<n>` env vars in one place — adding a future override is a one-liner. No user-visible behavior change (CHANGELOG untouched). `go test ./...` passes, `gofmt -l .` clean. Commit: `bd2d5af`. |
