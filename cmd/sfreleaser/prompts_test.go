package main

import (
	"testing"

	"github.com/streamingfast/cli"
	"github.com/stretchr/testify/assert"
)

func TestReleaseModel_confirm(t *testing.T) {
	labels := []string{
		"View release right now?",
		"Publish release right now?",
		"Would you like to delete this existing draft release?",
	}

	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			t.Run("non-interactive answers no without prompting", func(t *testing.T) {
				release := &ReleaseModel{NonInteractive: true, promptConfirm: failingPromptConfirm(t)}
				assert.False(t, release.confirm(label))
			})

			t.Run("yes answers yes without prompting", func(t *testing.T) {
				for _, nonInteractive := range []bool{true, false} {
					release := &ReleaseModel{Yes: true, NonInteractive: nonInteractive, promptConfirm: failingPromptConfirm(t)}
					assert.True(t, release.confirm(label))
				}
			})

			t.Run("interactive forwards the user answer", func(t *testing.T) {
				for _, answer := range []bool{true, false} {
					prompted := ""
					release := &ReleaseModel{promptConfirm: func(label string, _ ...cli.PromptOption) (bool, bool) {
						prompted = label
						return answer, true
					}}

					assert.Equal(t, answer, release.confirm(label))
					assert.Equal(t, label, prompted)
				}
			})
		})
	}
}

func Test_isCI(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"", false},
		{"not-a-bool", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("CI", tt.value)
			assert.Equal(t, tt.want, isCI())
		})
	}
}

func failingPromptConfirm(t *testing.T) func(string, ...cli.PromptOption) (bool, bool) {
	return func(label string, _ ...cli.PromptOption) (bool, bool) {
		t.Fatalf("unexpected prompt %q", label)
		return false, false
	}
}
