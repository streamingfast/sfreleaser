// Command macossdkgen extracts the macOS SDK '.tbd' stubs the native builder links darwin
// binaries against from an osxcross Docker image, and writes them, along with a VERSION
// marker, to the output directory. It is run through 'go generate' in 'cmd/sfreleaser'.
package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// stubs are the files a Go cgo darwin link needs, relative to the SDK root: libSystem
// (implicit), libresolv (net), CoreFoundation and Security (crypto/x509) and libobjc,
// which CoreFoundation re-exports.
var stubs = []string{
	"usr/lib/libSystem.B.tbd",
	"usr/lib/libresolv.9.tbd",
	"usr/lib/libobjc.A.tbd",
	"System/Library/Frameworks/CoreFoundation.framework/CoreFoundation.tbd",
	"System/Library/Frameworks/Security.framework/Security.tbd",
}

func main() {
	image := flag.String("image", "crazymax/osxcross:26.1", "osxcross image holding the macOS SDK under /osxcross/SDK")
	platform := flag.String("platform", "linux/arm64", "Platform of the image to pull, the stubs are the same for every platform")
	out := flag.String("out", "macos_sdk", "Directory the stubs and VERSION marker are written to")
	flag.Parse()

	if err := generate(*image, *platform, *out); err != nil {
		fmt.Fprintln(os.Stderr, "macossdkgen:", err)
		os.Exit(1)
	}
}

func generate(image, platform, out string) error {
	if _, err := docker("pull", "--platform", platform, image); err != nil {
		return err
	}

	id, err := docker("create", "--platform", platform, image, "unused")
	if err != nil {
		return err
	}
	defer docker("rm", id)

	sdk, err := findSDK(id)
	if err != nil {
		return err
	}

	// Written next to out and renamed at the end, so a failure leaves the current stubs intact.
	staging, err := os.MkdirTemp(filepath.Dir(filepath.Clean(out)), ".macossdkgen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	for _, stub := range stubs {
		destination := filepath.Join(staging, filepath.FromSlash(stub))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}

		if _, err := docker("cp", "-L", id+":/osxcross/SDK/"+sdk+"/"+stub, destination); err != nil {
			return err
		}
	}

	// The digest pins what the tag pointed to, the stubs only change when it does.
	digest, err := docker("image", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	if err != nil {
		return err
	}

	version := fmt.Sprintf("image=%s\nsdk=%s\n", digest, strings.TrimSuffix(sdk, ".sdk"))
	if err := os.WriteFile(filepath.Join(staging, "VERSION"), []byte(version), 0o644); err != nil {
		return err
	}

	if err := os.RemoveAll(out); err != nil {
		return err
	}

	return os.Rename(staging, out)
}

// findSDK returns the name of the single 'MacOSX*.sdk' directory under /osxcross/SDK, so
// that a newer image works without changing this tool. The image has no shell, so the
// directory is listed by streaming it as a tar archive and reading its entry names.
func findSDK(id string) (string, error) {
	cmd := exec.Command("docker", "cp", id+":/osxcross/SDK", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}

	sdks := map[string]bool{}
	reader := tar.NewReader(stdout)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return "", fmt.Errorf("reading /osxcross/SDK listing: %w", err)
		}

		parts := strings.Split(header.Name, "/")
		if len(parts) >= 2 && strings.HasPrefix(parts[1], "MacOSX") && strings.HasSuffix(parts[1], ".sdk") && header.Typeflag == tar.TypeDir {
			sdks[parts[1]] = true
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("docker cp %s:/osxcross/SDK: %w: %s", id, err, strings.TrimSpace(stderr.String()))
	}

	if len(sdks) != 1 {
		return "", fmt.Errorf("expected exactly one MacOSX*.sdk directory under /osxcross/SDK, found %d", len(sdks))
	}

	for sdk := range sdks {
		return sdk, nil
	}

	panic("unreachable")
}

func docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
