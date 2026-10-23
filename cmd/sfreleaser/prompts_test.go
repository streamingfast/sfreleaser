package main

import (
	"testing"

	"github.com/streamingfast/cli"
	"github.com/stretchr/testify/assert"
)

func Test_confirm(t *testing.T) {
	labels := []string{
		"View release right now?",
		"Publish release right now?",
		"Would you like to delete this existing draft release?",
	}

	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			t.Run("non-interactive answers no without prompting", func(t *testing.T) {
				withPromptConfirm(t, failingPromptConfirm(t))
				assert.False(t, confirm(&GlobalModel{NonInteractive: true}, label))
			})

			t.Run("yes answers yes without prompting", func(t *testing.T) {
				withPromptConfirm(t, failingPromptConfirm(t))
				for _, nonInteractive := range []bool{true, false} {
					assert.True(t, confirm(&GlobalModel{Yes: true, NonInteractive: nonInteractive}, label))
				}
			})

			t.Run("interactive forwards the user answer", func(t *testing.T) {
				for _, answer := range []bool{true, false} {
					prompted := ""
					withPromptConfirm(t, func(label string, _ ...cli.PromptOption) (bool, bool) {
						prompted = label
						return answer, true
					})

					assert.Equal(t, answer, confirm(&GlobalModel{}, label))
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

// withPromptConfirm replaces the prompt function for the duration of the test.
func withPromptConfirm(t *testing.T, prompt func(string, ...cli.PromptOption) (bool, bool)) {
	previous := promptConfirm
	promptConfirm = prompt
	t.Cleanup(func() { promptConfirm = previous })
}

func failingPromptConfirm(t *testing.T) func(string, ...cli.PromptOption) (bool, bool) {
	return func(label string, _ ...cli.PromptOption) (bool, bool) {
		t.Fatalf("unexpected prompt %q", label)
		return false, false
	}
}
