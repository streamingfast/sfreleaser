package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goreleaser/nfpm/v2"
	_ "github.com/goreleaser/nfpm/v2/deb"
)

// debSmokeTestImage is the image the packages are installed in by the smoke test.
const debSmokeTestImage = "debian:stable-slim"

// debPackage is one Debian package built from the binary of a Linux target.
type debPackage = nativeArchive

// debArch is the Debian architecture of a target, which is its GOARCH for the ones built.
func debArch(target nativeTarget) string {
	return target.GOARCH
}

// debFileName is the file name of the package of a target, '<name>_<version>_<arch>.deb'
// with the version stripped of its 'v'.
func debFileName(name, version string, target nativeTarget) string {
	return fmt.Sprintf("%s_%s_%s.deb", name, strings.TrimPrefix(version, "v"), debArch(target))
}

// debEnv is the environment the nfpm configuration is rendered with, on top of the current one:
// PKG_VERSION (without its 'v'), PKG_ARCH and PKG_BINARY, the absolute path of the binary
// built for the architecture.
func debEnv(version string, target nativeTarget, binary string) map[string]string {
	return map[string]string{
		"PKG_VERSION": strings.TrimPrefix(version, "v"),
		"PKG_ARCH":    debArch(target),
		"PKG_BINARY":  binary,
	}
}

// nfpmPackage packages configPath to the file targetPath with the nfpm library, which is
// embedded so the packaging does not depend on the Go version of the project released. The
// configuration is expanded with the current environment, env taking precedence.
func nfpmPackage(configPath, targetPath string, env map[string]string) (err error) {
	config, err := nfpm.ParseFileWithEnvMapping(configPath, func(key string) string {
		if value, found := env[key]; found {
			return value
		}
		return os.Getenv(key)
	})
	if err != nil {
		return fmt.Errorf("parsing %s: %w", configPath, err)
	}

	info, err := config.Get("deb")
	if err != nil {
		return err
	}
	info = nfpm.WithDefaults(info)
	info.Target = targetPath

	packager, err := nfpm.Get("deb")
	if err != nil {
		return err
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			os.Remove(targetPath)
		}
	}()

	return packager.Package(info, file)
}

type debOptions struct {
	// Name is the package name, which starts the file names.
	Name    string
	Version string
	// ConfigPath is the nfpm configuration file, generated from Config when empty.
	ConfigPath string
	// Config describes the package to generate the nfpm configuration of when ConfigPath is
	// empty, its Version, Arch and Binary are set per target.
	Config debConfigOptions
	// Binaries maps a target id to the path of its binary, as returned by [nativeBuild].
	Binaries  map[string]string
	OutputDir string
	// SmokeTest installs every package in a Debian container once built.
	SmokeTest bool
}

// buildDebPackages builds one package per built Linux target with nfpm.
func buildDebPackages(ctx context.Context, options debOptions) ([]debPackage, error) {
	var packages []debPackage
	for _, target := range nativeTargets {
		binary, found := options.Binaries[target.ID]
		if !found || target.GOOS != "linux" {
			continue
		}

		binary, err := filepath.Abs(binary)
		if err != nil {
			return nil, err
		}

		pkg := debPackage{Target: target, Name: debFileName(options.Name, options.Version, target)}
		pkg.Path = filepath.Join(options.OutputDir, pkg.Name)

		if err := os.MkdirAll(options.OutputDir, 0o755); err != nil {
			return nil, err
		}

		configPath := options.ConfigPath
		if configPath == "" {
			config := options.Config
			config.Version = strings.TrimPrefix(options.Version, "v")
			config.Arch = debArch(target)
			config.Binary = binary

			rendered, err := renderNfpmConfig(config)
			if err != nil {
				return nil, err
			}

			configPath = filepath.Join(options.OutputDir, "nfpm-"+debArch(target)+".yaml")
			if err := os.WriteFile(configPath, []byte(rendered), 0o644); err != nil {
				return nil, err
			}
		}

		fmt.Printf("Packaging %s\n", pkg.Name)
		if err := nfpmPackage(configPath, pkg.Path, debEnv(options.Version, target, binary)); err != nil {
			return nil, fmt.Errorf("packaging %s with nfpm: %w", pkg.Name, err)
		}

		if pkg.SHA256, err = fileSHA256(pkg.Path); err != nil {
			return nil, fmt.Errorf("hashing %s: %w", pkg.Name, err)
		}

		if options.SmokeTest {
			fmt.Printf("Installing %s in %s\n", pkg.Name, debSmokeTestImage)
			if err := smokeTestDeb(ctx, pkg); err != nil {
				return nil, err
			}
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}

// smokeTestDeb installs the package in a clean Debian container of its architecture.
func smokeTestDeb(ctx context.Context, pkg debPackage) error {
	directory, err := filepath.Abs(filepath.Dir(pkg.Path))
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"--platform", "linux/"+debArch(pkg.Target),
		"-v", directory+":/packages:ro",
		debSmokeTestImage,
		"sh", "-c", "apt-get update -qq && apt-get install -y -qq /packages/"+pkg.Name,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing %s: %w", pkg.Name, err)
	}

	return nil
}
