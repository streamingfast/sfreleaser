package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// nativeArchive is one release archive produced from a native build.
type nativeArchive struct {
	Target nativeTarget
	// Name is the archive's file name, '<project>_<os>_<arch>.tar.gz'.
	Name   string
	Path   string
	SHA256 string
}

// nativeArchiveName is the name Goreleaser gave the archive of a target, which the Homebrew
// formulas and download scripts depend on.
func nativeArchiveName(project string, target nativeTarget) string {
	arch := strings.ToLower(target.GOARCH)
	switch target.GOARCH {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i386"
	}

	return fmt.Sprintf("%s_%s_%s.tar.gz", project, strings.ToLower(target.GOOS), arch)
}

type nativePackageOptions struct {
	Project string
	Binary  string
	// Binaries maps a target id to the path of its binary, as returned by [nativeBuild].
	Binaries map[string]string
	// Files are added next to the binary at the root of every archive, under their path
	// relative to the project root: the README and LICENSE.
	Files []string
	// ModTime is the modification time of every archived file, so that the archives only
	// change when their content does.
	ModTime   time.Time
	OutputDir string
}

// packageNativeArchives writes one '.tar.gz' archive per built target, holding the binary and
// options.Files at its root, the layout Goreleaser produced.
func packageNativeArchives(options nativePackageOptions) ([]nativeArchive, error) {
	var archives []nativeArchive
	for _, target := range nativeTargets {
		binary, found := options.Binaries[target.ID]
		if !found {
			continue
		}

		archive := nativeArchive{
			Target: target,
			Name:   nativeArchiveName(options.Project, target),
		}
		archive.Path = filepath.Join(options.OutputDir, archive.Name)

		// Goreleaser's order: the files sorted, then the binary.
		files := append([]string(nil), options.Files...)
		sort.Strings(files)

		var entries []archiveEntry
		for _, file := range files {
			entries = append(entries, archiveEntry{source: file, name: filepath.ToSlash(file)})
		}
		entries = append(entries, archiveEntry{source: binary, name: options.Binary, mode: 0o755})

		sum, err := writeTarGz(archive.Path, entries, options.ModTime)
		if err != nil {
			return nil, fmt.Errorf("packaging %s: %w", archive.Name, err)
		}

		archive.SHA256 = sum
		archives = append(archives, archive)
	}

	return archives, nil
}

type archiveEntry struct {
	source string
	name   string
	// mode is the archived permission bits, the source file's when zero.
	mode int64
}

// writeTarGz writes the entries to a gzipped tar archive at path and returns its SHA256.
func writeTarGz(path string, entries []archiveEntry, modTime time.Time) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	compressed := gzip.NewWriter(io.MultiWriter(file, hash))
	writer := tar.NewWriter(compressed)

	for _, entry := range entries {
		if err := addTarEntry(writer, entry, modTime); err != nil {
			return "", fmt.Errorf("adding %q: %w", entry.source, err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", err
	}
	if err := compressed.Close(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func addTarEntry(writer *tar.Writer, entry archiveEntry, modTime time.Time) error {
	source, err := os.Open(entry.source)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}

	mode := entry.mode
	if mode == 0 {
		mode = int64(info.Mode().Perm())
	}

	if err := writer.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     entry.name,
		Size:     info.Size(),
		Mode:     mode,
		ModTime:  modTime,
		Format:   tar.FormatPAX,
	}); err != nil {
		return err
	}

	_, err = io.Copy(writer, source)
	return err
}

// writeChecksums writes 'checksums.txt' in Goreleaser's format: '<sha256>  <archive name>'
// lines sorted by name.
func writeChecksums(path string, archives []nativeArchive) error {
	sorted := append([]nativeArchive(nil), archives...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var content strings.Builder
	for _, archive := range sorted {
		fmt.Fprintf(&content, "%s  %s\n", archive.SHA256, archive.Name)
	}

	return os.WriteFile(path, []byte(content.String()), 0o644)
}

// nativeArchiveModTime is the commit time of HEAD, so that rebuilding the same commit produces
// the same archives.
func nativeArchiveModTime() time.Time {
	if output, _, err := maybeResultOf("git log -1 --format=%ct"); err == nil {
		var seconds int64
		if _, err := fmt.Sscan(strings.TrimSpace(output), &seconds); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	}

	return time.Now().UTC()
}
