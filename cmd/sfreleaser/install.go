package main

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
	"go.uber.org/zap"
)

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

	renderInstallTemplate(".sfreleaser", overwrite, sfreleaserYamlTmpl, model)

	if !cli.FileExists("CHANGELOG.md") {
		if yes, _ := cli.PromptConfirm("Do you want to generate an empty CHANGELOG.md file?"); yes {
			renderInstallTemplate("CHANGELOG.md", false, changelogTmpl, model)
		}
	}

	fmt.Println()
	fmt.Println("Install completed")
	return nil
}

func renderInstallTemplate(file string, overwrite bool, tmplContent []byte, model map[string]any) {
	wrote := renderTemplate(file, overwrite, tmplContent, model)
	if wrote == "" {
		fmt.Printf("Ignoring %q, it already exists\n", file)
	} else {
		fmt.Printf("Wrote %s\n", wrote)
	}
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

var templateFuncs = template.FuncMap{
	"lower": transformStringFunc(strings.ToLower),
	"upper": transformStringFunc(strings.ToUpper),
}

func transformStringFunc(transformer func(in string) string) func(in any) string {
	return func(in any) string {
		switch v := in.(type) {
		case string:
			return transformer(v)

		case fmt.Stringer:
			return transformer(v.String())

		default:
			return transformer(fmt.Sprintf("%s", v))
		}
	}
}
