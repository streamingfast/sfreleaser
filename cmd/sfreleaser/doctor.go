package main

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var DoctorCmd = Command(doctor,
	"doctor",
	"Troubleshoot common errors and your current setup",
	Flags(func(flags *pflag.FlagSet) {
	}),
)

func doctor(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)

	zlog.Debug("starting 'sfreleaser doctor'",
		zap.Inline(global),
	)

	fmt.Println(cli.Dedent(`
		Here a list of known issues and possible solutions:

		##
		### X release failed after 0s error=yaml: unmarshal errors: line X: field ids not found in type config.Archive (or 'formats' or 'version_template')
		##

		This error indicates that you are using an outdated version of 'sfreleaser' that generates Goreleaser configuration
		files with deprecated field names. The Goreleaser v2 configuration has changed some field names:

		- 'archives.ids' is now 'archives.builds'
		- 'archives.formats' (plural) is now 'archives.format' (singular)
		- 'snapshot.version_template' is now 'snapshot.name_template'

		**Solution:** Update to the latest version of 'sfreleaser' which generates the correct configuration format.
		You can update by running:

			brew upgrade sfreleaser

		Or by downloading the latest release from GitHub and replacing your binary. After updating, the tool will
		generate configurations compatible with the latest Goreleaser versions.

		Additionally, check your project's '.sfreleaser' config file for any custom Docker image version:

			release:
				goreleaser-docker-image: <something>

		If you have an older image version specified (like 'goreleaser-cross:v1.23' or earlier), update it to
		'goreleaser/goreleaser-cross:v1.25' or later, or remove the setting entirely to use the default.

		##
		### X release failed after 0s error=only configurations files on  version: 1  are supported, yours is  version: 2 , please update your configuration
		##

		This happens when you are using a newer 'sfreleaser' version (>= v0.8.0) but that the Docker image 'goreleaser/goreleaser-cross' used for the
		building is too old. Indeed, newer versions of 'goreleaser-cross' uses Goreleaser version > 2.0 which brings a bunch of breaking changes.

		The 'sfreleaser' tool only works with Goreleaser version >= 2.0.0. First check the project's '.sfreleaser', if there is

			release:
  				goreleaser-docker-image: <something>

		Ensure the image is based on 'goreleaser-cross:v1.25' or later. If you were using a custom image,
		update it the base to use 'goreleaser/goreleaser-cross:v1.25' or later.

		If you are not using a custom image and still have the problem, you might need to re-pull the
		image, as Docker may have cached an older version. This can be done with the following command:

			docker pull --platform=linux/arm64 goreleaser/goreleaser-cross:v1.25

		> **Note**
		> Change --platform=linux/arm64 to your platform if you are not on ARM64.

		##
		### scm releases: failed to publish artifacts: could not release: POST https://api.github.com/repos/streamingfast/substreams-ethereum/releases: 422 Validation Failed [{Resource:Release Field:target_commitish Code:invalid Message:}]
		##

		It seems on fast build, there is race condition on GitHub side where the commit
		does not exists yet when we try to create the release (at least it's not visible yet
		to GitHub Releaser service).

		Push manually the commit you want to release and re-run the command. Often, I simply
		just re-run the command after the initial failure and it works right away.

		If this doesn't work, check if GitHub is having issues with their API, you can check their
		status at https://www.githubstatus.com/. Sometimes there is no downtime status but
		GitHub is slow leading to the error repeating more often.

		Wait a bit and retry later. If the issue persist, the is probably something bigger
		going on.

		##
		### Unable to copy command PTY to stdout: read /dev/ptmx: input/output error
		##

		If you have some error related to PTY, it's possible we have a problem running the command inside
		an internal PTY. That is done like this so that executed commands thinks that are in a standard
		terminal and as such, render colors and other things correctly.

		If you have such error, the best course of action is to disable PTY by setting the following
		environment variable: SFRELEASER_DISABLE_PTY=true.

		Doing so will run the command using a non-PTY terminal, rendering will be different but it
		everything should work correctly.

		##
		### homebrew tap formula: failed to publish artifacts: PUT https://api.github.com/repos/<owner>/<tap-repo>/<...>: 404 Not Found
		##

		This happens because you are trying to publish a new version of a formula and the configured
		tap owner/repo does not exists or you don't have access to (if error code is 403 for example).

		You can fix this by creating the tap repository on GitHub and making sure you have access to it.
		You can use:

		    release:
		        brew-tap-repo: <repo>

		To define the repository that is going to hold the tap formula. You can also completely disable
		brew publishing with:

		    release:
		        brew-disabled: true

		##
		### Failed to upload artifact <...> https://uploads.github.com/repos/<org>/<repo>/releases/<resource>: 307 Moved Permanently
		##

		This happens usually happens when Git 'origin' remote is misaligned with GitHub. A to
		notice if it's the case is looking at the <org>/<repo> value in the url, it usually
		not the same as GitHub.

		This happens for example when the reposistory is renamed but you did not
		update your local remote URL, it's still pointing at the old location. The release tool
		use your remote's origin to infer the release URL but the tool don't hande redirects
		correctly.

		To fix this, you need to update your remote URL to point at the new location. You can
		use the following command to do so: 'git remote set-url origin <new-url>'.
	`))

	return nil
}
