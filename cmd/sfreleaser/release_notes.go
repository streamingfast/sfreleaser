package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var headerRegex = regexp.MustCompile(`^##([^#]+)`)

func readReleaseNotesVersion(changelogFile string) string {
	if !cli.FileExists(changelogFile) {
		return ""
	}

	file, err := os.Open(changelogFile)
	cli.NoError(err, "Unable to open changelog %q", changelogFile)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if headerRegex.MatchString(line) {
			return extractVersionFromHeader(line)
		}
	}

	cli.NoError(scanner.Err(), "Unable to scan lines from changelog")

	// We found nothing!
	return ""
}

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
		return version
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
