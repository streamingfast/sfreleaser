package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:generate go run ./internal/macossdkgen -out macos_sdk

// macosSDK holds the macOS SDK '.tbd' stubs darwin binaries are linked against by the
// native builder: zig ships the macOS libc headers but not Apple's SDK libraries. They are
// text files listing symbols, extracted from an osxcross image by 'go generate'.
//
//go:embed macos_sdk
var macosSDK embed.FS

// macosSDKAliases are the unversioned names the linker looks up, symlinks in the SDK that
// cannot be embedded, so they are recreated as copies of their target.
var macosSDKAliases = map[string]string{
	"usr/lib/libSystem.tbd": "usr/lib/libSystem.B.tbd",
	"usr/lib/libresolv.tbd": "usr/lib/libresolv.9.tbd",
	"usr/lib/libobjc.tbd":   "usr/lib/libobjc.A.tbd",
}

// materializeMacOSSDK writes the embedded stubs under cacheRoot and returns the directory to
// pass as zig's '--sysroot'. The directory is named after the content hash of the stubs, so
// an sfreleaser embedding different stubs never reuses a stale copy, and an existing complete
// copy is reused as is.
func materializeMacOSSDK(cacheRoot string) (string, error) {
	files, digest, err := macosSDKFiles()
	if err != nil {
		return "", err
	}

	root := filepath.Join(cacheRoot, "macos-sdk", digest)
	marker := filepath.Join(root, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return root, nil
	}

	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", fmt.Errorf("creating macOS SDK cache directory: %w", err)
	}

	staging, err := os.MkdirTemp(filepath.Dir(root), "staging-")
	if err != nil {
		return "", fmt.Errorf("creating macOS SDK staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	write := func(name string, content []byte) error {
		destination := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destination, content, 0o644)
	}

	for name, content := range files {
		if err := write(name, content); err != nil {
			return "", fmt.Errorf("writing macOS SDK stub %q: %w", name, err)
		}
	}

	for alias, target := range macosSDKAliases {
		if err := write(alias, files[target]); err != nil {
			return "", fmt.Errorf("writing macOS SDK alias %q: %w", alias, err)
		}
	}

	if err := os.WriteFile(filepath.Join(staging, ".complete"), nil, 0o644); err != nil {
		return "", err
	}

	// MkdirTemp creates it private to the user.
	if err := os.Chmod(staging, 0o755); err != nil {
		return "", err
	}

	// Another sfreleaser may have materialized the same stubs concurrently, its copy is as
	// good as ours.
	if err := os.Rename(staging, root); err != nil {
		if _, statErr := os.Stat(marker); statErr == nil {
			return root, nil
		}
		return "", fmt.Errorf("moving macOS SDK into place: %w", err)
	}

	return root, nil
}

// macosSDKFiles returns the embedded stubs keyed by their path relative to the SDK root, and
// a short digest of their content.
func macosSDKFiles() (map[string][]byte, string, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(macosSDK, "macos_sdk", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".tbd") {
			return err
		}

		content, err := macosSDK.ReadFile(path)
		if err != nil {
			return err
		}

		files[strings.TrimPrefix(path, "macos_sdk/")] = content
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("reading embedded macOS SDK: %w", err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	hash := sha256.New()
	for _, name := range names {
		fmt.Fprintf(hash, "%s\x00%d\x00", name, len(files[name]))
		hash.Write(files[name])
	}

	return files, hex.EncodeToString(hash.Sum(nil))[:16], nil
}
