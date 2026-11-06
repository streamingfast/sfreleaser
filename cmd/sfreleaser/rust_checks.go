package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/streamingfast/cli"
)

// cratesIOTokenCheckURL accepts API tokens and authenticates them before looking up the token
// id, which never exists, so it answers without side effects.
var cratesIOTokenCheckURL = "https://crates.io/api/v1/me/tokens/0"

const (
	cratesIOAuthFailed       = "authentication failed"
	cratesIOScopedTokenValid = "this token does not have the required permissions to perform this action"
)

// verifyCratesRegistryAuth fails the release when the crates.io token is revoked, expired or
// unknown, before anything is published. A scoped token cannot be checked against the crates
// it may publish, crates.io only does that on the publish call itself. A missing token fails
// in CI only when publishing.
func verifyCratesRegistryAuth(rust *RustReleaseModel, publishing bool) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	if devSkipRustCargoPublish {
		return
	}

	if !publishesToCratesIO(rust) {
		fmt.Println("Crates are not published to crates.io, skipping the registry token check")
		return
	}

	token, source := cratesRegistryToken()
	if token == "" {
		if publishing && isCI() {
			cli.Quit("No crates.io token found, set CARGO_REGISTRY_TOKEN to publish the crates")
		}

		fmt.Println("No crates.io token found in CARGO_REGISTRY_TOKEN nor in Cargo's credentials file, skipping the registry token check")
		return
	}

	if strings.HasPrefix(token, cratesIOTrustedPublishingTokenPrefix) {
		fmt.Printf("The crates.io token from %s is a Trusted Publishing token, crates.io checks it when publishing\n", source)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cli.NoError(checkCratesRegistryToken(ctx, cratesIOTokenCheckURL, token), "The crates.io token from %s cannot publish", source)
	fmt.Printf("The crates.io token from %s is valid\n", source)
}

// cratesRegistryToken returns the token 'cargo publish' uses for crates.io, from
// CARGO_REGISTRY_TOKEN or else Cargo's credentials file. A token held by a credential
// provider, like the macOS keychain, is not found.
func cratesRegistryToken() (token string, source string) {
	if token := os.Getenv("CARGO_REGISTRY_TOKEN"); token != "" {
		return token, "CARGO_REGISTRY_TOKEN"
	}

	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		cargoHome = filepath.Join(home, ".cargo")
	}

	for _, name := range []string{"credentials.toml", "credentials"} {
		path := filepath.Join(cargoHome, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var credentials struct {
			Registry struct {
				Token string `toml:"token"`
			} `toml:"registry"`
		}
		if err := toml.Unmarshal(content, &credentials); err != nil || credentials.Registry.Token == "" {
			continue
		}

		return credentials.Registry.Token, path
	}

	return "", ""
}

func checkCratesRegistryToken(ctx context.Context, url string, token string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building crates.io request: %w", err)
	}
	request.Header.Set("Authorization", token)
	request.Header.Set("User-Agent", "sfreleaser (https://github.com/streamingfast/sfreleaser)")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("querying crates.io: %w", err)
	}
	defer response.Body.Close()

	// A legacy token passes authentication and gets to the lookup of a token id that never exists.
	if response.StatusCode == http.StatusNotFound {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var payload struct {
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(body, &payload)

	detail := ""
	if len(payload.Errors) > 0 {
		detail = payload.Errors[0].Detail
	}

	switch {
	case response.StatusCode == http.StatusForbidden && detail == cratesIOScopedTokenValid:
		return nil
	case response.StatusCode == http.StatusForbidden && detail == cratesIOAuthFailed:
		return fmt.Errorf("crates.io rejected it, it is revoked, expired or unknown")
	case detail != "":
		return fmt.Errorf("crates.io answered %s: %s", response.Status, detail)
	default:
		return fmt.Errorf("crates.io answered %s", response.Status)
	}
}

// verifyRustCratesVersion fails when a configured crate's Cargo.toml version is not the
// release version, the bump must be part of the released commit.
func verifyRustCratesVersion(rust *RustReleaseModel, version string) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	versions, err := parseCargoMetadataVersions([]byte(resultOf("cargo metadata --format-version 1 --no-deps")))
	cli.NoError(err, "Unable to read the crates versions")

	mismatches := mismatchedCrateVersions(versions, rust.Crates, strings.TrimPrefix(version, "v"))
	cli.Ensure(len(mismatches) == 0, "The Cargo.toml version of some crates is not %s: %s", version, strings.Join(mismatches, ", "))
}

func parseCargoMetadataVersions(metadata []byte) (map[string]string, error) {
	var parsed struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(metadata, &parsed); err != nil {
		return nil, fmt.Errorf("parsing 'cargo metadata' output: %w", err)
	}

	versions := make(map[string]string, len(parsed.Packages))
	for _, pkg := range parsed.Packages {
		versions[pkg.Name] = pkg.Version
	}

	return versions, nil
}

func mismatchedCrateVersions(versions map[string]string, crates []string, version string) (mismatches []string) {
	for _, crate := range crates {
		actual, found := versions[crate]
		switch {
		case !found:
			mismatches = append(mismatches, crate+" (not in the workspace)")
		case actual != version:
			mismatches = append(mismatches, crate+" is "+actual)
		}
	}

	return mismatches
}

// publishesToCratesIO reports whether the crates go to crates.io, false when the publish args
// name another registry.
func publishesToCratesIO(rust *RustReleaseModel) bool {
	return !slices.ContainsFunc(rust.CargoPublishArgs, func(arg string) bool {
		return arg == "--registry" || arg == "--index" || strings.HasPrefix(arg, "--registry=") || strings.HasPrefix(arg, "--index=")
	})
}
