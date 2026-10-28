package main

import (
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary act as the zig cc shim, as the sfreleaser binary does, since
// darwin targets built by the tests go through [os.Executable].
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == zigCCShimCommand {
		os.Exit(runZigCCShim(os.Args[2:]))
	}

	os.Exit(m.Run())
}

func TestParseBuilder(t *testing.T) {
	for raw, expected := range map[string]Builder{
		"goreleaser-docker": BuilderGoreleaserDocker,
		"native":            BuilderNative,
		"NATIVE":            BuilderNative,
	} {
		builder, err := ParseBuilder(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, expected, builder, raw)
	}

	_, err := ParseBuilder("zig")
	assert.Error(t, err)
}

func Test_selectNativeTargets(t *testing.T) {
	ids := func(targets []nativeTarget) (out []string) {
		for _, target := range targets {
			out = append(out, target.ID)
		}
		return out
	}

	tests := []struct {
		name      string
		all       bool
		platforms []string
		expected  []string
		expectErr bool
	}{
		{name: "all", all: true, expected: []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"}},
		{name: "host by default", expected: []string{"darwin-arm64"}},
		{name: "explicit platforms", platforms: []string{"linux-arm64", "darwin-amd64"}, expected: []string{"linux-arm64", "darwin-amd64"}},
		{name: "operating system alone", platforms: []string{"linux"}, expected: []string{"linux-amd64", "linux-arm64"}},
		{name: "overlapping platforms", platforms: []string{"darwin", "darwin-arm64"}, expected: []string{"darwin-amd64", "darwin-arm64"}},
		{name: "unknown platform", platforms: []string{"windows-amd64"}, expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets, err := selectNativeTargets(tt.all, tt.platforms, "darwin", "arm64")
			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, ids(targets))
		})
	}
}

func Test_nativeTarget_env(t *testing.T) {
	toolchain := nativeToolchain{Zig: "/cache/zig", MacOSSDK: "/cache/macos sdk", Shim: "/bin/sfreleaser"}
	target := func(id string) nativeTarget {
		for _, target := range nativeTargets {
			if target.ID == id {
				return target
			}
		}
		panic(id)
	}

	env := func(id string, cgo bool) []string {
		env, err := target(id).env(cgo, toolchain)
		require.NoError(t, err)
		return env
	}

	assert.Equal(t, []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"}, env("linux-amd64", false))

	assert.Equal(t, []string{
		"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=1",
		"CC=/cache/zig cc -target aarch64-linux-gnu.2.17",
		"CXX=/cache/zig c++ -target aarch64-linux-gnu.2.17",
		"CGO_LDFLAGS=-O2 -g -lunwind",
	}, env("linux-arm64", true))

	assert.Equal(t, []string{
		"GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=1",
		"CC=/bin/sfreleaser __zig-cc-shim /cache/zig cc -target aarch64-macos.11.0 '-F/cache/macos sdk/System/Library/Frameworks' '-L/cache/macos sdk/usr/lib' -idirafter '/cache/macos sdk/usr/include'",
		"CXX=/bin/sfreleaser __zig-cc-shim /cache/zig c++ -target aarch64-macos.11.0 '-F/cache/macos sdk/System/Library/Frameworks' '-L/cache/macos sdk/usr/lib' -idirafter '/cache/macos sdk/usr/include'",
	}, env("darwin-arm64", true))

	_, err := target("linux-amd64").env(true, nativeToolchain{Zig: `/it's "quoted"/zig`})
	assert.Error(t, err)
}

func Test_quoteCompilerArg(t *testing.T) {
	for arg, expected := range map[string]string{
		"/plain/zig":          "/plain/zig",
		"/with space/zig":     "'/with space/zig'",
		"/O'Brien/cache/zig":  `"/O'Brien/cache/zig"`,
		`/a "quoted"/dir/zig`: `'/a "quoted"/dir/zig'`,
	} {
		quoted, err := quoteCompilerArg(arg)
		require.NoError(t, err, arg)
		assert.Equal(t, expected, quoted, arg)
	}
}

func Test_nativeBuildEnv(t *testing.T) {
	assert.Equal(t,
		[]string{
			"PATH=/bin", "HOME=/home/me", "GOTOOLCHAIN=auto",
			"GOFLAGS=-mod=readonly", "GOWORK=off",
			"CGO_CFLAGS=-O2 -g", "CGO_CXXFLAGS=-O2 -g", "CGO_FFLAGS=-O2 -g", "CGO_LDFLAGS=-O2 -g",
			"CGO_CPPFLAGS=-DSFRELEASER_CGO=zig-0.17.0", "PKG_CONFIG_LIBDIR=/scratch/pkgconfig", "GOTMPDIR=/scratch/go",
		},
		nativeBuildEnv([]string{"PATH=/bin", "CGO_CFLAGS=-I/opt/homebrew/include", "HOME=/home/me", "GOWORK=/src/go.work", "GOFLAGS=-tags=dev", "GOTOOLCHAIN=auto", "LIBRARY_PATH=/opt/homebrew/lib", "GOTMPDIR=/host"}, "zig-0.17.0", "/scratch"),
	)
}

// Values set with 'go env -w' live in Go's env file, which Go reads for any variable left unset:
// the build environment must override them, not only drop them from the process environment.
func Test_nativeBuildEnv_overridesGoEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env")
	require.NoError(t, os.WriteFile(envFile, []byte("GOFLAGS=-tags=leak\nCGO_CFLAGS=-DLEAK\nCGO_LDFLAGS=-lleak\nGOWORK=/leak/go.work\n"), 0o644))

	scratch := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(scratch, "go"), 0o755))

	cmd := exec.Command("go", "env", "GOFLAGS", "CGO_CFLAGS", "CGO_LDFLAGS", "GOWORK")
	cmd.Env = nativeBuildEnv(append(os.Environ(), "GOENV="+envFile), "zig-test", scratch)
	output, err := cmd.Output()
	require.NoError(t, err)

	assert.Equal(t, []string{"-mod=readonly", "-O2 -g", "-O2 -g", "off"}, strings.Split(strings.TrimSpace(string(output)), "\n"))
}

func Test_dedupeFrameworks(t *testing.T) {
	assert.Equal(t,
		[]string{"cc", "-o", "out", "-framework", "CoreFoundation", "-framework", "Security", "-lresolv"},
		dedupeFrameworks([]string{"cc", "-o", "out", "-framework", "CoreFoundation", "-framework", "Security", "-framework", "CoreFoundation", "-lresolv"}),
	)

	assert.Equal(t, []string{"cc", "-framework"}, dedupeFrameworks([]string{"cc", "-framework"}), "a trailing -framework is kept as is")

	assert.Equal(t,
		[]string{"cc", "-framework", "CoreFoundation", "-lresolv", "-framework", "Security", "-o", "out"},
		dedupeFrameworks([]string{"cc", "-Wl,-framework,CoreFoundation", "-lresolv", "-Wl,-framework", "-Wl,CoreFoundation", "-framework", "Security", "-lresolv", "-o", "out"}),
		"the -Wl forms and repeated libraries are deduped too",
	)
}

func Test_duplicateEntries(t *testing.T) {
	assert.Empty(t, duplicateEntries([]string{"a", "b"}))
	assert.Equal(t, []string{"a"}, duplicateEntries([]string{"a", "b", "a", "a"}))
}

func Test_materializeMacOSSDK(t *testing.T) {
	cacheRoot := t.TempDir()

	root, err := materializeMacOSSDK(cacheRoot)
	require.NoError(t, err)

	for _, name := range []string{
		"usr/lib/libSystem.B.tbd",
		"usr/lib/libSystem.tbd",
		"usr/lib/libresolv.9.tbd",
		"usr/lib/libresolv.tbd",
		"usr/lib/libobjc.A.tbd",
		"System/Library/Frameworks/CoreFoundation.framework/CoreFoundation.tbd",
		"System/Library/Frameworks/Security.framework/Security.tbd",
		"usr/include/mach/mach_vm.h",
	} {
		assert.FileExists(t, filepath.Join(root, name))
	}

	alias, err := os.ReadFile(filepath.Join(root, "usr/lib/libSystem.tbd"))
	require.NoError(t, err)
	target, err := os.ReadFile(filepath.Join(root, "usr/lib/libSystem.B.tbd"))
	require.NoError(t, err)
	assert.Equal(t, target, alias)

	again, err := materializeMacOSSDK(cacheRoot)
	require.NoError(t, err)
	assert.Equal(t, root, again)
}

// Test_nativeBuild_cgo builds cgo programs for every target: one with C and C++ code, macOS SDK
// headers zig lacks and the stdlib parts linking Apple frameworks, one linking unwinder
// functions without C++. It needs
// zig (found on PATH at the pinned version, or downloaded), so it only runs when
// SFRELEASER_NATIVE_INTEGRATION=true.
func Test_nativeBuild_cgo(t *testing.T) {
	if os.Getenv("SFRELEASER_NATIVE_INTEGRATION") != "true" {
		t.Skip("set SFRELEASER_NATIVE_INTEGRATION=true to build the cgo fixture with zig")
	}

	output := t.TempDir()
	t.Chdir(filepath.Join("testdata", "native", "cgo"))

	binaries, err := nativeBuild(t.Context(), nativeBuildOptions{
		Binary:    "fixture",
		Version:   "v1.2.3",
		Targets:   nativeTargets,
		OutputDir: output,
	})
	require.NoError(t, err)
	require.Len(t, binaries, len(nativeTargets))

	for _, target := range nativeTargets {
		path := binaries[target.ID]

		switch target.GOOS {
		case "linux":
			file, err := elf.Open(path)
			require.NoError(t, err, target.ID)
			assert.Equal(t, map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[target.GOARCH], file.Machine, target.ID)
			assert.Nil(t, file.Section(".debug_info"), "%s is stripped", target.ID)
			file.Close()

		case "darwin":
			file, err := macho.Open(path)
			require.NoError(t, err, target.ID)
			assert.Equal(t, map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[target.GOARCH], file.Cpu, target.ID)
			assert.Equal(t, "11.0", machoMinOS(t, file), "%s runs on macOS 11 and later", target.ID)
			file.Close()
			assert.NoError(t, checkNoDuplicateDylibs(path), target.ID)
		}
	}

	// C only, without C++, like a Go program linking a prebuilt Rust library: nothing pulls in an
	// unwinder, the builder has to.
	t.Chdir(filepath.Join("..", "unwind"))
	_, err = nativeBuild(t.Context(), nativeBuildOptions{
		Binary:    "unwind",
		Version:   "v1.2.3",
		Targets:   []nativeTarget{nativeTargetByID("linux-amd64"), nativeTargetByID("linux-arm64")},
		OutputDir: t.TempDir(),
	})
	assert.NoError(t, err)
}

// machoMinOS is the minimum macOS version of a binary, from its LC_BUILD_VERSION load command.
func machoMinOS(t *testing.T, file *macho.File) string {
	t.Helper()

	const loadCmdBuildVersion = 0x32
	for _, load := range file.Loads {
		raw := load.Raw()
		if len(raw) < 16 || file.ByteOrder.Uint32(raw) != loadCmdBuildVersion {
			continue
		}

		minOS := binary.LittleEndian.Uint32(raw[12:16])
		if file.ByteOrder != binary.LittleEndian {
			minOS = file.ByteOrder.Uint32(raw[12:16])
		}
		return strings.TrimSuffix(fmt.Sprintf("%d.%d.%d", minOS>>16, (minOS>>8)&0xff, minOS&0xff), ".0")
	}

	require.Fail(t, "no LC_BUILD_VERSION load command")
	return ""
}
