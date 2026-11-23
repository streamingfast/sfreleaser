package main

import (
	"os"
	"strings"
	"unicode/utf8"
)

// maxInferredDescriptionLength is the longest README paragraph taken as the package
// description, a longer one is more than a summary.
const maxInferredDescriptionLength = 200

// licenseMarkers identifies a license text by phrases all found in it (whitespace and case
// insensitive), the most specific first.
var licenseMarkers = []struct {
	spdx    string
	markers []string
}{
	{"AGPL-3.0-only", []string{"gnu affero general public license", "version 3"}},
	{"LGPL-3.0-only", []string{"gnu lesser general public license", "version 3"}},
	{"GPL-3.0-only", []string{"gnu general public license", "version 3"}},
	{"GPL-2.0-only", []string{"gnu general public license", "version 2"}},
	{"Apache-2.0", []string{"apache license", "version 2.0"}},
	{"MPL-2.0", []string{"mozilla public license", "2.0"}},
	{"Unlicense", []string{"this is free and unencumbered software released into the public domain"}},
	{"MIT", []string{"permission is hereby granted, free of charge"}},
	{"ISC", []string{"permission to use, copy, modify, and/or distribute this software for any purpose"}},
	{"BSD-3-Clause", []string{"redistribution and use in source and binary forms", "neither the name of"}},
	{"BSD-2-Clause", []string{"redistribution and use in source and binary forms"}},
}

// detectLicense returns the SPDX identifier of the license text at path, empty when it is not
// one of the common ones.
func detectLicense(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	text := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
	for _, license := range licenseMarkers {
		matches := true
		for _, marker := range license.markers {
			if !strings.Contains(text, marker) {
				matches = false
				break
			}
		}

		if matches {
			return license.spdx
		}
	}

	return ""
}

// readmeDescription returns the first paragraph of plain text of the README, the lines being
// joined, skipping headings, badges, HTML, lists, tables, quotes and code. Empty when there is
// none or it is longer than [maxInferredDescriptionLength].
func readmeDescription(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var paragraph []string
	inFence := false
	flush := func() string {
		text := strings.Join(paragraph, " ")
		paragraph = nil
		return text
	}

	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}

		switch {
		case inFence:
			continue

		case line == "":
			if text := flush(); text != "" {
				return acceptDescription(text)
			}

		case isMarkdownMarkup(line):
			// A paragraph interrupted by markup is not a clean summary, keep looking
			paragraph = nil

		default:
			paragraph = append(paragraph, line)
		}
	}

	return acceptDescription(flush())
}

func isMarkdownMarkup(line string) bool {
	for _, prefix := range []string{"#", "[![", "![", "<", "|", ">", "- ", "* ", "+ ", "---", "===", "[!["} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	// Numbered list, '1. ' or '1) '
	if len(line) > 2 && line[0] >= '0' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
		return true
	}

	return false
}

func acceptDescription(text string) string {
	if utf8.RuneCountInString(text) > maxInferredDescriptionLength {
		return ""
	}

	return text
}
