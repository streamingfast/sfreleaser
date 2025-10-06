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

var InitCmd = Command(initCmd,
	"init",
	"Initialize the necessary configuration files",
	Flags(func(flags *pflag.FlagSet) {
		flags.BoolP("overwrite", "f", false, "[Destructive] Overwrite configuration files that already exists")
	}),
)

var InstallCmd = Command(installCmd,
	"install",
	"Initialize the necessary configuration files (deprecated: use 'init' instead)",
	Flags(func(flags *pflag.FlagSet) {
		flags.BoolP("overwrite", "f", false, "[Destructive] Overwrite configuration files that already exists")
	}),
)

func installCmd(cmd *cobra.Command, args []string) error {
	fmt.Println("WARNING: 'sfreleaser install' is deprecated, please use 'sfreleaser init' instead")
	fmt.Println()
	return initCmd(cmd, args)
}

func initCmd(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)
	overwrite := sflags.MustGetBool(cmd, "overwrite")

	zlog.Debug("starting 'sfreleaser init'",
		zap.Inline(global),
		zap.Bool("overwrite", overwrite),
	)

	if global.Language == LanguageUnset {
		global.Language = promptLanguage()
	}

	if global.Variant == VariantUnset {
		global.Variant = promptVariant()
	}

	var noBinaries bool
	if global.Language == LanguageRust && global.Variant == VariantApplication {
		yes, _ := cli.PromptConfirm(dedent(`
			Application variant for language Rust works but without support for automatic
			binaries building and inclusion in the release.

			Do you want to continue?
		`))
		if !yes {
			return fmt.Errorf("operation cancelled by user")
		}
		noBinaries = true
	}

	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	model := getInstallTemplateModel(global, noBinaries)

	var sfreleaserYamlTmpl []byte
	switch global.Language {
	case LanguageGolang:
		sfreleaserYamlTmpl = sfreleaserGolangYamlTmpl

	case LanguageRust:
		if global.Variant == VariantSubstreams {
			sfreleaserYamlTmpl = sfreleaserSubstreamsYamlTmpl
		} else {
			// For Rust library variant, add crate model
			// For Rust application variant with noBinaries, we just use the rust template without crates
			if global.Variant == VariantLibrary {
				model = addRustModel(model)
			}
			sfreleaserYamlTmpl = sfreleaserRustYamlTmpl
		}

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
	fmt.Println("Initialization completed")
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
