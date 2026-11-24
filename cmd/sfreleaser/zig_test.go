package main

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jedisct1/go-minisign"
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

// zigSigner signs like the zig project does, with a throwaway key.
type zigSigner struct {
	private ed25519.PrivateKey
	keyID   [8]byte
}

func newZigSigner(t *testing.T) *zigSigner {
	t.Helper()

	_, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	return &zigSigner{private: private, keyID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
}

func (s *zigSigner) publicKey() string {
	raw := append([]byte{'E', 'd'}, s.keyID[:]...)
	raw = append(raw, s.private.Public().(ed25519.PublicKey)...)
	return base64.StdEncoding.EncodeToString(raw)
}

func (s *zigSigner) sign(content []byte) string {
	signature := ed25519.Sign(s.private, content)
	comment := "timestamp:1"

	raw := append([]byte{'E', 'd'}, s.keyID[:]...)
	raw = append(raw, signature...)
	global := ed25519.Sign(s.private, append(append([]byte{}, signature...), comment...))

	return "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) +
		"\ntrusted comment: " + comment + "\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

// isolateZig makes sure no test talks to the real network: no mirror list, no embedded mirrors,
// the zig public key being signer's. Restores everything when done.
func isolateZig(t *testing.T, signer *zigSigner) {
	t.Helper()

	previousKey, previousList, previousFallback := zigMinisignPublicKey, zigCommunityMirrorsURL, zigFallbackMirrors
	previousBase, previousConnect, previousStall := zigDownloadBaseURL, zigConnectTimeout, zigStallTimeout
	previousMax, previousDeadline := zigMaxMirrorAttempts, zigDownloadDeadline
	t.Cleanup(func() {
		zigMinisignPublicKey, zigCommunityMirrorsURL, zigFallbackMirrors = previousKey, previousList, previousFallback
		zigDownloadBaseURL, zigConnectTimeout, zigStallTimeout = previousBase, previousConnect, previousStall
		zigMaxMirrorAttempts, zigDownloadDeadline = previousMax, previousDeadline
	})

	zigMinisignPublicKey = signer.publicKey()
	zigFallbackMirrors = nil
	zigCommunityMirrorsURL = "http://127.0.0.1:1/unreachable"
	zigDownloadBaseURL = "http://127.0.0.1:1/unreachable"
}

// zigServer serves archive and its signature of the fake host under mirror layout ('/<name>')
// and ziglang.org layout ('/<version>/<name>'), recording the requests received.
func zigServer(t *testing.T, name string, archive []byte, signature string) (*httptest.Server, *[]string) {
	t.Helper()

	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		switch strings.TrimPrefix(r.URL.Path, "/"+zigVersion) {
		case "/" + name:
			w.Write(archive)
		case "/" + name + ".minisig":
			w.Write([]byte(signature))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

// serveZig serves archive as the signed zig tarball of a fake host from the ziglang.org
// location, no mirror available, and registers its checksum.
func serveZig(t *testing.T, archive []byte) string {
	t.Helper()

	host := "test-host"
	signer := newZigSigner(t)
	isolateZig(t, signer)

	server, _ := zigServer(t, "zig-"+host+"-"+zigVersion+".tar.xz", archive, signer.sign(archive))
	zigDownloadBaseURL = server.URL

	sum := sha256.Sum256(archive)
	zigChecksums[host] = hex.EncodeToString(sum[:])
	t.Cleanup(func() { delete(zigChecksums, host) })

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

// zigMirrorsFixture is a signed tarball of a fake host with its checksum registered.
type zigMirrorsFixture struct {
	host    string
	name    string
	archive []byte
	signer  *zigSigner
}

func newZigMirrorsFixture(t *testing.T) *zigMirrorsFixture {
	t.Helper()

	archive := tarXz(t, tarEntry{name: "zig-test-host-0.17.0/zig", content: "binary"})
	signer := newZigSigner(t)
	isolateZig(t, signer)

	host := "test-host"
	sum := sha256.Sum256(archive)
	zigChecksums[host] = hex.EncodeToString(sum[:])
	t.Cleanup(func() { delete(zigChecksums, host) })

	return &zigMirrorsFixture{host: host, name: "zig-" + host + "-" + zigVersion + ".tar.xz", archive: archive, signer: signer}
}

// listMirrors serves the community mirror list with urls.
func listMirrors(t *testing.T, urls ...string) {
	t.Helper()

	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "# mirrors\n"+strings.Join(urls, "\n")+"\n")
	}))
	t.Cleanup(list.Close)
	zigCommunityMirrorsURL = list.URL
}

func installTo(t *testing.T, f *zigMirrorsFixture) error {
	t.Helper()
	return installZig(t.Context(), f.host, filepath.Join(t.TempDir(), "zig", zigVersion))
}

func Test_installZig_mirrorFirstWithSourceParam(t *testing.T) {
	f := newZigMirrorsFixture(t)
	mirror, mirrorRequests := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	official, officialRequests := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	zigDownloadBaseURL = official.URL
	listMirrors(t, mirror.URL)

	require.NoError(t, installTo(t, f))
	assert.Equal(t, []string{"/" + f.name + "?source=sfreleaser", "/" + f.name + ".minisig?source=sfreleaser"}, *mirrorRequests)
	assert.Empty(t, *officialRequests, "ziglang.org must not be hit when a mirror works")
}

func Test_installZig_failedMirrorsFallThroughToNext(t *testing.T) {
	f := newZigMirrorsFixture(t)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 500) }))
	t.Cleanup(broken.Close)
	good, _ := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	listMirrors(t, broken.URL, good.URL)

	require.NoError(t, installTo(t, f))
}

func Test_installZig_badSignatureTriesNextMirror(t *testing.T) {
	f := newZigMirrorsFixture(t)
	other := newZigSigner(t)
	evil, _ := zigServer(t, f.name, f.archive, other.sign(f.archive))
	good, _ := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	listMirrors(t, evil.URL, good.URL)

	// Mirrors come in random order, whichever the first, the bad one never gets installed.
	for range 5 {
		require.NoError(t, installTo(t, f))
	}
}

func Test_installZig_badSignatureEverywhereFails(t *testing.T) {
	f := newZigMirrorsFixture(t)
	other := newZigSigner(t)
	evil, _ := zigServer(t, f.name, f.archive, other.sign(f.archive))
	zigDownloadBaseURL = evil.URL
	listMirrors(t, evil.URL)

	destination := filepath.Join(t.TempDir(), "zig", zigVersion)
	err := installZig(t.Context(), f.host, destination)
	assert.ErrorContains(t, err, "verifying signature")
	assert.NoDirExists(t, destination)
}

func Test_installZig_stalledMirrorTimesOut(t *testing.T) {
	f := newZigMirrorsFixture(t)
	zigStallTimeout = 200 * time.Millisecond

	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(release) })

	good, _ := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	zigDownloadBaseURL = good.URL
	listMirrors(t, stalled.URL)

	start := time.Now()
	require.NoError(t, installTo(t, f))
	assert.Less(t, time.Since(start), 5*time.Second)
}

func Test_installZig_stallMidBody(t *testing.T) {
	f := newZigMirrorsFixture(t)
	zigStallTimeout = 200 * time.Millisecond

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(f.archive[:10])
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })

	good, _ := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	zigDownloadBaseURL = good.URL
	listMirrors(t, slow.URL)

	require.NoError(t, installTo(t, f))
}

func Test_installZig_embeddedMirrorsWhenListUnavailable(t *testing.T) {
	f := newZigMirrorsFixture(t)
	mirror, requests := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	zigFallbackMirrors = []string{mirror.URL}

	require.NoError(t, installTo(t, f))
	assert.NotEmpty(t, *requests)
}

func Test_installZig_officialIsLastResort(t *testing.T) {
	f := newZigMirrorsFixture(t)
	official, requests := zigServer(t, f.name, f.archive, f.signer.sign(f.archive))
	zigDownloadBaseURL = official.URL
	listMirrors(t, "http://127.0.0.1:1/zig")

	require.NoError(t, installTo(t, f))
	assert.Equal(t, []string{"/" + zigVersion + "/" + f.name, "/" + zigVersion + "/" + f.name + ".minisig"}, *requests)
}

func Test_embeddedZigPublicKeyIsValid(t *testing.T) {
	_, err := minisign.NewPublicKey(zigMinisignPublicKey)
	require.NoError(t, err)
}

func Test_installZig_capsMirrorAttempts(t *testing.T) {
	f := newZigMirrorsFixture(t)
	zigMaxMirrorAttempts = 2

	var mu sync.Mutex
	hits := 0
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Error(w, "down", 500)
	}))
	t.Cleanup(broken.Close)
	zigDownloadBaseURL = broken.URL
	// 5 mirrors listed (same server), 2 tried, then ziglang.org: 3 tarball requests.
	listMirrors(t, broken.URL+"/a", broken.URL+"/b", broken.URL+"/c", broken.URL+"/d", broken.URL+"/e")

	err := installTo(t, f)
	assert.ErrorContains(t, err, "all 3 zig sources failed")
	assert.Equal(t, 3, hits)
}

func Test_installZig_overallDeadline(t *testing.T) {
	f := newZigMirrorsFixture(t)
	zigDownloadDeadline = 300 * time.Millisecond
	zigStallTimeout = 10 * time.Second

	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(release) })
	zigDownloadBaseURL = stalled.URL
	listMirrors(t, stalled.URL)

	start := time.Now()
	err := installTo(t, f)
	assert.ErrorContains(t, err, "download deadline")
	assert.Less(t, time.Since(start), 5*time.Second)
}
