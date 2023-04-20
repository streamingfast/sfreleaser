package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_newCommandInfo(t *testing.T) {
	var noEnv []string
	noArgs := []string{}

	items := func(ins ...string) []string { return ins }
	cmd := func(command string, args, env []string) *commandInfo {
		return &commandInfo{command: command, args: args, env: env}
	}

	tests := []struct {
		name   string
		inputs []string
		want   *commandInfo
	}{
		{"command", items(`bash`), cmd(`bash`, noArgs, noEnv)},

		{"env_unquoted", items(`A=1 bash`), cmd(`bash`, noArgs, items(`A=1`))},
		{"env_value_quoted", items(`A="1" bash`), cmd(`bash`, noArgs, items(`A=1`))},
		{"env_all_quoted", items(`"A=1" bash`), cmd(`bash`, noArgs, items(`A=1`))},

		{"short_flag", items(`bash -c`), cmd(`bash`, items(`-c`), noEnv)},
		{"short_flag_quoted", items(`bash '-c'`), cmd(`bash`, items(`-c`), noEnv)},
		{"short_flag_double_quoted", items(`bash "-c"`), cmd(`bash`, items(`-c`), noEnv)},

		{"long_flag", items(`bash --long`), cmd(`bash`, items(`--long`), noEnv)},
		{"long_flag_value", items(`bash --long=value`), cmd(`bash`, items(`--long=value`), noEnv)},
		{"long_flag_value_quoted", items(`bash --long='value'`), cmd(`bash`, items(`--long=value`), noEnv)},
		{"long_flag_value_double_quoted", items(`bash --long="value"`), cmd(`bash`, items(`--long=value`), noEnv)},

		{
			"complex_1",
			items(`git -c 'versionsort.suffix=-' ls-remote --exit-code --refs --sort='version:refname' --tags origin '*.*.*'`),
			cmd("git", items(`-c`, `versionsort.suffix=-`, `ls-remote`, `--exit-code`, `--refs`, `--sort=version:refname`, `--tags`, `origin`, `*.*.*`), noEnv),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newCommandInfo(tt.inputs...))
		})
	}
}
