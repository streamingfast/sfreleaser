package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	versioning "github.com/hashicorp/go-version"
	"github.com/pelletier/go-toml/v2"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

const (
	// RustCratesPublishModeInferred lets 'sfreleaser' determine how the crates are published based
	// on the Cargo version found locally, publishing them all through a single invocation when
	// Cargo is recent enough.
	RustCratesPublishModeInferred = "inferred"

	// RustCratesPublishModeSequential forces publishing the crates one by one, in the exact order
	// they are configured in 'release.rust-crates'.
	RustCratesPublishModeSequential = "sequential"
)

var rustCratesPublishModes = []string{RustCratesPublishModeInferred, RustCratesPublishModeSequential}

// cargoMultiPackagePublishMinVersion is the first Cargo version accepting multiple '-p <crate>'
// arguments on 'cargo publish'. Starting from it, Cargo computes the dependency order of the
// received crates itself and waits for each of them to be available on the registry before
// publishing the ones depending on it, which makes the configured crates order irrelevant.
var cargoMultiPackagePublishMinVersion = versioning.Must(versioning.NewVersion("1.90.0"))

// cargoVersionRegex extracts the version out of a 'cargo --version' output which looks like
// 'cargo 1.96.0 (30a34c682 2026-05-25)' or 'cargo 1.91.0-nightly (2ea99a1 2026-01-13)'.
var cargoVersionRegex = regexp.MustCompile(`cargo\s+(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)`)

func validateRustCratesPublishMode(mode string) error {
	if !slices.Contains(rustCratesPublishModes, mode) {
		return fmt.Errorf("invalid value %q, accepted values are %s", mode, strings.Join(rustCratesPublishModes, ", "))
	}

	return nil
}

// resolveRustCratesSinglePublishInvocation determines, from the received publish mode, if all the
// crates can be published through a single 'cargo publish' invocation, in which case Cargo orders
// them by dependency itself and the configured crates order becomes irrelevant.
//
// The process exits with an actionable message if the mode is [RustCratesPublishModeInferred] but
// the local Cargo is too old to support it.
func resolveRustCratesSinglePublishInvocation(mode string) bool {
	if mode == RustCratesPublishModeSequential {
		return false
	}

	cli.NoError(validateRustCratesPublishMode(mode), "Invalid 'rust-cargo-publish-mode' config value")

	version, err := cargoVersion()
	cli.NoError(err, "Unable to determine the Cargo version, ensure Rust is installed and 'cargo --version' runs correctly")

	zlog.Debug("resolved cargo version", zap.Stringer("version", version))

	// The prerelease part is dropped because a 'X.Y.Z-nightly' toolchain has the features of 'X.Y.Z'.
	if version.Core().LessThan(cargoMultiPackagePublishMinVersion) {
		cli.Quit("%s", dedent(`
			Your Cargo version %s is too old, 'sfreleaser' publishes all the crates through a single
			'cargo publish' invocation so that Cargo orders them by dependency itself, which requires
			Cargo %s or later.

			Update your Rust toolchain with:

			    rustup update stable

			If you cannot update, you can fall back on publishing the crates one by one by adding
			the following to your '.sfreleaser' file:

			    release:
			        rust-cargo-publish-mode: sequential

			In that mode, the 'release.rust-crates' list must be strictly ordered by dependency, a
			crate must appear after every crate it depends on.
		`, version, cargoMultiPackagePublishMinVersion))
	}

	return true
}

func cargoVersion() (*versioning.Version, error) {
	output, info, err := maybeResultOf("cargo --version")
	if err != nil {
		return nil, fmt.Errorf("command %q failed: %w", info, err)
	}

	return parseCargoVersion(output)
}

func parseCargoVersion(output string) (*versioning.Version, error) {
	groups := cargoVersionRegex.FindStringSubmatch(output)
	if len(groups) == 0 {
		return nil, fmt.Errorf("unable to extract Cargo version out of %q", strings.TrimSpace(output))
	}

	version, err := versioning.NewVersion(groups[1])
	if err != nil {
		return nil, fmt.Errorf("invalid Cargo version %q: %w", groups[1], err)
	}

	return version, nil
}

// rustCratesPublishCommands computes the 'cargo publish' invocation(s) to perform to publish the
// received crates.
//
// When singleInvocation is true, a single command publishing every crate at once is returned so
// that Cargo determines the publishing order itself. Otherwise, one command per crate is returned
// and respecting the dependency order of the received crates is the caller's responsibility.
func rustCratesPublishCommands(crates []string, publishArgs []string, singleInvocation bool, extraArgs ...string) (commands [][]string) {
	if len(crates) == 0 {
		return nil
	}

	base := []string{"cargo", "publish"}
	base = append(base, publishArgs...)
	base = append(base, extraArgs...)

	// Clipping ensures each 'append' below allocates its own backing array
	base = slices.Clip(base)

	if singleInvocation {
		args := base
		for _, crate := range crates {
			args = append(args, "-p", crate)
		}

		return [][]string{args}
	}

	for _, crate := range crates {
		commands = append(commands, append(slices.Clip(base), "-p", crate))
	}

	return commands
}

func commandString(args []string) string {
	return strings.Join(unquotedFlatten(args...), " ")
}

func findAllRustCrates() (crates []string) {
	cargoManifests := map[string]bool{}
	cli.NoError(filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// We walk all directories expect 'target' (fragile in some circumstances, we should skip 'target' only if "root")
			if d.Name() == "target" {
				return fs.SkipDir
			}

			return nil
		}

		if d.Name() == "Cargo.toml" {
			cargoManifests[path] = true
		}

		return nil
	}), "Unable to list all Cargo manifest")

	for manifestPath := range cargoManifests {
		content := cli.ReadFile(manifestPath)

		// FIXME: Skip on read error instead, maybe skip only if flag specified?
		cfg := map[string]any{}
		err := toml.Unmarshal([]byte(content), &cfg)
		cli.NoError(err, "Unable to read manifest")

		if isWorkspaceCargoManifest(manifestPath, cfg) {
			// Workspace manifest are skipped
			continue
		}

		crates = append(crates, extractCargoManifestCrateName(manifestPath, cfg))
	}

	return
}

func isWorkspaceCargoManifest(path string, cfg map[string]any) bool {
	return findCargoManifestSection(path, cfg, "workspace") != nil
}

func extractCargoManifestCrateName(path string, cfg map[string]any) string {
	pkg := findCargoManifestSection(path, cfg, "package")
	name, found := pkg["name"]
	if !found {
		return filepath.Base(filepath.Dir(path))
	}

	return name.(string)
}

func findCargoManifestSection(path string, cfg map[string]any, name string) map[string]any {
	for sectionName, section := range cfg {
		if sectionName == name {
			v, ok := section.(map[string]any)
			cli.Ensure(ok, "Cargo manifest at %q is invalid, section %q should have key/value pairs, got type %T", path, name, section)

			return v
		}
	}

	return nil
}
