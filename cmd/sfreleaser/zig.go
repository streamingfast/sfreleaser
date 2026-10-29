package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"go.uber.org/zap"
)

// zigVersion is the zig the native builder uses as its cgo cross-compiler. Older versions
// fail to link darwin binaries against the macOS SDK stubs.
const zigVersion = "0.17.0"

// zigChecksums are the SHA256 of the zig tarball of each host, from
// https://ziglang.org/download/index.json.
var zigChecksums = map[string]string{
	"aarch64-linux": "9e8d11661d4ae3bd57702a3832781e23ad151dde5798e16a5ccd503f65234ff8",
	"x86_64-linux":  "1cbe9df9f27e6b78d14ccbca43b6703a404ef79ef1c463de901d7f088d4e2026",
	"aarch64-macos": "b607e9b9234790a008116ae5bdb71c6243b84b9fb42a53a9e70fde41c06c536a",
	"x86_64-macos":  "4f9a1c5269aa17ebda5e6d3c2b89d6cbf36f7d2b22a0306e9ab98f25f95529c6",
}

var zigDownloadBaseURL = "https://ziglang.org/download"

// zigHost is the name zig gives to the host sfreleaser runs on, as used in its tarball names.
func zigHost(goos, goarch string) (string, error) {
	arch, ok := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if !ok {
		return "", fmt.Errorf("no zig %s build for architecture %q", zigVersion, goarch)
	}

	system, ok := map[string]string{"darwin": "macos", "linux": "linux"}[goos]
	if !ok {
		return "", fmt.Errorf("no zig %s build for operating system %q", zigVersion, goos)
	}

	return arch + "-" + system, nil
}

// resolveZig returns the path of the zig binary to build with: override when set, else a
// 'zig' on PATH at version [zigVersion], else the copy cached under cacheRoot, downloading
// and verifying it first if needed.
func resolveZig(ctx context.Context, override string, cacheRoot string) (string, error) {
	if override != "" {
		return resolveZigOverride(ctx, override)
	}

	if path, err := exec.LookPath("zig"); err == nil {
		version, err := zigBinaryVersion(ctx, path)
		if err == nil && version == zigVersion {
			// Go refuses a relative C compiler, which a relative PATH entry gives.
			return filepath.Abs(path)
		}

		zlog.Debug("zig on PATH is not the pinned version", zap.String("path", path), zap.String("version", version), zap.Error(err))
	}

	installed := filepath.Join(cacheRoot, "zig", zigVersion, "zig")
	if _, err := os.Stat(installed); err == nil {
		return installed, nil
	}

	host, err := zigHost(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}

	fmt.Printf("Downloading zig %s for %s (once, cached under %s)\n", zigVersion, host, filepath.Join(cacheRoot, "zig"))
	if err := installZig(ctx, host, filepath.Dir(installed)); err != nil {
		return "", fmt.Errorf("installing zig %s: %w", zigVersion, err)
	}

	return installed, nil
}

// resolveZigOverride returns the absolute path of the zig given through 'zig-path': a bare name
// is looked up on PATH, a leading '~' is the home directory. A version other than [zigVersion]
// is allowed, with a warning, since older ones fail to link darwin binaries.
func resolveZigOverride(ctx context.Context, override string) (string, error) {
	path := override
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}

	if !strings.ContainsRune(path, filepath.Separator) {
		found, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("zig path %q: %w", override, err)
		}
		path = found
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("zig path %q: %w", override, err)
	}

	if version, err := zigBinaryVersion(ctx, path); err != nil || version != zigVersion {
		fmt.Printf("Warning: zig %q is version %q, the native builder is tested with %s only (error: %v)\n", path, version, zigVersion, err)
	}

	return path, nil
}

func zigBinaryVersion(ctx context.Context, path string) (string, error) {
	output, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// installZig downloads the zig tarball of host, verifies its checksum and extracts it to
// destination, which only appears once complete.
func installZig(ctx context.Context, host string, destination string) error {
	checksum, ok := zigChecksums[host]
	if !ok {
		return fmt.Errorf("no zig %s checksum for host %q", zigVersion, host)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	archive, err := os.CreateTemp(filepath.Dir(destination), "zig-*.tar.xz")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()

	url := fmt.Sprintf("%s/%s/zig-%s-%s.tar.xz", zigDownloadBaseURL, zigVersion, host, zigVersion)
	if err := download(ctx, url, archive, checksum); err != nil {
		return err
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(filepath.Dir(destination), "staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := extractTarXz(ctx, archive, staging); err != nil {
		return fmt.Errorf("extracting %s: %w", url, err)
	}

	// MkdirTemp creates it private to the user.
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}

	if err := os.Rename(staging, destination); err != nil {
		// Another sfreleaser may have installed it concurrently.
		if _, statErr := os.Stat(filepath.Join(destination, "zig")); statErr == nil {
			return nil
		}
		return err
	}

	return nil
}

func download(ctx context.Context, url string, to io.Writer, expectedSHA256 string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %s", url, response.Status)
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(to, hash), response.Body); err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}

	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedSHA256 {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", url, expectedSHA256, actual)
	}

	return nil
}

// extractTarXz extracts a '.tar.xz' archive to destination, dropping the single top-level
// directory zig tarballs wrap their content in.
func extractTarXz(ctx context.Context, archive io.Reader, destination string) error {
	decompressed, err := xz.NewReader(archive)
	if err != nil {
		return err
	}

	reader := tar.NewReader(decompressed)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		_, name, found := strings.Cut(header.Name, "/")
		if !found || name == "" {
			continue
		}

		target := filepath.Join(destination, filepath.FromSlash(name))
		if !strings.HasPrefix(target, filepath.Clean(destination)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q escapes the destination", header.Name)
		}
		// The check above is lexical, an entry written through a symlink extracted earlier could
		// still land elsewhere.
		if err := ensureNoSymlinkOnPath(destination, target); err != nil {
			return fmt.Errorf("archive entry %q: %w", header.Name, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}

			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, reader)
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}

		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) || !strings.HasPrefix(filepath.Join(filepath.Dir(target), header.Linkname), filepath.Clean(destination)+string(os.PathSeparator)) {
				return fmt.Errorf("archive symlink %q points outside the destination", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}

		case tar.TypeXGlobalHeader:
			// Carries archive-wide metadata, no file.

		default:
			return fmt.Errorf("archive entry %q has unsupported type %q", header.Name, header.Typeflag)
		}
	}
}

// ensureNoSymlinkOnPath fails when target, or any directory between root and target, already
// exists as a symlink.
func ensureNoSymlinkOnPath(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}

	current := root
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%q is a symlink extracted earlier", current)
		}
	}

	return nil
}

// sfreleaserCacheDir is where the native builder keeps zig and the macOS SDK stubs.
func sfreleaserCacheDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("finding the user cache directory: %w", err)
	}

	return filepath.Join(root, "sfreleaser"), nil
}
