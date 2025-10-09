package main

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"text/template"

	"github.com/streamingfast/cli"
)

//go:embed templates/application/goreleaser.yaml.gotmpl
var goreleaserAppTmpl []byte

//go:embed templates/library/goreleaser.yaml.gotmpl
var goreleaserLibTmpl []byte

//go:embed templates/substreams/goreleaser.yaml.gotmpl
var goreleaserSubstreamsTmpl []byte

//go:embed templates/CHANGELOG.md.gotmpl
var changelogTmpl []byte

//go:embed templates/sfreleaser-golang.yaml.gotmpl
var sfreleaserGolangYamlTmpl []byte

//go:embed templates/sfreleaser-rust.yaml.gotmpl
var sfreleaserRustYamlTmpl []byte

//go:embed templates/sfreleaser-substreams.yaml.gotmpl
var sfreleaserSubstreamsYamlTmpl []byte

func getInstallTemplateModel(global *GlobalModel, noBinaries bool) map[string]any {
	return map[string]any{
		"global":     global,
		"noBinaries": noBinaries,
	}
}

func getReleaseTemplateModel(global *GlobalModel, release *ReleaseModel) map[string]any {
	return map[string]any{
		"global":  global,
		"release": release,
	}
}

func renderGoreleaserFile(global *GlobalModel, release *ReleaseModel, github *GitHubReleaseModel) {
	var goreleaserTemplate []byte
	switch global.Variant {
	case VariantLibrary:
		goreleaserTemplate = goreleaserLibTmpl
	case VariantSubstreams:
		goreleaserTemplate = goreleaserSubstreamsTmpl
	default:
		goreleaserTemplate = goreleaserAppTmpl
	}

	renderTemplate(github.GoreleaserConfigPath, true, goreleaserTemplate, getReleaseTemplateModel(global, release))
}

func renderTemplate(file string, overwrite bool, tmplContent []byte, model map[string]any) (wrote string) {
	if !cli.FileExists(file) || overwrite {
		tmpl, err := template.New(file).Funcs(templateFuncs).Parse(string(tmplContent))
		cli.NoError(err, "Unable to instantiate template")

		buffer := bytes.NewBuffer(nil)
		tmpl.Execute(buffer, model)

		directory := filepath.Dir(file)
		if !cli.DirectoryExists(directory) {
			cli.NoError(os.MkdirAll(directory, os.ModePerm), "Making directories for template file %q", file)
		}

		cli.WriteFile(file, "%s", buffer.String())

		return file
	}

	return ""
}
