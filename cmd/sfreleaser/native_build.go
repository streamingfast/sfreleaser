package main

import (
	"bytes"
	"context"
	"debug/macho"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// nativeTarget is one platform the native builder produces a binary for. ID matches the
// build ids of the Goreleaser template, so '--platform' selects the same binaries with
// either builder.
type nativeTarget struct {
	ID     string
	GOOS   string
	GOARCH string
	// ZigTarget is the '-target' zig compiles cgo code for. Its version suffix is the oldest
	// system the binaries run on: glibc 2.17 for linux, macOS 11 for darwin (zig defaults to
	// the macOS it was released with otherwise).
	ZigTarget string
}

var nativeTargets = []nativeTarget{
	{ID: "darwin-amd64", GOOS: "darwin", GOARCH: "amd64", ZigTarget: "x86_64-macos.11.0"},
	{ID: "darwin-arm64", GOOS: "darwin", GOARCH: "arm64", ZigTarget: "aarch64-macos.11.0"},
	{ID: "linux-amd64", GOOS: "linux", GOARCH: "amd64", ZigTarget: "x86_64-linux-gnu.2.17"},
	{ID: "linux-arm64", GOOS: "linux", GOARCH: "arm64", ZigTarget: "aarch64-linux-gnu.2.17"},
}

// selectNativeTargets returns every target with all, the ones named by platforms ('os-arch',
// or an 'os' alone for all of its architectures) when given, and the host's otherwise.
func selectNativeTargets(all bool, platforms []string, hostOS, hostArch string) ([]nativeTarget, error) {
	if all {
		return nativeTargets, nil
	}

	if len(platforms) == 0 {
		platforms = []string{hostOS + "-" + hostArch}
	}

	var selected []nativeTarget
	picked := map[string]bool{}
	for _, platform := range platforms {
		found := false
		for _, target := range nativeTargets {
			if target.ID != platform && target.GOOS != platform {
				continue
			}

			found = true
			if !picked[target.ID] {
				picked[target.ID] = true
				selected = append(selected, target)
			}
		}

		if !found {
			return nil, fmt.Errorf("the native builder has no target %q, available ones are %s", platform, strings.Join(nativeTargetIDs(), ", "))
		}
	}

	return selected, nil
}

func nativeTargetIDs() []string {
	ids := make([]string, len(nativeTargets))
	for i, target := range nativeTargets {
		ids[i] = target.ID
	}
	return ids
}

// nativeToolchain is what cgo cross-compilation needs, resolved once per build.
type nativeToolchain struct {
	// Zig is the path of the zig binary.
	Zig string
	// ZigVersion is what 'zig version' prints.
	ZigVersion string
	// MacOSSDK is the directory holding the macOS SDK stubs, set when a darwin target is built.
	MacOSSDK string
	// Shim is the sfreleaser binary, which darwin targets go through as their C compiler, see
	// [runZigCCShim].
	Shim string
}

// env is the environment 'go build' runs with for the target, on top of the current one.
func (t nativeTarget) env(cgo bool, toolchain nativeToolchain) ([]string, error) {
	env := []string{"GOOS=" + t.GOOS, "GOARCH=" + t.GOARCH, "CGO_ENABLED=0"}
	if !cgo {
		return env, nil
	}

	env[2] = "CGO_ENABLED=1"

	compiler := []string{toolchain.Zig}
	flags := []string{"-target", t.ZigTarget}
	if t.GOOS == "darwin" {
		compiler = []string{toolchain.Shim, zigCCShimCommand, toolchain.Zig}
		// The SDK stubs are given as plain search paths rather than '--sysroot', which zig would
		// also prefix the project's own '-L' paths with, breaking prebuilt libraries such as
		// wasmtime-go's. The headers come after zig's own, only filling in the ones it lacks.
		flags = append(flags,
			"-F"+filepath.Join(toolchain.MacOSSDK, "System", "Library", "Frameworks"),
			"-L"+filepath.Join(toolchain.MacOSSDK, "usr", "lib"),
			"-idirafter", filepath.Join(toolchain.MacOSSDK, "usr", "include"),
		)
	}

	var quoted []string
	for _, arg := range append(compiler, flags...) {
		value, err := quoteCompilerArg(arg)
		if err != nil {
			return nil, err
		}
		quoted = append(quoted, value)
	}
	compilerArgs, flagArgs := quoted[:len(compiler)], quoted[len(compiler):]

	cc := strings.Join(append(append(append([]string{}, compilerArgs...), "cc"), flagArgs...), " ")
	cxx := strings.Join(append(append(append([]string{}, compilerArgs...), "c++"), flagArgs...), " ")

	env = append(env, "CC="+cc, "CXX="+cxx)
	if t.GOOS == "linux" {
		// zig links LLVM's runtime instead of GCC's libgcc, prebuilt libraries built against the
		// latter, Rust ones such as wasmtime-go's, need its unwinder for __register_frame and
		// _Unwind_Backtrace. Static, only what is used ends up in the binary.
		env = append(env, "CGO_LDFLAGS=-O2 -g -lunwind")
	}

	return env, nil
}

// quoteCompilerArg quotes a path for the CC and CXX variables, which Go splits on spaces
// unless the value is wrapped in single or double quotes, with no escaping inside them.
func quoteCompilerArg(arg string) (string, error) {
	switch {
	case !strings.ContainsAny(arg, " \t\n\r'\""):
		return arg, nil
	case !strings.Contains(arg, "'"):
		return "'" + arg + "'", nil
	case !strings.Contains(arg, `"`):
		return `"` + arg + `"`, nil
	default:
		return "", fmt.Errorf("path %q has both single and double quotes, Go cannot use it as part of a C compiler", arg)
	}
}

// nativeBuildHostEnv are host variables that never reach 'go build': Goreleaser built in a
// container where they were unset, and on a developer machine they point at host headers and
// libraries (Homebrew's, for example) or swap the module's dependencies (a parent 'go.work').
var nativeBuildHostEnv = map[string]bool{
	"CGO_CFLAGS": true, "CGO_CPPFLAGS": true, "CGO_CXXFLAGS": true, "CGO_FFLAGS": true, "CGO_LDFLAGS": true,
	"CPATH": true, "C_INCLUDE_PATH": true, "CPLUS_INCLUDE_PATH": true, "LIBRARY_PATH": true,
	"PKG_CONFIG_PATH": true, "PKG_CONFIG_LIBDIR": true,
	"CC": true, "CXX": true, "GOFLAGS": true, "GOWORK": true, "GOTMPDIR": true,
}

// nativeBuildEnv is host minus [nativeBuildHostEnv], plus explicit values for the variables
// 'go env -w' can also set: Go reads its env file for any of them left unset, an explicit value
// is the only way to override it. cgoID identifies the C toolchain in CGO_CPPFLAGS, which Go's
// build cache keys cgo objects on. scratchDir holds Go's work directories, removed by sfreleaser
// even when an interrupted go leaves them behind, and an empty 'pkgconfig' directory replacing
// the host's pkg-config search path, which Homebrew's pkg-config otherwise has built in.
func nativeBuildEnv(host []string, cgoID string, scratchDir string) []string {
	env := make([]string, 0, len(host)+8)
	for _, entry := range host {
		name, _, _ := strings.Cut(entry, "=")
		if !nativeBuildHostEnv[name] {
			env = append(env, entry)
		}
	}

	return append(env,
		"GOFLAGS=-mod=readonly",
		"GOWORK=off",
		"CGO_CFLAGS=-O2 -g",
		"CGO_CXXFLAGS=-O2 -g",
		"CGO_FFLAGS=-O2 -g",
		"CGO_LDFLAGS=-O2 -g",
		"CGO_CPPFLAGS=-DSFRELEASER_CGO="+cgoID,
		"PKG_CONFIG_LIBDIR="+filepath.Join(scratchDir, "pkgconfig"),
		"GOTMPDIR="+filepath.Join(scratchDir, "go"),
	)
}

type nativeBuildOptions struct {
	// Binary is the name of the binary, built from './cmd/<Binary>'.
	Binary string
	// Version is stamped as is in 'main.version'.
	Version     string
	CgoDisabled bool
	Targets     []nativeTarget
	// OutputDir receives one '<target id>/<Binary>' per target.
	OutputDir string
	// ZigPath overrides how zig is found, see [resolveZig].
	ZigPath string
}

// nativeBuild builds the binary of every target with 'go build', zig being the C compiler
// of cgo, and returns the path of each binary keyed by target id.
func nativeBuild(ctx context.Context, options nativeBuildOptions) (map[string]string, error) {
	toolchain := nativeToolchain{ZigVersion: "none"}
	if !options.CgoDisabled {
		var err error
		if toolchain, err = resolveNativeToolchain(ctx, options); err != nil {
			return nil, err
		}

		fmt.Printf("Compiling cgo with zig %s (%s), the first build of a target also compiles its C library, which takes a few minutes\n", toolchain.ZigVersion, toolchain.Zig)
	}

	scratchDir, err := os.MkdirTemp("", "sfreleaser-build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratchDir)
	for _, directory := range []string{"pkgconfig", "go"} {
		if err := os.Mkdir(filepath.Join(scratchDir, directory), 0o755); err != nil {
			return nil, err
		}
	}
	env := nativeBuildEnv(os.Environ(), "zig-"+toolchain.ZigVersion, scratchDir)

	parallelism := max(2, runtime.NumCPU()/2)
	semaphore := make(chan struct{}, parallelism)

	var (
		lock     sync.Mutex
		binaries = map[string]string{}
		failures []error
		wg       sync.WaitGroup
	)

	for _, target := range options.Targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			binary, err := buildNativeTarget(ctx, target, toolchain, env, options)

			lock.Lock()
			defer lock.Unlock()
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", target.ID, err))
				return
			}
			binaries[target.ID] = binary
		}()
	}

	wg.Wait()

	return binaries, errors.Join(failures...)
}

func resolveNativeToolchain(ctx context.Context, options nativeBuildOptions) (toolchain nativeToolchain, err error) {
	cacheRoot, err := sfreleaserCacheDir()
	if err != nil {
		return toolchain, err
	}

	if toolchain.Zig, err = resolveZig(ctx, options.ZigPath, cacheRoot); err != nil {
		return toolchain, err
	}

	if toolchain.ZigVersion, err = zigBinaryVersion(ctx, toolchain.Zig); err != nil {
		return toolchain, fmt.Errorf("running %q version: %w", toolchain.Zig, err)
	}

	for _, target := range options.Targets {
		if target.GOOS != "darwin" {
			continue
		}

		if toolchain.MacOSSDK, err = materializeMacOSSDK(cacheRoot); err != nil {
			return toolchain, err
		}

		if toolchain.Shim, err = os.Executable(); err != nil {
			return toolchain, fmt.Errorf("finding the sfreleaser binary: %w", err)
		}

		break
	}

	return toolchain, nil
}

func buildNativeTarget(ctx context.Context, target nativeTarget, toolchain nativeToolchain, baseEnv []string, options nativeBuildOptions) (string, error) {
	// A binary left by an earlier build must not be mistaken for this one's.
	if err := os.RemoveAll(filepath.Join(options.OutputDir, target.ID)); err != nil {
		return "", err
	}

	output := filepath.Join(options.OutputDir, target.ID, options.Binary)
	start := time.Now()

	cmd := exec.CommandContext(ctx, "go", "build",
		"-trimpath",
		"-mod=readonly",
		// zig does not strip what Go hands it when linking cgo binaries, '-extldflags=-s' asks it to.
		"-ldflags", "-s -w -extldflags=-s -X main.version="+options.Version,
		"-o", output,
		"./cmd/"+options.Binary,
	)
	env, err := target.env(!options.CgoDisabled, toolchain)
	if err != nil {
		return "", err
	}
	cmd.Env = append(append([]string{}, baseEnv...), env...)
	// On cancellation the whole process group of go is interrupted, not go alone, which would
	// exit and leave its C compilers running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 15 * time.Second

	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build failed: %w\n%s", err, strings.TrimSpace(logs.String()))
	}

	if target.GOOS == "darwin" {
		if err := checkNoDuplicateDylibs(output); err != nil {
			return "", err
		}
	}

	fmt.Printf("Built %s in %s\n", output, time.Since(start).Round(100*time.Millisecond))
	return output, nil
}

// checkNoDuplicateDylibs fails when a darwin binary links the same library twice, which
// macOS refuses to start ("dyld: duplicate linked dylib"). Go before 1.27 passes some
// '-framework' flags twice and zig, unlike Apple's linker, does not dedupe them; the
// [runZigCCShim] does, this catches anything it would miss.
func checkNoDuplicateDylibs(path string) error {
	file, err := macho.Open(path)
	if err != nil {
		return fmt.Errorf("reading Mach-O binary %q: %w", path, err)
	}
	defer file.Close()

	libraries, err := file.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("reading the libraries of %q: %w", path, err)
	}

	if duplicates := duplicateEntries(libraries); len(duplicates) > 0 {
		return fmt.Errorf("%q links %s more than once, macOS would refuse to start it", path, strings.Join(duplicates, ", "))
	}

	return nil
}

func duplicateEntries(values []string) (duplicates []string) {
	seen := map[string]int{}
	for _, value := range values {
		seen[value]++
		if seen[value] == 2 {
			duplicates = append(duplicates, value)
		}
	}
	return duplicates
}

// nativeInterrupt is the native build running, which the signal handlers of 'build' and
// 'release' stop before exiting: exiting right away would skip its cleanups and leave its
// compilers running.
var nativeInterrupt struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// runInterruptible runs f with a context that [stopInterruptible] cancels.
func runInterruptible(f func(ctx context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	nativeInterrupt.Lock()
	nativeInterrupt.cancel, nativeInterrupt.done = cancel, done
	nativeInterrupt.Unlock()

	defer func() {
		nativeInterrupt.Lock()
		nativeInterrupt.cancel, nativeInterrupt.done = nil, nil
		nativeInterrupt.Unlock()
		close(done)
	}()

	return f(ctx)
}

// stopInterruptible cancels the native build running, if any, and waits for it to clean up.
func stopInterruptible() {
	nativeInterrupt.Lock()
	cancel, done := nativeInterrupt.cancel, nativeInterrupt.done
	nativeInterrupt.Unlock()

	if cancel == nil {
		return
	}

	fmt.Println("Interrupted, stopping the build")
	cancel()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

// zigCCShimCommand is the hidden first argument that turns sfreleaser into the C compiler
// wrapper of darwin targets, see [runZigCCShim].
const zigCCShimCommand = "__zig-cc-shim"

// runZigCCShim runs zig (args[0]) with the rest of args, minus any framework or library already
// given earlier, and returns its exit code. Go before 1.27 passes '-framework CoreFoundation'
// twice when linking darwin binaries, which Apple's linker dedupes but zig does not.
func runZigCCShim(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "sfreleaser: the zig cc shim needs the zig binary as its first argument")
		return 2
	}

	cmd := exec.Command(args[0], dedupeFrameworks(args[1:])...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// -1 when zig was killed by a signal.
			if code := exitErr.ExitCode(); code > 0 {
				return code
			}
			return 1
		}

		fmt.Fprintln(os.Stderr, "sfreleaser: running zig:", err)
		return 1
	}

	return 0
}

// dedupeFrameworks drops the frameworks and libraries already linked by an earlier argument, as
// Apple's linker would: '-framework X', its '-Wl,-framework,X' and '-Wl,-framework -Wl,X'
// forms, and '-l<name>'.
func dedupeFrameworks(args []string) []string {
	seen := map[string]bool{}
	deduped := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		framework := ""
		switch {
		case args[i] == "-framework" && i+1 < len(args):
			framework = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-Wl,-framework,"):
			framework = strings.TrimPrefix(args[i], "-Wl,-framework,")
		case args[i] == "-Wl,-framework" && i+1 < len(args) && strings.HasPrefix(args[i+1], "-Wl,"):
			framework = strings.TrimPrefix(args[i+1], "-Wl,")
			i++
		case strings.HasPrefix(args[i], "-l") && len(args[i]) > 2:
			if seen[args[i]] {
				continue
			}
			seen[args[i]] = true
			deduped = append(deduped, args[i])
			continue
		default:
			deduped = append(deduped, args[i])
			continue
		}

		if seen["framework:"+framework] {
			continue
		}
		seen["framework:"+framework] = true
		deduped = append(deduped, "-framework", framework)
	}
	return deduped
}

// nativeSnapshotVersion is the version a build without one is stamped with: the latest tag,
// as Goreleaser's snapshot builds did.
func nativeSnapshotVersion() string {
	if tag, _, err := maybeResultOf("git describe --tags --abbrev=0"); err == nil && strings.TrimSpace(tag) != "" {
		return strings.TrimSpace(tag)
	}
	return "dev"
}
