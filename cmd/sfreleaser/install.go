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

//go:embed templates/sfreleaser-golang.yaml.gotmpl
var sfreleaserGolangYamlTmpl []byte

//go:embed templates/sfreleaser-rust.yaml.gotmpl
var sfreleaserRustYamlTmpl []byte

var InstallCmd = Command(install,
	"install",
	"Install the necessary files configuration files like .goreleaser",
	Flags(func(flags *pflag.FlagSet) {
		flags.BoolP("overwrite", "f", false, "[Destructive] Overwrite configuration files that already exists")
	}),
)

func install(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)
	overwrite := sflags.MustGetBool(cmd, "overwrite")

	zlog.Debug("starting 'sfreleaser install'",
		zap.Inline(global),
		zap.Bool("overwrite", overwrite),
	)

	if global.Language == LanguageUnset {
		global.Language = promptLanguage()
	}

	if global.Variant == VariantUnset {
		global.Variant = promptVariant()
	}

	if global.Language == LanguageRust && global.Variant == VariantApplication {
		cli.Quit("Application variant for language Rust is currently not supported")
	}

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	model := getInstallTemplateModel(global)

	goreleaserTemplate := goreleaserAppTmpl
	if global.Variant == VariantLibrary {
		goreleaserTemplate = goreleaserLibTmpl
	}

	renderTemplateAndReport(".goreleaser.yaml", overwrite, goreleaserTemplate, model)

	var sfreleaserYamlTmpl []byte
	switch global.Language {
	case LanguageGolang:
		sfreleaserYamlTmpl = sfreleaserGolangYamlTmpl

	case LanguageRust:
		model = addRustModel(model)
		sfreleaserYamlTmpl = sfreleaserRustYamlTmpl

	default:
		cli.Quit("unhandled language %q", global.Language)
	}

	renderTemplateAndReport(".sfreleaser", overwrite, sfreleaserYamlTmpl, model)

	if !cli.FileExists("CHANGELOG.md") {
		if yes, _ := cli.PromptConfirm("Do you want to generate an empty CHANGELOG.md file?"); yes {
			renderTemplateAndReport("CHANGELOG.md", false, changelogTmpl, model)
		}
	}

	fmt.Println()
	fmt.Println("Install completed")
	return nil
}

func getInstallTemplateModel(global *GlobalModel) map[string]any {
	return map[string]any{
		"binary":   global.Project,
		"project":  global.Project,
		"language": global.Language.Lower(),
		"variant":  global.Variant.Lower(),
	}
}

func renderTemplateAndReport(file string, overwrite bool, tmplContent []byte, model map[string]any) {
	wrote := renderTemplate(file, overwrite, tmplContent, model)
	if wrote == "" {
		fmt.Printf("Ignoring %q, it already exists\n", file)
	} else {
		fmt.Printf("Wrote %s\n", wrote)
	}
}

func renderTemplate(file string, overwrite bool, tmplContent []byte, model map[string]any) (wrote string) {
	if !cli.FileExists(file) || overwrite {
		tmpl, err := template.New(file).Parse(string(tmplContent))
		cli.NoError(err, "Unable to instantiate template")

		buffer := bytes.NewBuffer(nil)
		tmpl.Execute(buffer, model)

		directory := filepath.Dir(file)
		if !cli.DirectoryExists(directory) {
			cli.NoError(os.MkdirAll(directory, os.ModePerm), "Making directories for template file %q", file)
		}

		cli.WriteFile(file, buffer.String())

		return file
	}

	return ""
}

type RustInstallModel struct {
	Crates []string
}

func addRustModel(model map[string]any) map[string]any {
	model["rust"] = &RustInstallModel{
		Crates: findAllRustCrates(),
	}

	return model
}
