package main

import (
	"io/fs"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
	"github.com/streamingfast/cli"
)

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
