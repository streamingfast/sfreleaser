package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseCargoVersion(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		expected    string
		expectedErr string
	}{
		{
			name:     "stable",
			output:   "cargo 1.96.0 (30a34c682 2026-05-25)\n",
			expected: "1.96.0",
		},
		{
			name:     "nightly",
			output:   "cargo 1.91.0-nightly (2ea99a1de 2026-01-13)\n",
			expected: "1.91.0-nightly",
		},
		{
			name:     "old enough to be sequential only",
			output:   "cargo 1.89.0 (c24e10642 2025-06-23)\n",
			expected: "1.89.0",
		},
		{
			name:        "garbage",
			output:      "command not found: cargo",
			expectedErr: `unable to extract Cargo version out of "command not found: cargo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := parseCargoVersion(tt.output)
			if tt.expectedErr != "" {
				require.EqualError(t, err, tt.expectedErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, version.String())
		})
	}
}

func Test_cargoMultiPackagePublishSupport(t *testing.T) {
	tests := []struct {
		version   string
		supported bool
	}{
		{"1.89.0", false},
		{"1.90.0", true},
		{"1.96.0", true},
		{"2.0.0", true},
		// A prerelease toolchain has the features of the version it leads to
		{"1.90.0-nightly", true},
		{"1.89.0-nightly", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			version, err := parseCargoVersion("cargo " + tt.version + " (abcdef123 2026-01-01)")
			require.NoError(t, err)

			assert.Equal(t, tt.supported, !version.Core().LessThan(cargoMultiPackagePublishMinVersion))
		})
	}
}

func Test_rustCratesPublishCommands(t *testing.T) {
	tests := []struct {
		name             string
		crates           []string
		publishArgs      []string
		singleInvocation bool
		extraArgs        []string
		expected         [][]string
	}{
		{
			name:             "single invocation, multiple crates",
			crates:           []string{"firehose-tracer", "firehose-tracer-test"},
			singleInvocation: true,
			expected: [][]string{
				{"cargo", "publish", "-p", "firehose-tracer", "-p", "firehose-tracer-test"},
			},
		},
		{
			name:             "single invocation, publish args and extra args",
			crates:           []string{"a", "b"},
			publishArgs:      []string{"--no-verify"},
			singleInvocation: true,
			extraArgs:        []string{"--dry-run", "--allow-dirty"},
			expected: [][]string{
				{"cargo", "publish", "--no-verify", "--dry-run", "--allow-dirty", "-p", "a", "-p", "b"},
			},
		},
		{
			name:             "sequential, multiple crates",
			crates:           []string{"a", "b"},
			publishArgs:      []string{"--no-verify"},
			singleInvocation: false,
			extraArgs:        []string{"--dry-run"},
			expected: [][]string{
				{"cargo", "publish", "--no-verify", "--dry-run", "-p", "a"},
				{"cargo", "publish", "--no-verify", "--dry-run", "-p", "b"},
			},
		},
		{
			name:             "no crates yields no command",
			crates:           nil,
			singleInvocation: true,
			expected:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := rustCratesPublishCommands(tt.crates, tt.publishArgs, tt.singleInvocation, tt.extraArgs...)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func Test_rustCratesPublishCommandsDoNotAliasBaseArguments(t *testing.T) {
	commands := rustCratesPublishCommands([]string{"a", "b", "c"}, []string{"--no-verify"}, false)

	assert.Equal(t, [][]string{
		{"cargo", "publish", "--no-verify", "-p", "a"},
		{"cargo", "publish", "--no-verify", "-p", "b"},
		{"cargo", "publish", "--no-verify", "-p", "c"},
	}, commands)
}

func Test_commandString(t *testing.T) {
	assert.Equal(t, "cargo publish -p a -p b", commandString([]string{"cargo", "publish", "-p", "a", "-p", "b"}))
}

func Test_validateRustCratesPublishMode(t *testing.T) {
	require.NoError(t, validateRustCratesPublishMode(RustCratesPublishModeInferred))
	require.NoError(t, validateRustCratesPublishMode(RustCratesPublishModeSequential))
	require.EqualError(t, validateRustCratesPublishMode("nope"), `invalid value "nope", accepted values are inferred, sequential`)
	require.EqualError(t, validateRustCratesPublishMode(""), `invalid value "", accepted values are inferred, sequential`)
}
