package main

import (
	"fmt"
	"regexp"

	"github.com/bobg/go-generics/v2/slices"
	versioning "github.com/hashicorp/go-version"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

func promptLanguage() Language {
	return cli.PromptSelect("Project language", slices.Filter(LanguageNames(), isSupportedLanguage), ParseLanguage)
}

func promptVariant() Variant {
	return cli.PromptSelect("Project variant", slices.Filter(VariantNames(), isSupportedVariant), ParseVariant)
}

func promptVersion(changelogPath string, gitRemote string) string {
	latestTag := latestTag(gitRemote)
	defaultVersion := readVersionFromChangelog(changelogPath)

	zlog.Debug("asking for version via terminal", zap.String("default", defaultVersion), zap.String("changelog_path", changelogPath))
	if defaultVersion == latestTag {
		cli.Quit(cli.Dedent(`
			Latest tag %q is the same as latest version extracted from your changelog, you can't
			release the same version twice.
		`), latestTag)
	}

	if defaultVersion == "" && latestTag != "" {
		latestVersion, err := versioning.NewVersion(latestTag)
		cli.NoError(err, "unable to parse latest tag %q", latestTag)

		latestSegments := latestVersion.Segments()

		// Version is always valid
		nextVersion, _ := versioning.NewVersion(fmt.Sprintf("%d.%d.%d", latestSegments[0], latestSegments[1], latestSegments[2]+1))
		defaultVersion = "v" + nextVersion.String()
	}

	opts := []cli.PromptOption{
		validateVersionPrompt(),
	}

	if defaultVersion != "" {
		opts = append(opts, cli.WithPromptDefaultValue(defaultVersion))
	}

	return cli.Prompt(
		fmt.Sprintf("What version do you want to release (current latest tag is %s)", latestTag),
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
