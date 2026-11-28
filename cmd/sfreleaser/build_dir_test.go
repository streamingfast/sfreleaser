package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_resolveBuildDirectory(t *testing.T) {
	t.Run("configured absolute", func(t *testing.T) {
		t.Setenv("RUNNER_TEMP", "/runner/temp")

		directory, err := resolveBuildDirectory("/abs/build/", "/work/project", "project")
		require.NoError(t, err)
		assert.Equal(t, "/abs/build", directory)
	})

	t.Run("configured relative", func(t *testing.T) {
		directory, err := resolveBuildDirectory("build", "/work/project", "project")
		require.NoError(t, err)
		assert.Equal(t, "/work/project/build", directory)
	})

	t.Run("GitHub Actions runner", func(t *testing.T) {
		t.Setenv("RUNNER_TEMP", "/runner/temp")

		directory, err := resolveBuildDirectory("", "/work/project", "project")
		require.NoError(t, err)
		assert.Equal(t, "/runner/temp/sfreleaser/project", directory)
	})

	t.Run("cache directory, per checkout", func(t *testing.T) {
		t.Setenv("RUNNER_TEMP", "")

		cacheDir, err := sfreleaserCacheDir()
		require.NoError(t, err)

		first, err := resolveBuildDirectory("", "/work/project", "project")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(cacheDir, "build"), filepath.Dir(first))
		assert.Regexp(t, `^project-[0-9a-f]{8}$`, filepath.Base(first))

		again, err := resolveBuildDirectory("", "/work/project", "project")
		require.NoError(t, err)
		assert.Equal(t, first, again)

		other, err := resolveBuildDirectory("", "/work/other-checkout/project", "project")
		require.NoError(t, err)
		assert.NotEqual(t, first, other)
	})
}
