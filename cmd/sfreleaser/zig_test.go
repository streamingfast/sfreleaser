package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ulikunitz/xz"
)

func Test_zigHost(t *testing.T) {
	host, err := zigHost("darwin", "arm64")
	require.NoError(t, err)
	assert.Equal(t, "aarch64-macos", host)

	host, err = zigHost("linux", "amd64")
	require.NoError(t, err)
	assert.Equal(t, "x86_64-linux", host)

	_, err = zigHost("windows", "amd64")
	assert.Error(t, err)
}

type tarEntry struct {
	name     string
	content  string
	linkname string
	dir      bool
}

func tarXz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	compressed, err := xz.NewWriter(&buffer)
	require.NoError(t, err)

	writer := tar.NewWriter(compressed)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.content)), Typeflag: tar.TypeReg}
		switch {
		case entry.dir:
			header.Typeflag, header.Size = tar.TypeDir, 0
		case entry.linkname != "":
			header.Typeflag, header.Size, header.Linkname = tar.TypeSymlink, 0, entry.linkname
		}

		require.NoError(t, writer.WriteHeader(header))
		_, err := writer.Write([]byte(entry.content))
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())
	require.NoError(t, compressed.Close())
	return buffer.Bytes()
}

// serveZig serves archive as the zig tarball of a fake host and registers its checksum.
func serveZig(t *testing.T, archive []byte) string {
	t.Helper()

	host := "test-host"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+zigVersion+"/zig-"+host+"-"+zigVersion+".tar.xz" {
			http.NotFound(w, r)
			return
		}
		w.Write(archive)
	}))
	t.Cleanup(server.Close)

	previousURL := zigDownloadBaseURL
	zigDownloadBaseURL = server.URL
	sum := sha256.Sum256(archive)
	zigChecksums[host] = hex.EncodeToString(sum[:])
	t.Cleanup(func() {
		zigDownloadBaseURL = previousURL
		delete(zigChecksums, host)
	})

	return host
}

func Test_installZig(t *testing.T) {
	archive := tarXz(t,
		tarEntry{name: "zig-test-host-0.17.0/", dir: true},
		tarEntry{name: "zig-test-host-0.17.0/zig", content: "binary"},
		tarEntry{name: "zig-test-host-0.17.0/lib/std/std.zig", content: "std"},
		tarEntry{name: "zig-test-host-0.17.0/lib/latest.zig", linkname: "std/std.zig"},
	)
	host := serveZig(t, archive)

	destination := filepath.Join(t.TempDir(), "zig", zigVersion)
	require.NoError(t, installZig(t.Context(), host, destination))

	content, err := os.ReadFile(filepath.Join(destination, "zig"))
	require.NoError(t, err)
	assert.Equal(t, "binary", string(content))

	info, err := os.Stat(filepath.Join(destination, "zig"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	link, err := os.Readlink(filepath.Join(destination, "lib", "latest.zig"))
	require.NoError(t, err)
	assert.Equal(t, "std/std.zig", link)
}

func Test_installZig_checksumMismatch(t *testing.T) {
	host := serveZig(t, tarXz(t, tarEntry{name: "zig-test-host-0.17.0/zig", content: "binary"}))
	zigChecksums[host] = "0000"

	destination := filepath.Join(t.TempDir(), "zig", zigVersion)
	err := installZig(t.Context(), host, destination)
	assert.ErrorContains(t, err, "checksum mismatch")
	assert.NoDirExists(t, destination)
}

func Test_extractTarXz_rejectsEscapes(t *testing.T) {
	for name, entry := range map[string]tarEntry{
		"path":    {name: "zig-x/../../escape", content: "x"},
		"symlink": {name: "zig-x/link", linkname: "../../escape"},
	} {
		t.Run(name, func(t *testing.T) {
			err := extractTarXz(t.Context(), bytes.NewReader(tarXz(t, entry)), t.TempDir())
			assert.Error(t, err)
		})
	}

	t.Run("through an extracted symlink", func(t *testing.T) {
		archive := tarXz(t,
			tarEntry{name: "zig-x/sub/", dir: true},
			tarEntry{name: "zig-x/link", linkname: "sub"},
			tarEntry{name: "zig-x/link/file", content: "x"},
		)
		err := extractTarXz(t.Context(), bytes.NewReader(archive), t.TempDir())
		assert.ErrorContains(t, err, "symlink extracted earlier")
	})
}

func Test_extractTarXz_rejectsUnknownTypes(t *testing.T) {
	var buffer bytes.Buffer
	compressed, err := xz.NewWriter(&buffer)
	require.NoError(t, err)
	writer := tar.NewWriter(compressed)
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "zig-x/hard", Typeflag: tar.TypeLink, Linkname: "zig-x/zig"}))
	require.NoError(t, writer.Close())
	require.NoError(t, compressed.Close())

	err = extractTarXz(t.Context(), &buffer, t.TempDir())
	assert.ErrorContains(t, err, "unsupported type")
}

func Test_resolveZig(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		zig := filepath.Join(t.TempDir(), "my-zig")
		require.NoError(t, os.WriteFile(zig, nil, 0o755))

		path, err := resolveZig(t.Context(), zig, t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, zig, path)
	})

	t.Run("on PATH at the pinned version", func(t *testing.T) {
		bin := fakeZigOnPath(t, zigVersion)

		path, err := resolveZig(t.Context(), "", t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(bin, "zig"), path)
	})

	t.Run("override by name on PATH", func(t *testing.T) {
		bin := fakeZigOnPath(t, zigVersion)

		path, err := resolveZig(t.Context(), "zig", t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(bin, "zig"), path)
	})

	t.Run("relative override made absolute", func(t *testing.T) {
		directory := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(directory, "my-zig"), nil, 0o755))
		t.Chdir(directory)

		path, err := resolveZig(t.Context(), "./my-zig", t.TempDir())
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(path), path)
	})

	t.Run("cached when PATH has another version", func(t *testing.T) {
		fakeZigOnPath(t, "0.15.2")

		cacheRoot := t.TempDir()
		cached := filepath.Join(cacheRoot, "zig", zigVersion, "zig")
		require.NoError(t, os.MkdirAll(filepath.Dir(cached), 0o755))
		require.NoError(t, os.WriteFile(cached, nil, 0o755))

		path, err := resolveZig(t.Context(), "", cacheRoot)
		require.NoError(t, err)
		assert.Equal(t, cached, path)
	})
}

// fakeZigOnPath puts on PATH, alone, a 'zig' script printing version.
func fakeZigOnPath(t *testing.T, version string) string {
	t.Helper()

	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "zig"), []byte("#!/bin/sh\necho "+version+"\n"), 0o755))
	t.Setenv("PATH", bin)

	return bin
}
