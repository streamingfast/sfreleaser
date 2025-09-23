package main

import (
	"bufio"
	"fmt"
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
	}),
	Description(`
		Extracts a specific section from a changelog file.

		Arguments:
		  file     Path to the changelog file (defaults to "CHANGELOG.md")
		  version  Version to extract (defaults to first section found)

		Examples:
		  sfreleaser changelog extract-section
		  sfreleaser changelog extract-section CHANGELOG.md
		  sfreleaser changelog extract-section CHANGELOG.md v1.2.3
		  sfreleaser changelog extract-section --start-header-regex="## v1\\.2\\..+" CHANGELOG.md
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

	fmt.Print(section)
	return nil
}

// extractChangelogSection extracts a section from a changelog using custom regex patterns
func extractChangelogSection(changelogFile, startHeaderRegex, endHeaderRegex string) (string, error) {
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
