package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeGitIgnoreFile creates a file at `path` (creating parent dirs as needed)
// with sample gitignore content and returns the absolute path.
func writeGitIgnoreFile(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("*.tmp\n"), 0o644))
	return path
}

// writeGitGlobalConfig writes a temporary `.gitconfig` file pointing
// `core.excludesFile` at the provided value and wires Git to use it via
// `GIT_CONFIG_GLOBAL`. The env var is set via `t.Setenv` so it is restored
// automatically at test teardown.
func writeGitGlobalConfig(t *testing.T, excludesFile string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gitconfig")
	content := "[core]\n\texcludesFile = " + excludesFile + "\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)
	// GIT_CONFIG_SYSTEM is set to /dev/null to keep tests deterministic
	// regardless of any system-wide git config installed on the runner.
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// clearGitGlobalConfig points GIT_CONFIG_GLOBAL at an empty config so the
// helper observes "core.excludesFile is unset" rather than picking up the
// developer's real ~/.gitconfig.
func clearGitGlobalConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gitconfig")
	require.NoError(t, os.WriteFile(configPath, []byte(""), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// writeGitGlobalConfigContent writes `content` as the global Git config used by
// commands run from the test, isolating them from the developer's real
// `~/.gitconfig` and from any system-wide config installed on the runner.
func writeGitGlobalConfigContent(t *testing.T, content string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", configPath)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// initGitRepositoryWithCommit creates a Git repository holding a single commit
// and makes it the working directory for the duration of the test. The PTY is
// disabled so `run` and friends keep a deterministic output under `go test`.
func initGitRepositoryWithCommit(t *testing.T) string {
	t.Helper()

	previousPtyDisabled := ptyDisabled
	ptyDisabled = true
	t.Cleanup(func() { ptyDisabled = previousPtyDisabled })

	directory := t.TempDir()
	t.Chdir(directory)
	clearGitGlobalConfig(t)

	t.Setenv("GIT_AUTHOR_NAME", "sfreleaser")
	t.Setenv("GIT_AUTHOR_EMAIL", "sfreleaser@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "sfreleaser")
	t.Setenv("GIT_COMMITTER_EMAIL", "sfreleaser@example.com")

	runGitInTest(t, "init", "--initial-branch=main")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "README.md"), []byte("test\n"), 0o644))
	runGitInTest(t, "add", "README.md")
	runGitInTest(t, "commit", "--no-gpg-sign", "-m", "initial commit")

	return directory
}

// runGitInTest runs a Git command for test setup purposes, reporting the
// command output on failure. Setup does not go through `run` because that one
// terminates the process (and so the whole test binary) on error.
func runGitInTest(t *testing.T, arguments ...string) {
	t.Helper()
	output, err := exec.Command("git", arguments...).CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(arguments, " "), output)
}

func Test_resolveHostGlobalGitIgnore_FromGitConfig_AbsolutePath(t *testing.T) {
	tmp := t.TempDir()
	ignorePath := writeGitIgnoreFile(t, filepath.Join(tmp, "ignore"))

	writeGitGlobalConfig(t, ignorePath)

	got, ok := resolveHostGlobalGitIgnore()
	require.True(t, ok, "expected detection to succeed")
	require.Equal(t, ignorePath, got)
}

func Test_resolveHostGlobalGitIgnore_FromGitConfig_TildePrefix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ignorePath := writeGitIgnoreFile(t, filepath.Join(home, ".gitignore_global"))

	// Note: configured value uses `~/` which Git would expand, but we are
	// the ones reading the raw value so we must perform the expansion.
	writeGitGlobalConfig(t, "~/.gitignore_global")

	got, ok := resolveHostGlobalGitIgnore()
	require.True(t, ok)
	require.Equal(t, ignorePath, got)
}

func Test_resolveHostGlobalGitIgnore_FromGitConfig_HomeVariable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ignorePath := writeGitIgnoreFile(t, filepath.Join(home, ".gitignore_global"))

	writeGitGlobalConfig(t, "$HOME/.gitignore_global")

	got, ok := resolveHostGlobalGitIgnore()
	require.True(t, ok)
	require.Equal(t, ignorePath, got)
}

func Test_resolveHostGlobalGitIgnore_FromGitConfig_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist")

	writeGitGlobalConfig(t, missing)
	// Ensure XDG fallback also misses so we get a clean negative.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got, ok := resolveHostGlobalGitIgnore()
	require.False(t, ok, "missing file must not be reported as resolved")
	require.Equal(t, "", got)
}

func Test_resolveHostGlobalGitIgnore_FromGitConfig_DirectoryNotFile(t *testing.T) {
	dir := t.TempDir()
	// Point at the directory itself (not a file inside it).
	writeGitGlobalConfig(t, dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got, ok := resolveHostGlobalGitIgnore()
	require.False(t, ok, "directory must not be accepted as a global ignore file")
	require.Equal(t, "", got)
}

func Test_resolveHostGlobalGitIgnore_XDGFallback(t *testing.T) {
	clearGitGlobalConfig(t)

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	ignorePath := writeGitIgnoreFile(t, filepath.Join(xdg, "git", "ignore"))

	got, ok := resolveHostGlobalGitIgnore()
	require.True(t, ok)
	require.Equal(t, ignorePath, got)
}

func Test_resolveHostGlobalGitIgnore_HomeFallback(t *testing.T) {
	clearGitGlobalConfig(t)

	// XDG_CONFIG_HOME unset → fall back to <home>/.config/git/ignore.
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	ignorePath := writeGitIgnoreFile(t, filepath.Join(home, ".config", "git", "ignore"))

	got, ok := resolveHostGlobalGitIgnore()
	require.True(t, ok)
	require.Equal(t, ignorePath, got)
}

func Test_resolveHostGlobalGitIgnore_NothingConfigured(t *testing.T) {
	clearGitGlobalConfig(t)

	// Point HOME and XDG_CONFIG_HOME at empty temp dirs so no fallback file
	// can be found.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got, ok := resolveHostGlobalGitIgnore()
	require.False(t, ok)
	require.Equal(t, "", got)
}

func Test_createTemporaryTag_WithTagGpgSignEnabled(t *testing.T) {
	initGitRepositoryWithCommit(t)

	// `gpg.program` and `GIT_EDITOR` are neutered so that a regression fails fast
	// instead of hanging on an editor or on a GPG passphrase prompt.
	writeGitGlobalConfigContent(t, "[tag]\n\tgpgsign = true\n[gpg]\n\tprogram = /bin/false\n")
	t.Setenv("GIT_EDITOR", "true")

	createTemporaryTag("v1.2.3")

	objectType := strings.TrimSpace(resultOf("git cat-file -t v1.2.3"))
	require.Equal(t, "commit", objectType, "temporary tag must stay lightweight so it is never signed nor annotated")
}

func Test_createTemporaryTag_ThenDeleteTemporaryTag(t *testing.T) {
	initGitRepositoryWithCommit(t)
	clearGitGlobalConfig(t)

	createTemporaryTag("v1.2.3")
	require.Equal(t, "v1.2.3", strings.TrimSpace(resultOf("git tag --list")))

	deleteTemporaryTag("v1.2.3")
	require.Equal(t, "", strings.TrimSpace(resultOf("git tag --list")))
}
