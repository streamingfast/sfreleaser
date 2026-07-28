package main

import (
	"fmt"

	"github.com/streamingfast/cli"
)

func printRustCratesNotPublishedMessage(rust *RustReleaseModel) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	fmt.Println(dedent(`
		Since release is not published yet, we have not perform crates publishing to crates.io
		repository. Once the release is published, you will need afterward to publish the crates
		manually.

		Here the command(s) you need to perform to publish your crate(s):
	`))

	commands := rustCratesPublishCommands(rust.Crates, rust.CargoPublishArgs, rust.SinglePublishInvocation)

	fmt.Println()
	for _, command := range commands {
		fmt.Println("  ", commandString(command))
	}
	fmt.Println()

	if len(commands) > 1 {
		fmt.Println(dedent(`
			It's important to run them strictly in the order printed above, otherwise publishing will fail.
		`))
	}

	fmt.Println(dedent(`
		Also, ensure that you are on the published tag before doing the 'cargo publish' command(s), to
		be 100%% your are releasing the crates from the correct commit.
	`))
}

func releaseRustPublishCrates(rust *RustReleaseModel) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	if devSkipRustCargoPublish {
		return
	}

	for _, command := range rustCratesPublishCommands(rust.Crates, rust.CargoPublishArgs, rust.SinglePublishInvocation) {
		run(command...)
	}
}

// verifyRustCratesPublish performs a 'cargo publish --dry-run' of the configured crates, which
// packages and compiles them exactly like a real publishing would, without uploading anything.
func verifyRustCratesPublish(rust *RustReleaseModel, allowDirty bool) {
	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	extraArgs := []string{"--dry-run"}
	if allowDirty {
		extraArgs = append(extraArgs, "--allow-dirty")
	}

	for _, command := range rustCratesPublishCommands(rust.Crates, rust.CargoPublishArgs, rust.SinglePublishInvocation, extraArgs...) {
		run(command...)
	}
}

func verifyRustTools() {
	ensureCommandExist("cargo", cli.Dedent(`
		The 'cargo' utility (https://doc.rust-lang.org/cargo/) is required to publish the
		project's crates to the registry.

		Install the Rust toolchain via https://rustup.rs/ and ensure 'cargo --version' runs
		correctly in your terminal.
	`))
}
