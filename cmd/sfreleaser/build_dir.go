package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/streamingfast/cli"
)

// resolveBuildDirectory returns the absolute directory sfreleaser writes its generated files
// to (Goreleaser config, release notes, token env file, binaries). It lives outside of the
// project so the Git working tree is never made dirty by a build, in order:
//
//   - the configured directory, resolved against the working directory when relative
//   - '$RUNNER_TEMP/sfreleaser/<project>' on GitHub Actions
//   - '<user cache dir>/sfreleaser/build/<project>-<hash of the working directory>'
func resolveBuildDirectory(configured, workingDirectory, project string) (string, error) {
	if configured != "" {
		if filepath.IsAbs(configured) {
			return filepath.Clean(configured), nil
		}

		return filepath.Join(workingDirectory, configured), nil
	}

	if runnerTemp := os.Getenv("RUNNER_TEMP"); runnerTemp != "" {
		return filepath.Join(runnerTemp, "sfreleaser", project), nil
	}

	cacheDir, err := sfreleaserCacheDir()
	if err != nil {
		return "", err
	}

	// Two checkouts of the same project (worktrees, clones) get their own directory.
	hash := sha256.Sum256([]byte(workingDirectory))

	return filepath.Join(cacheDir, "build", project+"-"+hex.EncodeToString(hash[:])[:8]), nil
}

func mustResolveBuildDirectory(configured, workingDirectory, project string) string {
	directory, err := resolveBuildDirectory(configured, workingDirectory, project)
	cli.NoError(err, "Unable to resolve the build directory")

	return directory
}

// prepareBuildDirectory creates the build directory and tells where it is.
func prepareBuildDirectory(global *GlobalModel) {
	cli.NoError(os.MkdirAll(global.BuildDirectory, os.ModePerm), "Unable to create build directory %q", global.BuildDirectory)
	fmt.Printf("Using build directory %q\n", global.BuildDirectory)
}
