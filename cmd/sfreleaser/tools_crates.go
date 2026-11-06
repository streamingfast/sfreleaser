package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var cratesIOAPIURL = "https://crates.io/api/v1"

const cratesIOTrustedPublishingTokenPrefix = "cio_tp_"

var ToolsCratesSetupTrustedPublisherCmd = Command(toolsCratesSetupTrustedPublisherE,
	"setup-trusted-publisher",
	"Adds this repository's release workflow as a crates.io Trusted Publisher of its crates",
	Flags(func(flags *pflag.FlagSet) {
		flags.StringArray("crate", nil, "The crate to configure, repeatable, defaults to 'release.rust-crates' from '.sfreleaser' or else every publishable crate of the Cargo workspace")
		flags.String("workflow", "publish-release.yml", "The file name, in '.github/workflows', of the workflow that publishes the crates")
		flags.String("environment", "", "The GitHub Actions environment the publishing job must run in, any when empty")
		flags.Bool("trusted-publishing-only", false, "Also make Trusted Publishing the only way to publish the crates, which refuses API tokens from then on")
		flags.Bool("dry-run", false, "Print what would be configured without changing anything on crates.io")
	}),
	Description(`
		Configures crates.io Trusted Publishing for the crates of the current repository, so that the
		release workflow publishes them with a short-lived token exchanged from GitHub's OIDC token
		(the 'rust-lang/crates-io-auth-action' action) instead of a stored crates.io token.

		The GitHub repository is the one of the git remote ('--git-remote', 'origin' by default),
		'--repository' overrides it. A crate that already has this exact configuration is left as is.

		The crates.io API token comes from CARGO_REGISTRY_TOKEN or Cargo's credentials file. Its
		user must own every crate (a team owner is not enough), have a verified email and a linked
		GitHub account, and a scoped token needs the 'trusted-publishing' scope.

		crates.io only accepts this configuration for a crate that is already published, a new
		crate's first version is published with an API token.
	`),
	ExamplePrefixed("sfreleaser tools crates setup-trusted-publisher", `
		# Configure every crate of the workspace for '.github/workflows/publish-release.yml'
		--dry-run

		# Configure specific crates for another workflow
		--crate substreams --crate substreams-macro --workflow release.yml
	`),
)

func toolsCratesSetupTrustedPublisherE(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)
	dryRun := sflags.MustGetBool(cmd, "dry-run")

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	owner, repository := trustedPublisherRepository(cmd, global)
	config := trustedPublisherConfig{
		RepositoryOwner:  owner,
		RepositoryName:   repository,
		WorkflowFilename: sflags.MustGetString(cmd, "workflow"),
	}
	if environment := sflags.MustGetString(cmd, "environment"); environment != "" {
		config.Environment = &environment
	}

	if err := validateWorkflowFilename(config.WorkflowFilename); err != nil {
		return err
	}

	crates := sflags.MustGetStringArray(cmd, "crate")
	if len(crates) == 0 {
		crates = viper.GetStringSlice("release.rust-crates")
	}
	if len(crates) == 0 {
		if !cli.FileExists("Cargo.toml") {
			return fmt.Errorf("no Cargo.toml in %s, run it in the repository of the crates or pass them with '--crate'", global.WorkingDirectory)
		}
		ensureCommandExist("cargo", "The 'cargo' utility is required to list the crates of the workspace, or pass them with '--crate'.")

		var err error
		crates, err = parseCargoMetadataPublishableCrates([]byte(resultOf("cargo metadata --format-version 1 --no-deps")))
		if err != nil {
			return err
		}
	}
	if len(crates) == 0 {
		return fmt.Errorf("no crate to configure, the workspace has no publishable crate, pass them with '--crate'")
	}

	token, source := cratesRegistryToken()
	if token == "" {
		return fmt.Errorf("no crates.io token found, set CARGO_REGISTRY_TOKEN or run 'cargo login'")
	}
	if strings.HasPrefix(token, cratesIOTrustedPublishingTokenPrefix) {
		return fmt.Errorf("the crates.io token from %s is a Trusted Publishing token, which cannot configure crates, use an API token", source)
	}

	fmt.Printf("Configuring %d crate(s) for workflow %s of %s/%s with the crates.io token from %s\n", len(crates), config.WorkflowFilename, config.RepositoryOwner, config.RepositoryName, source)
	if dryRun {
		fmt.Println("Dry run, nothing is changed on crates.io")
	}
	fmt.Println()

	client := &cratesIOClient{baseURL: cratesIOAPIURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
	trustedOnly := sflags.MustGetBool(cmd, "trusted-publishing-only")

	var failed []string
	for _, crate := range crates {
		if err := setupCrateTrustedPublisher(context.Background(), client, crate, config, trustedOnly, dryRun); err != nil {
			fmt.Printf("  %s: %s\n", crate, err)
			failed = append(failed, crate)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d crate(s) failed: %s", len(failed), strings.Join(failed, ", "))
	}

	fmt.Println()
	fmt.Println(dedent(`
		The release workflow needs 'id-token: write' and a 'rust-lang/crates-io-auth-action@v1' step
		before 'sfreleaser release publish', its 'token' output passed as CARGO_REGISTRY_TOKEN.
	`))

	return nil
}

func setupCrateTrustedPublisher(ctx context.Context, client *cratesIOClient, crate string, config trustedPublisherConfig, trustedOnly bool, dryRun bool) error {
	config.Crate = crate

	existing, err := client.listGitHubConfigs(ctx, crate)
	if err != nil {
		return err
	}

	if slices.ContainsFunc(existing, config.sameAs) {
		fmt.Printf("  %s: already configured\n", crate)
	} else if dryRun {
		fmt.Printf("  %s: would be configured%s\n", crate, otherConfigsNote(existing))
	} else {
		if err := client.createGitHubConfig(ctx, config); err != nil {
			return err
		}
		fmt.Printf("  %s: configured%s\n", crate, otherConfigsNote(existing))
	}

	if !trustedOnly {
		return nil
	}

	if dryRun {
		fmt.Printf("  %s: would only accept Trusted Publishing\n", crate)
		return nil
	}

	if err := client.setTrustedPublishingOnly(ctx, crate); err != nil {
		return err
	}
	fmt.Printf("  %s: only accepts Trusted Publishing\n", crate)

	return nil
}

func otherConfigsNote(existing []trustedPublisherConfig) string {
	if len(existing) == 0 {
		return ""
	}

	var others []string
	for _, config := range existing {
		others = append(others, config.String())
	}

	return fmt.Sprintf(" (already trusts %s)", strings.Join(others, ", "))
}

// trustedPublisherRepository is the GitHub repository of '--repository' or else of the git
// remote, falling back to '--owner' and '--project'.
func trustedPublisherRepository(cmd *cobra.Command, global *GlobalModel) (owner string, repository string) {
	if _, provided := sflags.MustGetStringProvided(cmd, "repository"); provided {
		return global.Owner, global.Project
	}

	remoteURL, _, err := maybeResultOf("git remote get-url", global.GitRemote)
	if err == nil {
		if owner, repository, ok := parseGitHubRemoteURL(strings.TrimSpace(remoteURL)); ok {
			return owner, repository
		}
	}

	cli.Quit("Unable to find the GitHub repository from git remote %q, pass it with '--repository <owner>/<name>'", global.GitRemote)
	return "", ""
}

var gitHubRemoteURLRegex = regexp.MustCompile(`^(?:https?://(?:[^@/]+@)?github\.com/|ssh://git@github\.com/|git@github\.com:)([^/]+)/([^/]+?)(?:\.git)?/?$`)

func parseGitHubRemoteURL(remoteURL string) (owner string, repository string, ok bool) {
	match := gitHubRemoteURLRegex.FindStringSubmatch(remoteURL)
	if match == nil {
		return "", "", false
	}

	return match[1], match[2], true
}

var workflowFilenameRegex = regexp.MustCompile(`^[^/\\]+\.ya?ml$`)

func validateWorkflowFilename(filename string) error {
	if !workflowFilenameRegex.MatchString(filename) {
		return fmt.Errorf("invalid workflow %q, expected the file name only, like 'publish-release.yml'", filename)
	}

	return nil
}

// parseCargoMetadataPublishableCrates lists the workspace crates that can go to crates.io,
// skipping those with 'publish = false' or restricted to other registries.
func parseCargoMetadataPublishableCrates(metadata []byte) ([]string, error) {
	var parsed struct {
		Packages []struct {
			Name    string    `json:"name"`
			Publish *[]string `json:"publish"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(metadata, &parsed); err != nil {
		return nil, fmt.Errorf("parsing 'cargo metadata' output: %w", err)
	}

	var crates []string
	for _, pkg := range parsed.Packages {
		if pkg.Publish != nil && !slices.Contains(*pkg.Publish, "crates-io") {
			continue
		}
		crates = append(crates, pkg.Name)
	}

	return crates, nil
}

type trustedPublisherConfig struct {
	Crate            string  `json:"crate"`
	RepositoryOwner  string  `json:"repository_owner"`
	RepositoryName   string  `json:"repository_name"`
	WorkflowFilename string  `json:"workflow_filename"`
	Environment      *string `json:"environment"`
}

func (c trustedPublisherConfig) sameAs(other trustedPublisherConfig) bool {
	return strings.EqualFold(c.RepositoryOwner, other.RepositoryOwner) &&
		strings.EqualFold(c.RepositoryName, other.RepositoryName) &&
		c.WorkflowFilename == other.WorkflowFilename &&
		strings.EqualFold(stringOrEmpty(c.Environment), stringOrEmpty(other.Environment))
}

func (c trustedPublisherConfig) String() string {
	out := c.RepositoryOwner + "/" + c.RepositoryName + " " + c.WorkflowFilename
	if c.Environment != nil {
		out += " (environment " + *c.Environment + ")"
	}

	return out
}

func stringOrEmpty(in *string) string {
	if in == nil {
		return ""
	}

	return *in
}

type cratesIOClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func (c *cratesIOClient) listGitHubConfigs(ctx context.Context, crate string) ([]trustedPublisherConfig, error) {
	// crates.io allows 5 configurations per crate, the first page has them all.
	var response struct {
		GitHubConfigs []trustedPublisherConfig `json:"github_configs"`
	}
	if err := c.do(ctx, http.MethodGet, "/trusted_publishing/github_configs?crate="+url.QueryEscape(crate), nil, &response); err != nil {
		return nil, fmt.Errorf("listing its Trusted Publishers: %w", err)
	}

	return response.GitHubConfigs, nil
}

func (c *cratesIOClient) createGitHubConfig(ctx context.Context, config trustedPublisherConfig) error {
	body := map[string]trustedPublisherConfig{"github_config": config}
	if err := c.do(ctx, http.MethodPost, "/trusted_publishing/github_configs", body, nil); err != nil {
		return fmt.Errorf("adding the Trusted Publisher: %w", err)
	}

	return nil
}

func (c *cratesIOClient) setTrustedPublishingOnly(ctx context.Context, crate string) error {
	body := map[string]map[string]bool{"crate": {"trustpub_only": true}}
	if err := c.do(ctx, http.MethodPatch, "/crates/"+url.PathEscape(crate), body, nil); err != nil {
		return fmt.Errorf("requiring Trusted Publishing: %w", err)
	}

	return nil
}

func (c *cratesIOClient) do(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("building crates.io request: %w", err)
	}
	request.Header.Set("Authorization", c.token)
	request.Header.Set("User-Agent", "sfreleaser (https://github.com/streamingfast/sfreleaser)")
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("querying crates.io: %w", err)
	}
	defer response.Body.Close()

	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading crates.io response: %w", err)
	}

	if response.StatusCode/100 != 2 {
		return cratesIOError(response.Status, content)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(content, out); err != nil {
		return fmt.Errorf("parsing crates.io response: %w", err)
	}

	return nil
}

func cratesIOError(status string, content []byte) error {
	var payload struct {
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(content, &payload)

	if len(payload.Errors) == 0 || payload.Errors[0].Detail == "" {
		return fmt.Errorf("crates.io answered %s", status)
	}

	detail := payload.Errors[0].Detail
	switch detail {
	case cratesIOAuthFailed:
		return fmt.Errorf("crates.io rejected the token, it is revoked, expired or unknown")
	case cratesIOScopedTokenValid:
		return fmt.Errorf("the token cannot manage this crate, it needs the 'trusted-publishing' scope and this crate in its crate scopes")
	}

	return fmt.Errorf("crates.io answered %s: %s", status, detail)
}
