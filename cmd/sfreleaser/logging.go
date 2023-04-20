package main

import (
	"github.com/streamingfast/cli"
	"github.com/streamingfast/logging"
)

var zlog, tracer = logging.ApplicationLogger("sfreleaser", "github.com/streamingfast/tooling/cmd/sfreleaser", logging.WithConsoleToStderr())

func init() {
	cli.SetLogger(zlog, tracer)
}
