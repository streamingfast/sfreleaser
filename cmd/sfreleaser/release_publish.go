package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var ReleasePublishCmd = Command(releasePublish,
	"publish [<version>]",
	"Publish a prepared release and merge its Homebrew pull request",
	Flags(releaseStepFlags),
	Description(`
		Finishes what 'sfreleaser release prepare' started, it is safe to run again. The Homebrew
		pull request is opened if it is missing.

		The checks of the Homebrew pull request must pass first. A release still in draft then
		has its crates or Substreams package published, followed by the release itself. The
		pull request is merged last, as the formula points at the archives of the published
		release.

		A release already published, from the GitHub UI for example, only gets its pull request
		merged, its crates or Substreams package are not published.

		The version defaults to the tag of the GitHub Actions 'release' event.
	`),
	ExamplePrefixed("sfreleaser release publish", `
		v1.2.0
	`),
	releaseExecute(releasePublish),
	OnCommandError(onReleaseError),
)

// releaseStepFlags are the flags of the release subcommands that update the Homebrew formula.
func releaseStepFlags(flags *pflag.FlagSet) {
	releaseFlags(flags)
	flags.Duration("checks-timeout", 5*time.Minute, "How long to wait for the checks of the Homebrew pull request to start")
}

func releasePublish(cmd *cobra.Command, args []string) error {
	version := ""
	if len(args) > 0 {
		version = args[0]
	} else if os.Getenv("GITHUB_EVENT_NAME") == "release" {
		version = os.Getenv("GITHUB_REF_NAME")
	}

	if version == "" {
		return fmt.Errorf("the <version> argument is required")
	}

	if err := validVersion(version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	global := mustGetGlobal(cmd)
	release := &ReleaseModel{Version: version}
	release.populate(cmd, global)

	global.ensureValidForRelease()
	release.ensureValidForRelease(global)

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	verifyTools(false)
	useGitHubTokenForGh()

	state, err := publishedReleaseState(global.Owner+"/"+global.Project, version)
	if err != nil {
		return err
	}

	var update *brewFormulaUpdate
	if brewFormulaApplies(global, release) && !state.IsPrerelease {
		if !release.Brew.HasTapToken {
			return fmt.Errorf("%s is required, the Homebrew pull request is merged with it", homebrewTapTokenEnv)
		}

		if update, err = openBrewPullRequest(global, release, state); err != nil {
			return err
		}

		if !update.Unchanged {
			if err := waitForBrewPullRequestChecks(update, sflags.MustGetDuration(cmd, "checks-timeout")); err != nil {
				return err
			}
		}
	}

	if state.IsDraft {
		if release.Rust != nil && len(release.Rust.Crates) > 0 {
			verifyRustTools()
			release.Rust.SinglePublishInvocation = resolveRustCratesSinglePublishInvocation(release.Rust.CratesPublishMode)
		}

		publishReleaseNow(global, release)
	} else {
		fmt.Printf("Release %s is already published\n", version)
		if release.Substreams != nil {
			printSubstreamsRegistryNotPublishedMessage(release.Substreams)
		} else if release.Rust != nil && len(release.Rust.Crates) > 0 {
			printRustCratesNotPublishedMessage(release.Rust)
		}
	}

	if update == nil || update.Unchanged {
		return nil
	}

	return mergeBrewPullRequest(update)
}

// brewFormulaApplies reports whether the release updates the Homebrew formula, pre-releases
// never do.
func brewFormulaApplies(global *GlobalModel, release *ReleaseModel) bool {
	return usesGoreleaser(global, release) && !release.Brew.Disabled && !isPrereleaseVersion(release.Version)
}

// openBrewPullRequest renders the formula from the archives attached to the release, draft or
// not, and opens the pull request updating it, or finds the one already open.
func openBrewPullRequest(global *GlobalModel, release *ReleaseModel, state *releaseStateInfo) (*brewFormulaUpdate, error) {
	release.Brew.PullRequest = true

	directory, err := os.MkdirTemp("", "sfreleaser-brew-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)

	archives, err := downloadReleaseArchives(global.Owner+"/"+global.Project, release.Version, global.Project, state.Assets, directory)
	if err != nil {
		return nil, err
	}

	return publishBrewFormula(global, release, archives)
}

type releaseStateInfo struct {
	IsDraft      bool `json:"isDraft"`
	IsPrerelease bool `json:"isPrerelease"`
	Assets       []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

func publishedReleaseState(repository, version string) (*releaseStateInfo, error) {
	output, _, err := maybeResultOf("gh release view", "'"+version+"'", "--repo", repository, "--json isDraft,isPrerelease,assets")
	if err != nil {
		return nil, fmt.Errorf("reading release %s of %s: %w: %s", version, repository, err, strings.TrimSpace(output))
	}

	state := &releaseStateInfo{}
	if err := json.Unmarshal([]byte(output), state); err != nil {
		return nil, fmt.Errorf("decoding release %s of %s: %w", version, repository, err)
	}

	return state, nil
}

// downloadReleaseArchives downloads the archive of every native target attached to the release
// and returns them with their SHA256.
func downloadReleaseArchives(repository, version, project string, assets []struct {
	Name string `json:"name"`
}, directory string) ([]nativeArchive, error) {
	attached := map[string]bool{}
	for _, asset := range assets {
		attached[asset.Name] = true
	}

	var archives []nativeArchive
	for _, target := range nativeTargets {
		name := nativeArchiveName(project, target)
		if !attached[name] {
			continue
		}

		fmt.Printf("Downloading %s\n", name)
		if output, _, err := maybeResultOf("gh release download", "'"+version+"'", "--repo", repository, "--pattern", "'"+name+"'", "--dir", "'"+directory+"'", "--clobber"); err != nil {
			return nil, fmt.Errorf("downloading %s: %w: %s", name, err, strings.TrimSpace(output))
		}

		sum, err := fileSHA256(filepath.Join(directory, name))
		if err != nil {
			return nil, fmt.Errorf("hashing %s: %w", name, err)
		}

		archives = append(archives, nativeArchive{Target: target, Name: name, SHA256: sum})
	}

	if len(archives) == 0 {
		return nil, fmt.Errorf("release %s of %s has no '%s_<os>_<arch>.tar.gz' archive", version, repository, project)
	}

	return archives, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// waitForBrewPullRequestChecks waits for the checks of the formula pull request to pass.
func waitForBrewPullRequestChecks(update *brewFormulaUpdate, checksTimeout time.Duration) error {
	deadline := time.Now().Add(checksTimeout)
	for {
		output, err := runGH("pr", "view", update.PullRequest, "--json", "statusCheckRollup", "--jq", ".statusCheckRollup | length")
		if err != nil {
			return fmt.Errorf("reading the checks of %s: %w", update.PullRequest, err)
		}

		if count, _ := strconv.Atoi(strings.TrimSpace(output)); count > 0 {
			break
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("no check started on %s within %s", update.PullRequest, checksTimeout)
		}

		time.Sleep(10 * time.Second)
	}

	fmt.Printf("Waiting for the checks of %s\n", update.PullRequest)
	if _, err := runGH("pr", "checks", update.PullRequest, "--watch", "--fail-fast"); err != nil {
		return fmt.Errorf("checks of %s failed: %w", update.PullRequest, err)
	}

	return nil
}

// mergeBrewPullRequest merges the formula pull request, as an admin since nobody reviews it,
// only if it still holds the commit the checks passed on.
func mergeBrewPullRequest(update *brewFormulaUpdate) error {
	if _, err := runGH("pr", "merge", update.PullRequest, "--squash", "--admin", "--delete-branch", "--match-head-commit", update.Commit); err != nil {
		return fmt.Errorf("merging %s: %w", update.PullRequest, err)
	}

	fmt.Printf("Merged %s\n", update.PullRequest)
	return nil
}
