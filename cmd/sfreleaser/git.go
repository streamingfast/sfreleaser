package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

func ensureGitSync(global *GlobalModel) {
	state := fetchGitSyncState()

	switch state {
	case gitSyncUpToDate:

	case gitSyncNeedPull:
		if yes, _ := cli.PromptConfirm("It seems you need to 'git pull', do it now?"); yes {
			run("git pull", global.GitRemote)
		}

	case gitSyncNeedPush:
		fmt.Println("Pushing our changes to Git so it knowns about our commit(s)")
		run("git push", global.GitRemote)

	case gitSyncDiverged:
		fmt.Println("Your branch has diverged from remote, cannot continue")
		run("git status")
		cli.Quit("")
	}
}

type gitSyncState string

var (
	gitSyncUpToDate gitSyncState = "up-to-date"
	gitSyncNeedPull gitSyncState = "need-pull"
	gitSyncNeedPush gitSyncState = "need-push"
	gitSyncDiverged gitSyncState = "diverged"
)

// See https://stackoverflow.com/a/3278427/697930
func fetchGitSyncState() gitSyncState {
	upstream := "'@{u}'"
	local := resultOf("git rev-parse @")

	remote, info, err := maybeResultOf("git rev-parse", upstream)
	if err != nil {
		if strings.Contains(remote, "no upstream configured") {
			remote = ""
		} else {
			cli.NoError(err, "Command %q failed with %q", info, remote)
		}
	}

	base, info, err := maybeResultOf("git merge-base @", upstream)
	if strings.Contains(base, "no upstream configured") {
		base = ""
	} else {
		cli.NoError(err, "Command %q failed with %q", info, base)
	}

	if local == remote {
		return gitSyncUpToDate
	}

	if local == base {
		return gitSyncNeedPull
	}

	if remote == base {
		return gitSyncNeedPush
	}

	return gitSyncDiverged
}

func ensureGitNotDirty() {
	if isGitDirty() {
		fmt.Println("Your git repository is dirty, refusing to release (use --allow-dirty to continue even if Git is dirty)")
		run("git status")
		cli.Exit(1)
	}
}

func isGitDirty() bool {
	return resultOf("git status --porcelain") != ""
}

var remoteTagRegex = regexp.MustCompile(`refs/tags/(v?[0-9]+\.[0-9]+\.[0-9]+[^\s]*)`)

func latestTag(remote string) (latestTag string) {
	defer func() {
		zlog.Debug("latest tag", zap.String("found", latestTag))
	}()

	// We use `maybeResultOf` but ignore error so no error is printed
	output, _, _ := maybeResultOf("git -c 'versionsort.suffix=-' ls-remote --exit-code --refs --sort='version:refname' --tags", remote, "'*.*.*'")

	lines := getLines(output)
	if len(lines) == 0 {
		return ""
	}

	lastLine := lines[len(lines)-1]
	groups := remoteTagRegex.FindStringSubmatch(lastLine)
	cli.Ensure(len(groups) > 1, "Unable to extract tag regex from line %q", lastLine)

	return groups[1]
}
