package main

import (
	"github.com/streamingfast/logging"
)

var zlogTest, _ = logging.PackageLogger("sfreleaser.test", "github.com/streamingfast/sfreleaser/cmd/sfreleaser.test")

func init() {
	logging.InstantiateLoggers()
}
