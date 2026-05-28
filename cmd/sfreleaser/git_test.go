package main

import (
	"os"
	"path/filepath"
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
