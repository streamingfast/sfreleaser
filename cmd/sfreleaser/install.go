package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

//go:embed templates/application/goreleaser.yaml.gotmpl
var goreleaserAppTmpl []byte

//go:embed templates/library/goreleaser.yaml.gotmpl
var goreleaserLibTmpl []byte

//go:embed templates/CHANGELOG.md.gotmpl
var changelogTmpl []byte

//go:embed templates/sfreleaser.yaml.gotmpl
var sfreleaserYamlTmpl []byte

var InstallCmd = Command(install,
	"install",
	"Install the necessary files configuration files like .goreleaser",
	Flags(func(flags *pflag.FlagSet) {
		flags.BoolP("overwrite", "f", false, "[Destructive] Overwrite configuration files that already exists")
	}),
)

func install(cmd *cobra.Command, _ []string) error {
	language := mustGetLanguage(cmd)
	variant := mustGetVariant(cmd)
	root := sflags.MustGetString(cmd, "root")
	overwrite := sflags.MustGetBool(cmd, "overwrite")
	project := sflags.MustGetString(cmd, "project")

	if project == "" {
		target := root
		if target == "" {
			target = cli.WorkingDirectory()
		}

		project = filepath.Base(target)
	}

	zlog.Debug("starting 'sfreleaser install'",
		zap.Stringer("language", language),
		zap.Stringer("variant", variant),
		zap.String("root", root),
		zap.Bool("overwrite", overwrite),
		zap.String("project", project),
	)

	if language == LanguageUnset {
		language = promptLanguage()
	}

	if variant == VariantUnset {
		variant = promptVariant()
	}

	if root != "" {
		cli.NoError(os.Chdir(root), "Unable to change directory to %q", root)
	}

	model := map[string]any{
		"binary":   project,
		"project":  project,
		"language": language.Lower(),
		"variant":  variant.Lower(),
	}

	goreleaserTemplate := goreleaserAppTmpl
	if variant == VariantLibrary {
		goreleaserTemplate = goreleaserLibTmpl
	}

	renderTemplate(".goreleaser.yaml", overwrite, goreleaserTemplate, model)
	renderTemplate(".sfreleaser", overwrite, sfreleaserYamlTmpl, model)

	if !cli.FileExists("CHANGELOG.md") {
		if yes, _ := cli.PromptConfirm("Do you want to generate an empty CHANGELOG.md file?"); yes {
			renderTemplate("CHANGELOG.md", false, changelogTmpl, model)
		}
	}

	fmt.Println()
	fmt.Println("Install completed")
	return nil
}

func renderTemplate(file string, overwrite bool, tmplContent []byte, model map[string]any) {
	fileExists := cli.FileExists(file)

	if fileExists && !overwrite {
		fmt.Printf("Ignoring %q, it already exists\n", file)
	} else if !fileExists || overwrite {
		tmpl, err := template.New(file).Parse(string(tmplContent))
		cli.NoError(err, "Unable to instantiate template")

		buffer := bytes.NewBuffer(nil)
		tmpl.Execute(buffer, model)

		directory := filepath.Dir(file)
		if !cli.DirectoryExists(directory) {
			cli.NoError(os.MkdirAll(directory, os.ModePerm), "Making directories for template file %q", file)
		}

		cli.WriteFile(file, buffer.String())
		fmt.Printf("Wrote %s\n", file)
	}
}
