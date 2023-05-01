package main

import "github.com/streamingfast/cli"

func releaseRustPublishCrates(rust *RustReleaseModel) {
	if devSkipRustCargoPublish {
		return
	}

	cli.Ensure(rust != nil, "Rust model should have been populated by now but it's currently nil")

	for _, crate := range rust.Crates {
		args := []string{"cargo publish"}
		args = append(args, rust.CargoPublishArgs...)
		args = append(args, "-p", crate)

		run(args...)
	}
}
