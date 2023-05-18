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
		flags.BoolP("overwrite", "f", false, "[Destructive] Overwrite configuration files that already exists")
	}),
)

func doctor(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)

	zlog.Debug("starting 'sfreleaser doctor'",
		zap.Inline(global),
	)

	answer := cli.PromptSelect("What are you seeking", []string{"Troubleshoot an issue"}, cli.PromptTypeString)
	switch answer {
	case "Troubleshoot an issue":
		fmt.Println(cli.Dedent(`
			Here a list of known issues and possible solutions:

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
	default:
		panic("unreachable")
	}

	return nil
}
