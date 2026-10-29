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
	"github.com/spf13/viper"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var PublishBrewCmd = Command(publishBrew,
	"publish-brew <version>",
	"Update the Homebrew formula from the archives of a published release",
	Flags(func(flags *pflag.FlagSet) {
		flags.String("brew-tap-repo", "homebrew-tap", "The GitHub project name of the tap, the repo owner is defined by 'owner' config value, defaults to the 'release.brew-tap-repo' config value")
		flags.Bool("brew-pull-request", false, "Open a pull request against the tap instead of committing the formula straight to its default branch, also enabled by the 'release.brew-pull-request' config value")
		flags.Bool("brew-disabled", false, "Skip the formula update, also enabled by the 'release.brew-disabled' config value")
		flags.Bool("merge", false, "With --brew-pull-request, wait for the checks of the pull request and merge it, as an admin since nobody reviews it")
		flags.Duration("checks-timeout", 5*time.Minute, "With --merge, how long to wait for the checks of the pull request to start")
	}),
	Description(`
		Renders the Homebrew formula of a Go application from the archives attached to its
		published GitHub release and commits it to the tap, with HOMEBREW_TAP_TOKEN when set.

		The release must be published, the formula points at its archives which are not
		downloadable while it is a draft. A pre-release never updates the formula, and a formula
		already up to date is left untouched, so running it again is safe.
	`),
	ExamplePrefixed("sfreleaser publish-brew", `
		v1.2.0 --brew-pull-request --merge
	`),
)

func publishBrew(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("the <version> argument is required")
	}

	version := args[0]
	if err := validVersion(version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	global := mustGetGlobal(cmd)
	if global.Language != LanguageGolang || global.Variant != VariantApplication {
		return fmt.Errorf("only the releases of a Go application have a Homebrew formula")
	}

	ensureCommandExist("gh", "The GitHub CLI utility (https://cli.github.com/) is required to read the release and update the tap.")

	if releaseConfigBool(cmd, "brew-disabled") {
		fmt.Println("The Homebrew formula is disabled for this project ('release.brew-disabled')")
		return nil
	}

	tapOwner, tapName := tapRepository(global, releaseConfigString(cmd, "brew-tap-repo"))
	release := &ReleaseModel{
		Version: version,
		Brew: &BrewReleaseModel{
			PullRequest:  releaseConfigBool(cmd, "brew-pull-request"),
			TapRepoOwner: tapOwner,
			TapRepoName:  tapName,
			HasTapToken:  homebrewTapToken() != "",
		},
	}

	merge := sflags.MustGetBool(cmd, "merge")
	if merge && !release.Brew.PullRequest {
		return fmt.Errorf("--merge requires --brew-pull-request")
	}

	if merge && !release.Brew.HasTapToken {
		return fmt.Errorf("--merge requires %s, the pull request is merged with it", homebrewTapTokenEnv)
	}

	if isPrereleaseVersion(version) {
		fmt.Printf("Release %s is a pre-release, the Homebrew formula is not updated\n", version)
		return nil
	}

	repository := global.Owner + "/" + global.Project
	state, err := publishedReleaseState(repository, version)
	if err != nil {
		return err
	}

	if state.IsDraft {
		return fmt.Errorf("release %s of %s is still a draft, publish it first, the formula points at its archives", version, repository)
	}

	if state.IsPrerelease {
		fmt.Printf("Release %s is marked as pre-release, the Homebrew formula is not updated\n", version)
		return nil
	}

	directory, err := os.MkdirTemp("", "sfreleaser-brew-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)

	archives, err := downloadReleaseArchives(repository, version, global.Project, state.Assets, directory)
	if err != nil {
		return err
	}

	update, err := publishBrewFormula(global, release, archives)
	if err != nil {
		return err
	}

	if !merge || update.Unchanged {
		return nil
	}

	if update.PullRequest == "" {
		return fmt.Errorf("no open pull request found for the Homebrew formula of %s", version)
	}

	return mergeBrewPullRequest(update, sflags.MustGetDuration(cmd, "checks-timeout"))
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

// mergeBrewPullRequest waits for the checks of the formula pull request to pass and merges it,
// only if it still holds the commit we pushed.
func mergeBrewPullRequest(update *brewFormulaUpdate, checksTimeout time.Duration) error {
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

	if _, err := runGH("pr", "merge", update.PullRequest, "--squash", "--admin", "--delete-branch", "--match-head-commit", update.Commit); err != nil {
		return fmt.Errorf("merging %s: %w", update.PullRequest, err)
	}

	fmt.Printf("Merged %s\n", update.PullRequest)
	return nil
}

// releaseConfigString is the flag when given, else the project's 'release.<name>' config value,
// so 'publish-brew' updates the tap 'release' is configured with.
func releaseConfigString(cmd *cobra.Command, name string) string {
	if !cmd.Flags().Changed(name) && viper.IsSet("release."+name) {
		return viper.GetString("release." + name)
	}

	return sflags.MustGetString(cmd, name)
}

func releaseConfigBool(cmd *cobra.Command, name string) bool {
	if !cmd.Flags().Changed(name) && viper.IsSet("release."+name) {
		return viper.GetBool("release." + name)
	}

	return sflags.MustGetBool(cmd, name)
}
