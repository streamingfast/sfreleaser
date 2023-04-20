package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/streamingfast/cli"
)

func trimBlankLines(in string) string {
	return strings.TrimFunc(in, unicode.IsSpace)
}

func getLines(output string) []string {
	return mapEachLine(output, func(line string) string { return line })
}

func indentAllLines(input string, indent string) string {
	return strings.Join(mapEachLine(input, func(line string) string { return indent + line }), "\n")
}

func mapEachLine[T any](input string, fn func(line string) T) []T {
	return mapEachReaderLine(strings.NewReader(input), fn)
}

func mapEachReaderLine[T any](reader io.Reader, fn func(line string) T) (out []T) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		out = append(out, fn(scanner.Text()))
	}

	cli.NoError(scanner.Err(), "Unable to scan lines from string in memory")
	return
}

// dedent dedents the multi-line string received and **then** format
// the string according to parameters. This ordering means that if your
// parameter is expected to be multi-line, you need to indent it yourself
// by calling for example [indentAllLines].
func dedent(format string, args ...any) string {
	return fmt.Sprintf(cli.Dedent(format), args...)
}

var onExits []func()

func osExit(code int) {
	for _, onExit := range onExits {
		onExit()
	}

	os.Exit(code)
}
