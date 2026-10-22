package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

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
	if defaultVersion == latestTag && latestTag != "" {
		cli.Quit(cli.Dedent(`
			Latest tag %q is the same as latest version extracted from your changelog, you can't
			release the same version twice.
		`), latestTag)
	}

	if defaultVersion == "" && latestTag != "" {
		latestVersion, err := versioning.NewVersion(latestTag)
		if err == nil {
			latestSegments := latestVersion.Segments()

			// Version is always valid
			nextVersion, _ := versioning.NewVersion(fmt.Sprintf("%d.%d.%d", latestSegments[0], latestSegments[1], latestSegments[2]+1))
			defaultVersion = "v" + nextVersion.String()
		}
	}

	opts := []cli.PromptOption{
		validateVersionPrompt(),
	}

	if defaultVersion != "" {
		opts = append(opts, cli.WithPromptDefaultValue(defaultVersion))
	}

	currentLatestTag := latestTag
	if currentLatestTag == "" {
		currentLatestTag = "'Never released yet'"
	}

	return cli.Prompt(
		fmt.Sprintf("What version do you want to release (current latest tag is %s)", currentLatestTag),
		cli.PromptTypeString,
		opts...,
	)
}

// confirm asks the user a yes/no question, unless --yes is set in which case the answer is
// "yes", or the release is non-interactive in which case the answer is "no", both without
// showing a prompt.
func (m *ReleaseModel) confirm(label string) bool {
	if m.Yes {
		fmt.Printf("%s Yes (--yes)\n", label)
		return true
	}

	if m.NonInteractive {
		fmt.Printf("%s No (non-interactive)\n", label)
		return false
	}

	promptConfirm := m.promptConfirm
	if promptConfirm == nil {
		promptConfirm = cli.PromptConfirm
	}

	yes, _ := promptConfirm(label)
	return yes
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

// isCI reports whether the CI environment variable, set by most CI providers, is true.
func isCI() bool {
	ci, err := strconv.ParseBool(os.Getenv("CI"))
	return err == nil && ci
}
