package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"go.uber.org/zap"
)

// Version value, injected via go build `ldflags` at build time
var version = "dev"

var (
	devSkipGoreleaser       = os.Getenv("SFRELEASER_DEV_SKIP_GORELEASER") == "true"
	devSkipRustCargoPublish = os.Getenv("SFRELEASER_DEV_SKIP_RUST_CARGO_PUBLISH") == "true"
)

func main() {
	Run(
		"sfreleaser",
		"StreamingFast specific releaser tool for easier maintaining release process",

		ConfigureViper("SFRELEASER"),
		ConfigureReleaserConfigFile(),
		ConfigureVersion(version),

		DoctorCmd,
		ReleaseCmd,
		InstallCmd,

		Description(`
			**Important** This tool is meant for StreamingFast usage and is not a generic release tool. If
			you like it, feel free to use it but your are not our main target.

			Perform the necessary commands to perform a release of the project.
			The <version> is optional, if not provided, you'll be asked the question.

			The release is performed against GitHub, you need a valid GitHub API token
			with the necessary rights to upload release and push to repositories. It needs to
			be provided in file ~/.config/goreleaser/github_token or through an environment
			variable GITHUB_TOKEN.
		`),
		PersistentFlags(func(flags *pflag.FlagSet) {
			flags.StringP("owner", "o", "streamingfast", "The owner/organization owning the project, used to compute the GitHub repository name")
			flags.StringP("binary", "b", "", "The binary name of the project, defaults to <project> if empty (Golang compiles 'cmd/<binary>')")
			flags.StringP("language", "l", "", "The language this release is for")
			flags.String("license", "Apache-2.0", "The license used for the project")
			flags.StringP("variant", "v", "", "Defines the variant of the project")
			flags.StringP("project", "p", "", "Override default computed project name which is directory of root/working directory folder")
			flags.String("root", "", "If defined, change the working directory of the process before proceeding with the release")
		}),
	)
}

func verifyCommand(command string, onErrorText string) {
	zlog.Debug("verifying command", zap.String("command", command))

	_, err := exec.LookPath(command)
	if err != nil {
		zlog.Debug("lookup path failed", zap.Error(err), zap.String("command", command), zap.String("PATH", os.Getenv("PATH")))

		fmt.Printf("Unable to find command %q\n", command)
		fmt.Println()
		fmt.Println(onErrorText)

		cli.Exit(1)
	}
}

func verifyCommandRunSuccesfully(command string, onErrorText string) {
	verifyCommand("docker", onErrorText)

	output, _, err := maybeResultOf(command)
	if err != nil {
		zlog.Debug("command check failed", zap.String("command", command), zap.String("output", output))

		fmt.Printf("Command %q did not execute succesfully, error with %q\n", command, err.Error())
		fmt.Println()
		fmt.Println(onErrorText)

		cli.Exit(1)
	}
}

func findSfreleaserDir(workingDirectory string) string {
	zlog.Debug("trying to find .sfreleaser directory", zap.String("working_directory", workingDirectory))
	current := workingDirectory
	volumeName := filepath.VolumeName(current)

	for {
		if current == volumeName || current == "/" {
			return ""
		}

		if _, err := os.Stat(filepath.Join(current, ".sfreleaser")); err == nil {
			return current
		}

		current = filepath.Dir(current)
	}
}
