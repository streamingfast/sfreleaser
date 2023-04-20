package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var headerRegex = regexp.MustCompile(`^##([^#])`)

func readReleaseNotes() string {
	changelogFile := "./CHANGELOG.md"
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
