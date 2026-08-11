package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

// resolveGitRemote attempts to find a git remote that matches the owner.
// If exactly one matching remote is found, it is used. Otherwise, falls back to global.GitRemote.
func resolveGitRemote(global *GlobalModel) string {
	if global.Owner == "" {
		zlog.Debug("no owner specified, using default git remote", zap.String("remote", global.GitRemote))
		return global.GitRemote
	}

	// Get list of remotes with their URLs from git config (no server queries)
	output, _, err := maybeResultOf("git config --get-regexp", "^remote\\..*\\.url$")
	if err != nil {
		zlog.Debug("failed to get git remotes, using default", zap.String("remote", global.GitRemote), zap.Error(err))
		return global.GitRemote
	}

	lines := getLines(output)
	matchingRemotes := make(map[string]bool)

	// Parse remotes and find those matching the owner
	// Format: remote.<name>.url <url>
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		// Extract remote name from "remote.<name>.url"
		configKey := parts[0]
		remoteURL := parts[1]

		// Extract name from "remote.<name>.url"
		remoteName := strings.TrimPrefix(configKey, "remote.")
		remoteName = strings.TrimSuffix(remoteName, ".url")

		// Check if URL contains the owner (handles both github.com/owner/ and github.com:owner/)
		if strings.Contains(remoteURL, "/"+global.Owner+"/") || strings.Contains(remoteURL, ":"+global.Owner+"/") {
			matchingRemotes[remoteName] = true
		}
	}

	// If exactly one matching remote found, use it
	if len(matchingRemotes) == 1 {
		for remote := range matchingRemotes {
			zlog.Debug("found matching git remote for owner", zap.String("owner", global.Owner), zap.String("remote", remote))
			return remote
		}
	}

	// If multiple or no matches, fall back to default
	if len(matchingRemotes) > 1 {
		zlog.Debug("multiple matching git remotes found, using default", zap.String("owner", global.Owner), zap.Int("count", len(matchingRemotes)), zap.String("remote", global.GitRemote))
	} else {
		zlog.Debug("no matching git remote found, using default", zap.String("owner", global.Owner), zap.String("remote", global.GitRemote))
	}

	return global.GitRemote
}

// createTemporaryTag creates the local throw-away tag that Goreleaser needs to
// resolve the version being released.
//
// The `tag.gpgsign` config is forced off for that single invocation: when it is
// enabled, Git creates a *signed annotated* tag, which requires a tag message and
// therefore opens `$GIT_EDITOR` (and GPG may in turn ask for a passphrase), both
// of which hang any non-interactive run. The signature would be pointless anyway,
// this tag is deleted by [deleteTemporaryTag] as soon as the command completes and
// the tag that ends up published is created server-side by the GitHub release.
func createTemporaryTag(version string) {
	run("git -c tag.gpgsign=false tag", version)
}

// deleteTemporaryTag removes the local tag created by [createTemporaryTag].
func deleteTemporaryTag(version string) {
	runSilent("git tag -d", version)
}

func ensureGitSync(global *GlobalModel) {
	state := fetchGitSyncState()

	switch state {
	case gitSyncUpToDate:

	case gitSyncNeedPull:
		if yes, _ := cli.PromptConfirm("It seems you need to 'git pull', do it now?"); yes {
			run("git pull", resolveGitRemote(global))
		}

	case gitSyncNeedPush:
		fmt.Println("Pushing our changes to Git so it knowns about our commit(s)")
		run("git push", resolveGitRemote(global))

	case gitSyncDiverged:
		fmt.Println("Your branch has diverged from remote, cannot continue")
		run("git status")
		cli.Quit("")
	}
}

type gitSyncState string

var (
	gitSyncUpToDate gitSyncState = "up-to-date"
	gitSyncNeedPull gitSyncState = "need-pull"
	gitSyncNeedPush gitSyncState = "need-push"
	gitSyncDiverged gitSyncState = "diverged"
)

// See https://stackoverflow.com/a/3278427/697930
func fetchGitSyncState() gitSyncState {
	upstream := "'@{u}'"
	local := resultOf("git rev-parse @")

	remote, info, err := maybeResultOf("git rev-parse", upstream)
	if err != nil {
		if strings.Contains(remote, "no upstream configured") {
			remote = ""
		} else {
			cli.NoError(err, "Command %q failed with %q", info, remote)
		}
	}

	base, info, err := maybeResultOf("git merge-base @", upstream)
	if strings.Contains(base, "no upstream configured") {
		base = ""
	} else {
		cli.NoError(err, "Command %q failed with %q", info, base)
	}

	if local == remote {
		return gitSyncUpToDate
	}

	if local == base {
		return gitSyncNeedPull
	}

	if remote == base {
		return gitSyncNeedPush
	}

	return gitSyncDiverged
}

func ensureGitNotDirty() {
	if isGitDirty() {
		fmt.Println("Your git repository is dirty, refusing to release (use --allow-dirty to continue even if Git is dirty)")
		run("git status")
		cli.Exit(1)
	}
}

func isGitDirty() bool {
	return resultOf("git status --porcelain") != ""
}

var remoteTagRegex = regexp.MustCompile(`refs/tags/(v?[0-9]+\.[0-9]+\.[0-9]+[^\s]*)`)

// resolveHostGlobalGitIgnore tries to find the host user's effective global Git
// excludes file, using the same precedence Git itself uses:
//
//  1. `git config --global --get core.excludesFile` (expanding a leading `~` /
//     `~/` and `$HOME` references).
//  2. `$XDG_CONFIG_HOME/git/ignore` if `XDG_CONFIG_HOME` is set.
//  3. `<user-home>/.config/git/ignore` otherwise.
//
// The returned path is an absolute, cleaned path and is guaranteed to refer to
// an existing regular file on disk. If no suitable file can be located the
// function returns `("", false)` — callers are expected to treat this as a
// silent no-op rather than an error.
func resolveHostGlobalGitIgnore() (string, bool) {
	if path, ok := resolveFromGitConfig(); ok {
		zlog.Debug("resolved host global git ignore from git config", zap.String("path", path))
		return path, true
	}

	if path, ok := resolveFromXDGFallback(); ok {
		zlog.Debug("resolved host global git ignore from XDG fallback", zap.String("path", path))
		return path, true
	}

	zlog.Debug("no host global git ignore file found")
	return "", false
}

// resolveFromGitConfig returns the absolute path to the global excludes file
// configured via `git config --global --get core.excludesFile` if (and only if)
// the file currently exists as a regular file on disk.
func resolveFromGitConfig() (string, bool) {
	output, _, err := maybeResultOf("git config --global --get core.excludesFile")
	if err != nil {
		zlog.Debug("git config --global --get core.excludesFile returned error (treated as unset)", zap.Error(err))
		return "", false
	}

	value := strings.TrimSpace(output)
	if value == "" {
		return "", false
	}

	expanded, ok := expandHomeReferences(value)
	if !ok {
		return "", false
	}

	return existingRegularFile(expanded)
}

// resolveFromXDGFallback returns the absolute path to the XDG-default global
// excludes file location (`$XDG_CONFIG_HOME/git/ignore` or
// `<home>/.config/git/ignore`) when that file exists as a regular file.
func resolveFromXDGFallback() (string, bool) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return existingRegularFile(filepath.Join(xdg, "git", "ignore"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		zlog.Debug("unable to resolve user home directory for XDG fallback", zap.Error(err))
		return "", false
	}

	return existingRegularFile(filepath.Join(home, ".config", "git", "ignore"))
}

// expandHomeReferences expands a leading `~` (with or without a trailing slash)
// and any embedded `$HOME` token in the provided value using the current
// user's home directory. It returns the expanded string plus a boolean
// indicating whether the resolution succeeded (it can only fail when the home
// directory itself cannot be resolved).
func expandHomeReferences(value string) (string, bool) {
	needsHome := strings.HasPrefix(value, "~") || strings.Contains(value, "$HOME")
	if !needsHome {
		return value, true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		zlog.Debug("unable to resolve user home directory for ~ / $HOME expansion", zap.Error(err))
		return "", false
	}

	if value == "~" {
		value = home
	} else if strings.HasPrefix(value, "~/") {
		value = filepath.Join(home, value[2:])
	}

	value = strings.ReplaceAll(value, "$HOME", home)
	return value, true
}

// existingRegularFile resolves `path` to an absolute, cleaned form and verifies
// it points at an existing regular file. Symlinks pointing to regular files
// are accepted (we follow them via `os.Stat`).
func existingRegularFile(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		zlog.Debug("unable to make path absolute", zap.String("path", path), zap.Error(err))
		return "", false
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		zlog.Debug("global git ignore candidate not usable", zap.String("path", abs), zap.Error(err))
		return "", false
	}
	if !info.Mode().IsRegular() {
		zlog.Debug("global git ignore candidate is not a regular file", zap.String("path", abs))
		return "", false
	}

	return abs, true
}

func latestTag(remote string) (latestTag string) {
	defer func() {
		zlog.Debug("latest tag", zap.String("found", latestTag))
	}()

	// We use `maybeResultOf` but ignore error so no error is printed
	output, _, _ := maybeResultOf("git -c 'versionsort.suffix=-' ls-remote --exit-code --refs --sort='version:refname' --tags", remote, "'*.*.*'")

	lines := getLines(output)
	if len(lines) == 0 {
		return ""
	}

	lastLine := lines[len(lines)-1]
	groups := remoteTagRegex.FindStringSubmatch(lastLine)
	cli.Ensure(len(groups) > 1, "Unable to extract tag regex from line %q", lastLine)

	return groups[1]
}
