package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var headerRegex = regexp.MustCompile(`^##([^#]+)`)

// readVersionFromChangelog will try to infer the default version from the changelog file.
// It skips "Unreleased" and "Next" sections and returns the first actual version found.
func readVersionFromChangelog(changelogFile string) string {
	if !cli.FileExists(changelogFile) {
		return ""
	}

	file, err := os.Open(changelogFile)
	cli.NoError(err, "Unable to open changelog %q", changelogFile)
	defer file.Close()

	version, _, err := changelogVersions(file)
	cli.NoError(err, "Unable to scan lines from changelog")

	return version
}

// changelogVersions returns the first versioned section of the changelog and the version of its
// first section, empty when that first section is not versioned, such as "Unreleased".
func changelogVersions(changelog io.Reader) (version string, firstSection string, err error) {
	foundFirstSection := false

	scanner := bufio.NewScanner(changelog)
	for scanner.Scan() {
		line := scanner.Text()
		if !headerRegex.MatchString(line) {
			continue
		}

		lineVersion := extractVersionFromHeader(line)
		if !foundFirstSection {
			foundFirstSection = true
			firstSection = lineVersion
		}

		if lineVersion != "" {
			return lineVersion, firstSection, nil
		}
	}

	return "", firstSection, scanner.Err()
}

var versionRegex = regexp.MustCompile(`v?([0-9]+\.[0-9]+\.[0-9]+[a-zA-Z0-9\-_\.]*)`)

func extractVersionFromHeader(header string) string {
	matches := headerRegex.FindAllStringSubmatch(header, 1)
	if len(matches) != 1 {
		return ""
	}

	groups := matches[0]
	if len(groups) != 2 {
		return ""
	}

	version := strings.TrimSpace(groups[1])
	if version == "" {
		return ""
	}

	normalizedVersion := strings.ToLower(version)
	if normalizedVersion != "unreleased" && normalizedVersion != "next" {
		groups := versionRegex.FindStringSubmatch(normalizedVersion)
		if len(groups) != 2 {
			// Nothing found, should had 2 group
			return ""
		}

		return "v" + groups[1]
	}

	return ""
}

func readReleaseNotes(changelogFile string) string {
	if !cli.FileExists(changelogFile) {
		return ""
	}

	file, err := os.Open(changelogFile)
	cli.NoError(err, "Unable to open changelog %q", changelogFile)
	defer file.Close()

	foundFirstHeader := false
	var releaseNotes []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !foundFirstHeader && headerRegex.MatchString(line) {
			foundFirstHeader = true
			continue
		}

		if foundFirstHeader {
			if headerRegex.MatchString(line) {
				break
			}

			releaseNotes = append(releaseNotes, line)
		}
	}

	zlog.Debug("computed changelog lines", zap.Strings("release_lines", releaseNotes))

	cli.NoError(scanner.Err(), "Unable to scan lines from changelog")
	return trimBlankLines(strings.Join(releaseNotes, "\n"))
}

func validateChangelogForVersion(changelogFile string, version string) (warning string, err error) {
	if !cli.FileExists(changelogFile) {
		return fmt.Sprintf("No changelog file found at %q, release notes will be empty", changelogFile), nil
	}

	changelogVersion := readVersionFromChangelog(changelogFile)
	if changelogVersion == "" {
		return "", fmt.Errorf("no versioned section found in changelog %q, did you forget to add a section like '## %s' before releasing?", changelogFile, version)
	}

	if changelogVersion != version {
		return "", fmt.Errorf("changelog version mismatch: releasing %q but changelog has %q, did you forget to update the changelog?", version, changelogVersion)
	}

	return "", nil
}
