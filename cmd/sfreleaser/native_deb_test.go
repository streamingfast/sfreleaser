package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blakesmith/ar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_debFileName(t *testing.T) {
	assert.Equal(t, "upgrader_1.2.3_amd64.deb", debFileName("upgrader", "v1.2.3", nativeTargetByID("linux-amd64")))
	assert.Equal(t, "upgrader_1.2.3-rc.1_arm64.deb", debFileName("upgrader", "1.2.3-rc.1", nativeTargetByID("linux-arm64")))
}

func Test_debEnv(t *testing.T) {
	assert.Equal(t,
		map[string]string{"PKG_VERSION": "1.2.3", "PKG_ARCH": "arm64", "PKG_BINARY": "/bin/app"},
		debEnv("v1.2.3", nativeTargetByID("linux-arm64"), "/bin/app"),
	)
}

// debControl returns the 'control' file of the Debian package at path.
func debControl(t *testing.T, path string) string {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	archive := ar.NewReader(file)
	for {
		header, err := archive.Next()
		require.NoError(t, err, "no control archive in %s", path)
		if header.Name != "control.tar.gz" {
			continue
		}

		compressed, err := gzip.NewReader(archive)
		require.NoError(t, err)

		control := tar.NewReader(compressed)
		for {
			entry, err := control.Next()
			require.NoError(t, err, "no control file in %s", path)
			if strings.TrimPrefix(entry.Name, "./") == "control" {
				content, err := io.ReadAll(control)
				require.NoError(t, err)
				return string(content)
			}
		}
	}
}

// writeFakeBinaries writes a binary per target id under a temporary directory.
func writeFakeBinaries(t *testing.T, ids ...string) map[string]string {
	t.Helper()

	work := t.TempDir()
	binaries := map[string]string{}
	for _, id := range ids {
		binaries[id] = filepath.Join(work, id, "app")
		require.NoError(t, os.MkdirAll(filepath.Dir(binaries[id]), 0o755))
		require.NoError(t, os.WriteFile(binaries[id], []byte("#!/bin/sh\n"), 0o755))
	}

	return binaries
}

func Test_buildDebPackages(t *testing.T) {
	config := filepath.Join(t.TempDir(), "nfpm.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`name: app
arch: ${PKG_ARCH}
platform: linux
version: ${PKG_VERSION}
maintainer: Jane <jane@example.com>
description: ${APP_DESCRIPTION}
contents:
  - src: ${PKG_BINARY}
    dst: /usr/bin/app
    expand: true
`), 0o644))
	t.Setenv("APP_DESCRIPTION", "From the environment")

	binaries := writeFakeBinaries(t, "darwin-arm64", "linux-amd64", "linux-arm64")
	packages, err := buildDebPackages(t.Context(), debOptions{
		Name:       "app",
		Version:    "v1.2.3",
		ConfigPath: config,
		Binaries:   binaries,
		OutputDir:  filepath.Join(t.TempDir(), "dist"),
	})
	require.NoError(t, err)

	// Only the Linux targets are packaged.
	require.Len(t, packages, 2)
	assert.Equal(t, "app_1.2.3_amd64.deb", packages[0].Name)
	assert.Equal(t, "app_1.2.3_arm64.deb", packages[1].Name)

	control := debControl(t, packages[1].Path)
	assert.Contains(t, control, "Version: 1.2.3\n")
	assert.Contains(t, control, "Architecture: arm64\n")
	assert.Contains(t, control, "Description: From the environment\n")

	sum, err := fileSHA256(packages[1].Path)
	require.NoError(t, err)
	assert.Equal(t, sum, packages[1].SHA256)
}

func Test_buildDebPackages_nfpmFails(t *testing.T) {
	output := t.TempDir()
	config := filepath.Join(t.TempDir(), "nfpm.yaml")
	require.NoError(t, os.WriteFile(config, []byte("name: app\nversion: 1.0.0\ncontents:\n  - src: /does/not/exist\n    dst: /usr/bin/app\n"), 0o644))

	_, err := buildDebPackages(t.Context(), debOptions{
		Name:       "app",
		Version:    "v1.0.0",
		ConfigPath: config,
		Binaries:   writeFakeBinaries(t, "linux-amd64"),
		OutputDir:  output,
	})
	require.ErrorContains(t, err, "packaging app_1.0.0_amd64.deb with nfpm")
	assert.NoFileExists(t, filepath.Join(output, "app_1.0.0_amd64.deb"))
}

func TestReleaseModel_validateForRelease_deb(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "nfpm.yaml"), nil, 0o644))
	global := &GlobalModel{Language: LanguageGolang, Variant: VariantApplication, ConfigRoot: root}

	valid := &ReleaseModel{Deb: true, DebConfig: "nfpm.yaml", Builder: BuilderNative}
	assert.Empty(t, valid.validateForRelease(global))

	noBinaries := &ReleaseModel{Deb: true, DebConfig: "nfpm.yaml", Builder: BuilderNative, NoBinaries: true}
	assert.Contains(t, noBinaries.validateForRelease(global)[0], `requires a Go application built with the native builder`)

	docker := &ReleaseModel{Deb: true, DebConfig: "nfpm.yaml", Builder: BuilderGoreleaserDocker}
	assert.Contains(t, docker.validateForRelease(global)[0], `requires a Go application built with the native builder`)

	generated := &ReleaseModel{Deb: true, Builder: BuilderNative}
	assert.Empty(t, generated.validateForRelease(global))

	missing := &ReleaseModel{Deb: true, DebConfig: "other.yaml", Builder: BuilderNative}
	assert.Contains(t, missing.validateForRelease(global)[0], `"deb-config" file "other.yaml" cannot be read`)
}

func Test_renderNfpmConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, mode os.FileMode) {
		file := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
		require.NoError(t, os.WriteFile(file, []byte("x"), mode))
		require.NoError(t, os.Chmod(file, mode))
	}
	write("rootfs/lib/systemd/system/app@.service", 0o644)
	write("rootfs/usr/share/app/example.env", 0o644)
	write("rootfs/usr/libexec/app-helper", 0o755)
	write("postinstall.sh", 0o755)

	rendered, err := renderNfpmConfig(debConfigOptions{
		Name:        "app",
		Version:     "1.2.3",
		Arch:        "arm64",
		Maintainer:  "Jane Doe <jane@example.com>",
		Description: "An \"app\"",
		Homepage:    "https://example.com/app",
		Depends:     []string{"libc6"},
		Binary:      "/build/app",
		Dir:         dir,
	})
	require.NoError(t, err)

	assert.Equal(t, `# Generated by sfreleaser, do not edit.
name: "app"
arch: "arm64"
platform: linux
version: "1.2.3"
section: utils
priority: optional
maintainer: "Jane Doe <jane@example.com>"
description: "An \"app\""
homepage: "https://example.com/app"
depends:
  - "libc6"
contents:
  - src: "/build/app"
    dst: "/usr/bin/app"
    file_info:
      mode: 0755
  - src: "`+dir+`/rootfs/lib/systemd/system/app@.service"
    dst: "/lib/systemd/system/app@.service"
    file_info:
      mode: 0644
  - src: "`+dir+`/rootfs/usr/libexec/app-helper"
    dst: "/usr/libexec/app-helper"
    file_info:
      mode: 0755
  - src: "`+dir+`/rootfs/usr/share/app/example.env"
    dst: "/usr/share/app/example.env"
    file_info:
      mode: 0644
scripts:
  postinstall: "`+dir+`/postinstall.sh"
`, rendered)
}

func Test_renderNfpmConfig_noPackagingDir(t *testing.T) {
	rendered, err := renderNfpmConfig(debConfigOptions{Name: "app", Version: "1", Arch: "amd64", Maintainer: "m", Binary: "/b", Dir: filepath.Join(t.TempDir(), "missing")})
	require.NoError(t, err)
	assert.Contains(t, rendered, `dst: "/usr/bin/app"`)
	assert.NotContains(t, rendered, "scripts:")
}

func Test_renderNfpmConfig_binaryConflict(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rootfs/usr/bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rootfs/usr/bin/app"), nil, 0o755))

	_, err := renderNfpmConfig(debConfigOptions{Name: "app", Dir: dir})
	require.ErrorContains(t, err, "would overwrite the binary")
}

func Test_buildDebPackages_generatedConfig(t *testing.T) {
	packages, err := buildDebPackages(t.Context(), debOptions{
		Name:      "app",
		Version:   "v1.2.3",
		Config:    debConfigOptions{Name: "app", Maintainer: "Jane <jane@example.com>", Dir: filepath.Join(t.TempDir(), "none")},
		Binaries:  writeFakeBinaries(t, "linux-amd64"),
		OutputDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.Len(t, packages, 1)

	control := debControl(t, packages[0].Path)
	assert.Contains(t, control, "Version: 1.2.3\n")
	assert.Contains(t, control, "Architecture: amd64\n")
	assert.Contains(t, control, "Maintainer: Jane <jane@example.com>\n")
}

func Test_parseRepositoryMetadata(t *testing.T) {
	metadata, err := parseRepositoryMetadata(`{"description":"Watches upgrades","licenseInfo":{"spdxId":"Apache-2.0"}}`)
	require.NoError(t, err)
	assert.Equal(t, "Watches upgrades", metadata.Description)
	assert.Equal(t, "Apache-2.0", metadata.LicenseInfo.SPDXID)

	metadata, err = parseRepositoryMetadata(`{"description":"","licenseInfo":{"spdxId":"NOASSERTION"}}`)
	require.NoError(t, err)
	assert.Empty(t, metadata.LicenseInfo.SPDXID)

	// 'licenseInfo' is null for a repository without license.
	metadata, err = parseRepositoryMetadata(`{"description":"d","licenseInfo":null}`)
	require.NoError(t, err)
	assert.Empty(t, metadata.LicenseInfo.SPDXID)

	_, err = parseRepositoryMetadata("not json")
	require.Error(t, err)
}

func Test_detectLicense(t *testing.T) {
	tests := map[string]struct {
		text string
		want string
	}{
		"apache": {"                                 Apache License\n                           Version 2.0, January 2004", "Apache-2.0"},
		"mit":    {"MIT License\n\nPermission is hereby granted,\nfree of charge, to any person", "MIT"},
		"bsd3":   {"Redistribution and use in source and binary forms, with or without\nNeither the name of the copyright holder", "BSD-3-Clause"},
		"bsd2":   {"Redistribution and use in source and binary forms, with or without", "BSD-2-Clause"},
		"gpl3":   {"GNU GENERAL PUBLIC LICENSE\nVersion 3, 29 June 2007", "GPL-3.0-only"},
		"agpl3":  {"GNU AFFERO GENERAL PUBLIC LICENSE\nVersion 3, 19 November 2007", "AGPL-3.0-only"},
		"custom": {"All rights reserved.", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "LICENSE")
			require.NoError(t, os.WriteFile(path, []byte(tt.text), 0o644))
			assert.Equal(t, tt.want, detectLicense(path))
		})
	}

	assert.Empty(t, detectLicense(filepath.Join(t.TempDir(), "missing")))
}

func Test_readmeDescription(t *testing.T) {
	tests := map[string]struct {
		readme string
		want   string
	}{
		"after title and badges": {"# app\n\n[![CI](x)](y)\n\nWatches for upgrade signals\nand swaps files.\n\n## Usage\n", "Watches for upgrade signals and swaps files."},
		"skips html and lists":   {"# app\n\n<p align=\"center\">logo</p>\n\n- a list\n\nThe summary.\n", "The summary."},
		"skips code fence":       {"# app\n\n```\nnot this\n```\n\nThis.\n", "This."},
		"no paragraph":           {"# app\n\n## Usage\n", ""},
		"too long":               {"# app\n\n" + strings.Repeat("word ", 60) + "\n", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "README.md")
			require.NoError(t, os.WriteFile(path, []byte(tt.readme), 0o644))
			assert.Equal(t, tt.want, readmeDescription(path))
		})
	}
}

func TestCanRunDebSmokeTestPlatform(t *testing.T) {
	docker := func(output string, err error) dockerRunner {
		return func(context.Context, ...string) (string, error) { return output, err }
	}

	assert.True(t, canRunDebSmokeTestPlatform(context.Background(), docker("", nil), "arm64"))
	assert.False(t, canRunDebSmokeTestPlatform(context.Background(), docker("exec /usr/bin/true: exec format error", assert.AnError), "arm64"))
	assert.True(t, canRunDebSmokeTestPlatform(context.Background(), docker("docker: command not found", assert.AnError), "arm64"))
}
