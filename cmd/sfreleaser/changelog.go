package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
)

var ChangelogExtractSectionCmd = Command(changelogExtractSection,
	"extract-section [<file>] [<version>]",
	"Extract a specific section from a changelog file, the latest by default",
	Flags(func(flags *pflag.FlagSet) {
		flags.String("start-header-regex", "", "Regex pattern to match section start headers (defaults to '^## .+' or '^## <version>' if version specified)")
		flags.String("end-header-regex", "^## .+", "Regex pattern to match section end headers")
		flags.String("github-output", "", "Path to GITHUB_OUTPUT file for GitHub Actions (format: 'path' or 'variable:path', defaults to 'changelog' variable)")
	}),
	Description(`
		Extracts a specific section from a changelog file.

		Arguments:
		  file     Path to the changelog file (defaults to "CHANGELOG.md") or GitHub URL
		  version  Version to extract (defaults to first section found)

		The file argument can be either:
		- A local file path: "CHANGELOG.md"
		- A GitHub URL: "github://[token:<token>@]<owner>/<repo>/[blob/]<sha>/<file_path>"
		  Token is optional for public repositories. The "/blob/" part is optional.

		GitHub Actions Output:
		The --github-output flag writes the extracted changelog content to the GitHub Actions
		output file in the proper format for use in workflows. It supports two formats:
		- "path": Uses default variable name "changelog"
		- "variable:path": Uses custom variable name

		GitHub Actions Example Usage:

		  - name: Extract Changelog
		    id: changelog
		    run: |
		      curl -L https://github.com/streamingfast/sfreleaser/releases/download/v0.12.1/sfreleaser_linux_x86_64.tar.gz | tar -xz
		      chmod +x sfreleaser

		      ./sfreleaser changelog extract-section \
		        github://token:${{ github.token }}@${{ github.repository }}/$GITHUB_SHA/CHANGELOG.sf.md \
		        --github-output="changelog:$GITHUB_OUTPUT"

		    - name: Release
		      uses: softprops/action-gh-release@v2
		      with:
		        body: ${{ steps.changelog.outputs.changelog }}
		        ...
	`),
	ExamplePrefixed("sfreleaser changelog extract-section", `
		# Extract latest section from default CHANGELOG.md

		# Extract from specific file
		CHANGELOG.md

		# Extract specific version
		CHANGELOG.md v1.2.3

		# Extract using custom regex pattern
		--start-header-regex="## v1\\.2\\..+" CHANGELOG.md

		# Extract from GitHub public repository
		"github://owner/repo/main/CHANGELOG.md"

		# Extract from GitHub using blob URL format
		"github://owner/repo/blob/main/CHANGELOG.md"

		# Extract from GitHub with authentication
		"github://token:ghp_abc123@owner/repo/main/CHANGELOG.md"

		# Extract using GitHub Actions variables
		"github://token:$GITHUB_TOKEN@$GITHUB_REPOSITORY/$GITHUB_SHA/CHANGELOG.md"

		# Write to GitHub Actions output with default 'changelog' variable name
		--github-output="$GITHUB_OUTPUT" CHANGELOG.md

		# Write to GitHub Actions output with custom variable name
		--github-output="release_notes:$GITHUB_OUTPUT" CHANGELOG.md

		# Complete GitHub Actions workflow example
		--github-output="changelog:$GITHUB_OUTPUT" "github://token:${{ github.token }}@${{ github.repository }}/$GITHUB_SHA/CHANGELOG.md"
	`),
)

func changelogExtractSection(cmd *cobra.Command, args []string) error {
	// Default values
	changelogFile := "CHANGELOG.md"
	targetVersion := ""

	// Parse arguments
	if len(args) > 0 {
		changelogFile = args[0]
	}
	if len(args) > 1 {
		targetVersion = args[1]
	}

	// Get flags
	startHeaderRegex, _ := cmd.Flags().GetString("start-header-regex")
	endHeaderRegex, _ := cmd.Flags().GetString("end-header-regex")
	githubOutputFile, _ := cmd.Flags().GetString("github-output")

	// Set default start header regex if not provided
	if startHeaderRegex == "" {
		if targetVersion != "" {
			// Escape the version for regex and create a pattern
			escapedVersion := regexp.QuoteMeta(targetVersion)
			startHeaderRegex = "^## .*" + escapedVersion + ".*"
		} else {
			startHeaderRegex = "^## .+"
		}
	}

	// Ensure end header regex is anchored
	switch endHeaderRegex {
	case "^## .+":
		// Already anchored, keep as is
	case "## .+":
		endHeaderRegex = "^## .+"
	}

	// Debug output
	// fmt.Fprintf(os.Stderr, "Debug: file=%q, version=%q, startRegex=%q, endRegex=%q\n",
	//	changelogFile, targetVersion, startHeaderRegex, endHeaderRegex)

	// Extract section
	section, err := extractChangelogSection(changelogFile, startHeaderRegex, endHeaderRegex)
	if err != nil {
		return err
	}

	if section == "" {
		if targetVersion != "" {
			fmt.Fprintf(os.Stderr, "No section found for version %q in %s\n", targetVersion, changelogFile)
		} else {
			fmt.Fprintf(os.Stderr, "No section found in %s\n", changelogFile)
		}
		os.Exit(1)
	}

	// Handle GitHub Actions output format
	if githubOutputFile != "" {
		variableName := "changelog" // default variable name
		outputFile := githubOutputFile

		// Check if the format is "variable:path"
		if colonIndex := strings.Index(githubOutputFile, ":"); colonIndex != -1 {
			variableName = githubOutputFile[:colonIndex]
			outputFile = githubOutputFile[colonIndex+1:]
		}

		err := writeGitHubOutput(outputFile, variableName, section)
		if err != nil {
			return fmt.Errorf("failed to write GitHub output: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Changelog section written to GitHub Actions output as variable '%s'\n", variableName)
	} else {
		fmt.Print(section)
	}

	return nil
}

// extractChangelogSection extracts a section from a changelog using custom regex patterns
func extractChangelogSection(changelogFile, startHeaderRegex, endHeaderRegex string) (string, error) {
	// Check if it's a GitHub URL
	if strings.HasPrefix(changelogFile, "github://") {
		ghURL, err := parseGitHubURL(changelogFile)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub URL %q: %w", changelogFile, err)
		}

		startRegex, err := regexp.Compile(startHeaderRegex)
		if err != nil {
			return "", fmt.Errorf("invalid start header regex %q: %w", startHeaderRegex, err)
		}

		endRegex, err := regexp.Compile(endHeaderRegex)
		if err != nil {
			return "", fmt.Errorf("invalid end header regex %q: %w", endHeaderRegex, err)
		}

		return readReleaseNotesFromGitHub(ghURL, startRegex, endRegex)
	}

	// Handle local file
	if !cli.FileExists(changelogFile) {
		return "", fmt.Errorf("changelog file %q does not exist", changelogFile)
	}

	startRegex, err := regexp.Compile(startHeaderRegex)
	if err != nil {
		return "", fmt.Errorf("invalid start header regex %q: %w", startHeaderRegex, err)
	}

	endRegex, err := regexp.Compile(endHeaderRegex)
	if err != nil {
		return "", fmt.Errorf("invalid end header regex %q: %w", endHeaderRegex, err)
	}

	return readReleaseNotesWithRegex(changelogFile, startRegex, endRegex)
}

// readReleaseNotesWithRegex is like readReleaseNotes but uses custom regex patterns
func readReleaseNotesWithRegex(changelogFile string, startRegex, endRegex *regexp.Regexp) (string, error) {
	if !cli.FileExists(changelogFile) {
		return "", fmt.Errorf("changelog file %q does not exist", changelogFile)
	}

	file, err := os.Open(changelogFile)
	if err != nil {
		return "", fmt.Errorf("unable to open changelog %q: %w", changelogFile, err)
	}
	defer file.Close()

	foundFirstHeader := false
	var releaseNotes []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if !foundFirstHeader && startRegex.MatchString(line) {
			foundFirstHeader = true
			continue
		}

		if foundFirstHeader {
			if endRegex.MatchString(line) {
				break
			}

			releaseNotes = append(releaseNotes, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading changelog: %w", err)
	}

	if !foundFirstHeader {
		return "", nil
	}

	return trimBlankLines(strings.Join(releaseNotes, "\n")), nil
}

// GitHubURL represents a parsed GitHub URL
type GitHubURL struct {
	Token      string
	Repository string
	SHA        string
	FilePath   string
}

// parseGitHubURL parses a GitHub URL in the format:
// github://[token:<token>@]<owner>/<repo>/[blob/]<sha>/<file_path>
func parseGitHubURL(urlStr string) (*GitHubURL, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "github" {
		return nil, fmt.Errorf("URL must use github:// scheme, got %s", parsedURL.Scheme)
	}

	// Parse user info (token:token_value) - optional for public repos
	var token string
	userInfo := parsedURL.User
	if userInfo != nil {
		username := userInfo.Username()
		tokenValue, hasToken := userInfo.Password()
		if username != "token" {
			return nil, fmt.Errorf("URL authentication must use format 'token:<token_value>' if provided")
		}
		if hasToken {
			token = tokenValue
		}
	}

	// Parse host and path to get repository (owner/repo)
	owner := parsedURL.Host
	if owner == "" {
		return nil, fmt.Errorf("missing repository owner in URL")
	}

	// Parse path (/repo/[blob/]sha/file_path)
	pathParts := strings.Split(strings.TrimPrefix(parsedURL.Path, "/"), "/")

	var repo, sha, filePath string

	if len(pathParts) >= 3 {
		repo = pathParts[0]

		// Check if there's an optional "/blob/" in the path
		if len(pathParts) >= 4 && pathParts[1] == "blob" {
			// Format: /repo/blob/sha/file_path...
			sha = pathParts[2]
			filePath = strings.Join(pathParts[3:], "/")
		} else {
			// Format: /repo/sha/file_path...
			sha = pathParts[1]
			filePath = strings.Join(pathParts[2:], "/")
		}
	} else {
		return nil, fmt.Errorf("URL path must be in format /<repo>/[blob/]<sha>/<file_path>")
	}

	if repo == "" {
		return nil, fmt.Errorf("missing repository name in URL path")
	}

	repository := owner + "/" + repo

	if sha == "" {
		return nil, fmt.Errorf("missing SHA in URL path")
	}
	if filePath == "" {
		return nil, fmt.Errorf("missing file path in URL")
	}

	return &GitHubURL{
		Token:      token,
		Repository: repository,
		SHA:        sha,
		FilePath:   filePath,
	}, nil
}

// downloadFromGitHub downloads a file from GitHub using the API
func downloadFromGitHub(ghURL *GitHubURL) (io.ReadCloser, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s",
		ghURL.Repository, ghURL.FilePath, ghURL.SHA)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authorization header only if token is provided
	if ghURL.Token != "" {
		req.Header.Set("Authorization", "token "+ghURL.Token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	return resp.Body, nil
}

// readReleaseNotesFromGitHub reads and parses changelog from GitHub using custom regex patterns
func readReleaseNotesFromGitHub(ghURL *GitHubURL, startRegex, endRegex *regexp.Regexp) (string, error) {
	reader, err := downloadFromGitHub(ghURL)
	if err != nil {
		return "", fmt.Errorf("failed to download file from GitHub: %w", err)
	}
	defer reader.Close()

	foundFirstHeader := false
	var releaseNotes []string

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()

		if !foundFirstHeader && startRegex.MatchString(line) {
			foundFirstHeader = true
			continue
		}

		if foundFirstHeader {
			if endRegex.MatchString(line) {
				break
			}

			releaseNotes = append(releaseNotes, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading changelog: %w", err)
	}

	if !foundFirstHeader {
		return "", nil
	}

	return trimBlankLines(strings.Join(releaseNotes, "\n")), nil
}

// writeGitHubOutput writes a variable to the GitHub Actions output file using the proper format
func writeGitHubOutput(outputFile, varName, content string) error {
	// Generate a random delimiter (similar to GitHub Actions' own method)
	randomBytes := make([]byte, 15)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("failed to generate random delimiter: %w", err)
	}
	delimiter := base64.StdEncoding.EncodeToString(randomBytes)

	file, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open GitHub output file %q: %w", outputFile, err)
	}
	defer file.Close()

	// Write in the format: variable<<EOF\ncontent\nEOF
	_, err = fmt.Fprintf(file, "%s<<%s\n%s\n%s\n", varName, delimiter, content, delimiter)
	if err != nil {
		return fmt.Errorf("failed to write to GitHub output file: %w", err)
	}

	return nil
}
