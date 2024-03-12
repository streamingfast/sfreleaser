package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_extractVersionFromHeader(t *testing.T) {
	type args struct {
		header string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"empty", args{""}, ""},
		{"unreleased", args{"## Unreleased"}, ""},
		{"next", args{"## Next"}, ""},
		{"v1.0.0", args{"## v1.0.0"}, "v1.0.0"},
		{"v1.0.0 with square bracket", args{"## [1.0.0]"}, "v1.0.0"},
		{"4.1.5-evm-devnet-fh3.0 with dots", args{"## 4.1.5-evm-devnet-fh3.0"}, "v4.1.5-evm-devnet-fh3.0"},
		{"v4.1.5-evm-devnet-fh3.0 with dots and prefix v", args{"## v4.1.5-evm-devnet-fh3.0"}, "v4.1.5-evm-devnet-fh3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractVersionFromHeader(tt.args.header))
		})
	}
}
