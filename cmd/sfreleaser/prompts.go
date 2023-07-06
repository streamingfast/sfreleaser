package main

import (
	"fmt"
	"regexp"

	"github.com/bobg/go-generics/v2/slices"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

func promptLanguage() Language {
	return cli.PromptSelect("Project language", slices.Filter(LanguageNames(), isSupportedLanguage), ParseLanguage)
}

func promptVariant() Variant {
	return cli.PromptSelect("Project variant", slices.Filter(VariantNames(), isSupportedVariant), ParseVariant)
}

func promptVersion(defaultVersion string) string {
	zlog.Debug("asking for version via terminal", zap.String("default", defaultVersion))

	opts := []cli.PromptOption{
		validateVersionPrompt(),
	}

	if defaultVersion != "" {
		opts = append(opts, cli.WithPromptDefaultValue(defaultVersion))
	}

	return cli.Prompt(
		fmt.Sprintf("What version do you want to release (current latest tag is %s)", latestTag()),
		cli.PromptTypeString,
		opts...,
	)
}

var cliVersionRegexp = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+`)

func validVersion(in string) error {
	if !cliVersionRegexp.MatchString(in) {
		return fmt.Errorf(`version %q must of the form "^v{major}.{minor}.{patch}" (end of input is free-form)`, in)
	}

	return nil
}

func validateVersionPrompt() cli.PromptOption {
	return cli.WithPromptValidate("invalid version", validVersion)
}
