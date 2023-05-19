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
