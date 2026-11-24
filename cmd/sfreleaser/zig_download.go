package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jedisct1/go-minisign"
	"go.uber.org/zap"
)

// zigMinisignPublicKey is the key the zig project signs its releases with, from
// https://ziglang.org/download.
var zigMinisignPublicKey = "RWSGOq2NVecA2UPNdBUZykf1CCb147pkmdtYxgb3Ti+JO/wCYvhbAb/U"

// zigCommunityMirrorsURL lists the community mirrors of zig releases, one base URL per line.
var zigCommunityMirrorsURL = "https://ziglang.org/download/community-mirrors.txt"

// zigFallbackMirrors is used when [zigCommunityMirrorsURL] cannot be fetched, which is likely
// when ziglang.org is having an outage.
var zigFallbackMirrors = []string{
	"https://pkg.hexops.org/zig",
	"https://zigmirror.hryx.net/zig",
	"https://zig.linus.dev/zig",
	"https://zig.squirl.dev",
	"https://zig.mirror.mschae23.de/zig",
	"https://ziglang.freetls.fastly.net",
	"https://zig.tilok.dev",
	"https://zig-mirror.tsimnet.eu/zig",
	"https://zig.karearl.com/zig",
	"https://pkg.earth/zig",
	"https://fs.liujiacai.net/zigbuilds",
	"https://zigmirror.com",
	"https://zig.chainsafe.dev",
	"https://zig.savalione.com",
	"https://zig.bcr.ist",
	"https://zig.vortan.dev/zig",
	"https://pkg.alexrp.com/zig",
}

const zigMirrorSource = "sfreleaser"

var (
	// zigConnectTimeout bounds establishing a connection (dial and TLS handshake).
	zigConnectTimeout = 15 * time.Second
	// zigStallTimeout aborts an attempt when no byte arrived for that long.
	zigStallTimeout = 60 * time.Second
	// zigMaxMirrorAttempts caps the mirrors tried before ziglang.org, which always stays the last
	// source.
	zigMaxMirrorAttempts = 5
	// zigDownloadDeadline caps the whole download, all sources included.
	zigDownloadDeadline = 3 * time.Minute
)

// zigSource is one place a zig tarball and its signature can be downloaded from.
type zigSource struct {
	tarballURL   string
	signatureURL string
}

// zigSources returns the sources to try for tarball name, in order: the community mirrors in
// random order, then ziglang.org as last resort, since the zig project asks CI not to hit it.
func zigSources(ctx context.Context, name string) []zigSource {
	mirrors := fetchZigMirrors(ctx)
	rand.Shuffle(len(mirrors), func(i, j int) { mirrors[i], mirrors[j] = mirrors[j], mirrors[i] })
	mirrors = mirrors[:min(len(mirrors), zigMaxMirrorAttempts)]

	sources := make([]zigSource, 0, len(mirrors)+1)
	for _, mirror := range mirrors {
		base := strings.TrimRight(mirror, "/") + "/" + name
		query := "?source=" + url.QueryEscape(zigMirrorSource)
		sources = append(sources, zigSource{tarballURL: base + query, signatureURL: base + ".minisig" + query})
	}

	official := fmt.Sprintf("%s/%s/%s", strings.TrimRight(zigDownloadBaseURL, "/"), zigVersion, name)
	return append(sources, zigSource{tarballURL: official, signatureURL: official + ".minisig"})
}

// fetchZigMirrors returns the community mirrors, the embedded list when it cannot be fetched.
func fetchZigMirrors(ctx context.Context) []string {
	mirrors, err := downloadZigMirrorList(ctx)
	if err != nil || len(mirrors) == 0 {
		zlog.Debug("using the embedded zig mirror list", zap.Error(err))
		if err == nil {
			err = errors.New("list is empty")
		}
		fmt.Printf("Could not fetch the zig community mirror list (%s), using the embedded one\n", err)
		return append([]string(nil), zigFallbackMirrors...)
	}

	return mirrors
}

func downloadZigMirrorList(ctx context.Context) ([]string, error) {
	var body bytes.Buffer
	if err := fetchZig(ctx, zigCommunityMirrorsURL, &body, 1<<20); err != nil {
		return nil, err
	}

	var mirrors []string
	scanner := bufio.NewScanner(&body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "https://") && !strings.HasPrefix(line, "http://") {
			continue
		}
		mirrors = append(mirrors, line)
	}

	return mirrors, scanner.Err()
}

// downloadZigTarball downloads tarball name into archive from the first source that serves a
// tarball matching expectedSHA256 and a valid minisign signature of the zig project, mirrors
// being untrusted. archive is left holding the verified tarball, positioned at its start.
func downloadZigTarball(ctx context.Context, name string, archive *os.File, expectedSHA256 string) error {
	ctx, cancel := context.WithTimeoutCause(ctx, zigDownloadDeadline, fmt.Errorf("download deadline of %s exceeded", zigDownloadDeadline))
	defer cancel()

	sources := zigSources(ctx, name)

	var failures []error
	for i, source := range sources {
		fmt.Printf("Downloading %s (source %d/%d)\n", source.tarballURL, i+1, len(sources))

		err := downloadZigFrom(ctx, source, archive, expectedSHA256)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("giving up after %d of %d zig sources: %w", i+1, len(sources), errors.Join(append(failures, fmt.Errorf("%s: %w", source.tarballURL, err), context.Cause(ctx))...))
		}

		fmt.Printf("Download from %s failed: %s\n", source.tarballURL, err)
		failures = append(failures, fmt.Errorf("%s: %w", source.tarballURL, err))
	}

	return fmt.Errorf("all %d zig sources failed: %w", len(sources), errors.Join(failures...))
}

func downloadZigFrom(ctx context.Context, source zigSource, archive *os.File, expectedSHA256 string) error {
	if err := archive.Truncate(0); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}

	hash := sha256.New()
	if err := fetchZig(ctx, source.tarballURL, io.MultiWriter(archive, hash), -1); err != nil {
		return err
	}

	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedSHA256 {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actual)
	}

	var signature bytes.Buffer
	if err := fetchZig(ctx, source.signatureURL, &signature, 1<<16); err != nil {
		return fmt.Errorf("signature: %w", err)
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	content, err := io.ReadAll(archive)
	if err != nil {
		return err
	}

	if err := verifyZigSignature(content, signature.String()); err != nil {
		return err
	}

	_, err = archive.Seek(0, io.SeekStart)
	return err
}

func verifyZigSignature(content []byte, signatureText string) error {
	publicKey, err := minisign.NewPublicKey(zigMinisignPublicKey)
	if err != nil {
		return fmt.Errorf("decoding zig public key: %w", err)
	}

	signature, err := minisign.DecodeSignature(signatureText)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}

	if _, err := publicKey.Verify(content, signature); err != nil {
		return fmt.Errorf("verifying signature: %w", err)
	}

	return nil
}

// fetchZig GETs url into to, giving up when connecting takes longer than [zigConnectTimeout]
// or when no byte arrived for [zigStallTimeout]. A positive limit caps the body size.
func fetchZig(ctx context.Context, url string, to io.Writer, limit int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stalled := time.AfterFunc(zigStallTimeout, cancel)
	defer stalled.Stop()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	dialer := &net.Dialer{Timeout: zigConnectTimeout}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: zigConnectTimeout,
	}
	defer transport.CloseIdleConnections()

	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return stallError(ctx, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.New(response.Status)
	}

	var body io.Reader = &stallReader{reader: response.Body, timer: stalled}
	if limit > 0 {
		body = io.LimitReader(body, limit)
	}

	if _, err := io.Copy(to, body); err != nil {
		return stallError(ctx, err)
	}

	return nil
}

func stallError(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return fmt.Errorf("no data received for %s: %w", zigStallTimeout, err)
	}
	return err
}

// stallReader restarts the stall timer every time bytes are read.
type stallReader struct {
	reader io.Reader
	timer  *time.Timer
}

func (r *stallReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.timer.Reset(zigStallTimeout)
	}
	return n, err
}
