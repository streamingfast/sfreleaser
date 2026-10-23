package main

import (
	"strings"
	"testing"

	"github.com/streamingfast/cli"
	"github.com/stretchr/testify/assert"
)

func TestGitHubTokenRegex(t *testing.T) {
	tests := []struct {
		name  string
		token string
		valid bool
	}{
		{"classic personal", "ghp_" + strings.Repeat("a1B2", 9), true},
		{"fine-grained personal", "github_pat_" + strings.Repeat("A", 22) + "_" + strings.Repeat("b", 59), true},
		{"oauth", "gho_" + strings.Repeat("x", 36), true},
		{"user-to-server", "ghu_" + strings.Repeat("x", 36), true},
		{"actions installation", "ghs_" + strings.Repeat("Ab9", 12), true},
		{"actions installation longer format", "ghs_" + strings.Repeat("Ab9_", 80), true},
		{"refresh", "ghr_" + strings.Repeat("x", 76), true},
		{"legacy actions", "v1." + strings.Repeat("0a", 20), true},
		{"surrounding whitespace", "  ghs_abc123\n", true},

		{"empty", "", false},
		{"unknown prefix", "gha_abc123", false},
		{"prefix only", "ghs_", false},
		{"env file line", "GITHUB_TOKEN=ghs_abc123", false},
		{"inner whitespace", "ghs_abc 123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, githubTokenRegex.MatchString(tt.token))
		})
	}
}

func Test_shouldDeleteExistingRelease(t *testing.T) {
	answering := func(answer bool) func(string, ...cli.PromptOption) (bool, bool) {
		return func(string, ...cli.PromptOption) (bool, bool) { return answer, true }
	}

	tests := []struct {
		name   string
		state  ghReleaseState
		global func(t *testing.T) *GlobalModel
		want   bool
	}{
		{"draft, interactive, user says yes", ghReleaseDraft, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, answering(true))
			return &GlobalModel{}
		}, true},
		{"draft, interactive, user says no", ghReleaseDraft, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, answering(false))
			return &GlobalModel{}
		}, false},
		{"draft, non-interactive refuses", ghReleaseDraft, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{NonInteractive: true}
		}, false},
		{"draft, yes deletes without prompting", ghReleaseDraft, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{Yes: true}
		}, true},
		{"draft, non-interactive and yes deletes", ghReleaseDraft, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{NonInteractive: true, Yes: true}
		}, true},
		{"published, yes never deletes", ghReleaseExists, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{Yes: true}
		}, false},
		{"published, non-interactive and yes never deletes", ghReleaseExists, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{NonInteractive: true, Yes: true}
		}, false},
		{"published, interactive never prompts", ghReleaseExists, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{}
		}, false},
		{"not found never deletes", ghReleaseNotFound, func(t *testing.T) *GlobalModel {
			withPromptConfirm(t, failingPromptConfirm(t))
			return &GlobalModel{Yes: true}
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldDeleteExistingRelease(tt.state, tt.global(t)))
		})
	}
}
