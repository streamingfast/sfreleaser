package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

//go:generate go-enum -f=$GOFILE --marshal --names --nocase

// ENUM(
//
//	Unset
//	Golang
//
// )
type Language uint

func (l Language) Lower() string {
	return strings.ToLower(l.String())
}

func LanguageResolveAlias(in string) string {
	lowered := strings.ToLower(in)
	switch lowered {
	case "go":
		return "golang"
	}

	return in
}

func mustGetLanguage(cmd *cobra.Command) Language {
	raw := sflags.MustGetString(cmd, "language")
	zlog.Debug("raw read language", zap.String("raw", raw))

	if raw == "" {
		return LanguageUnset
	}

	language, err := ParseLanguage(LanguageResolveAlias(raw))
	cli.NoError(err, "Invalid")

	return language
}

func isSupportedLanguage(x string) bool {
	return x != LanguageUnset.String()
}
