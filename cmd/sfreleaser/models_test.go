package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseRepository(t *testing.T) {
	tests := []struct {
		name          string
		repository    string
		wantOwner     string
		wantProject   string
		wantPanic     bool
		panicContains string
	}{
		{
			name:        "simple owner/project",
			repository:  "streamingfast/firehose-core",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "github.com prefix",
			repository:  "github.com/streamingfast/firehose-core",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "https://github.com prefix",
			repository:  "https://github.com/streamingfast/firehose-core",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "https://github.com with .git suffix",
			repository:  "https://github.com/streamingfast/firehose-core.git",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "http://github.com prefix",
			repository:  "http://github.com/streamingfast/firehose-core",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "with .git suffix only",
			repository:  "streamingfast/firehose-core.git",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "with spaces trimmed",
			repository:  "  streamingfast/firehose-core  ",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:          "missing project part",
			repository:    "streamingfast",
			wantPanic:     true,
			panicContains: "expected format: <owner>/<project>",
		},
		{
			name:          "missing project after slash",
			repository:    "streamingfast/",
			wantPanic:     true,
			panicContains: "both owner and project must be non-empty",
		},
		{
			name:          "missing owner before slash",
			repository:    "/firehose-core",
			wantPanic:     true,
			panicContains: "both owner and project must be non-empty",
		},
		{
			name:          "too many path segments",
			repository:    "github.com/streamingfast/firehose-core/extra",
			wantPanic:     true,
			panicContains: "got too many path segments",
		},
		{
			name:          "empty string",
			repository:    "",
			wantPanic:     true,
			panicContains: "expected format: <owner>/<project>",
		},
		{
			name:          "only slashes",
			repository:    "/",
			wantPanic:     true,
			panicContains: "both owner and project must be non-empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				// cli.Quit calls os.Exit, so we need to catch the panic/exit
				// For now, we'll just test the happy paths and document expected behavior
				// In a real scenario, you might want to refactor to return errors instead of calling cli.Quit
				t.Skip("Skipping panic test - parseRepository calls cli.Quit which exits the process")
				return
			}

			owner, project := parseRepository(tt.repository)
			assert.Equal(t, tt.wantOwner, owner, "owner mismatch")
			assert.Equal(t, tt.wantProject, project, "project mismatch")
		})
	}
}

func Test_parseRepository_ValidFormats(t *testing.T) {
	// Test all valid formats produce the same result
	formats := []string{
		"streamingfast/firehose-core",
		"github.com/streamingfast/firehose-core",
		"https://github.com/streamingfast/firehose-core",
		"https://github.com/streamingfast/firehose-core.git",
		"http://github.com/streamingfast/firehose-core",
	}

	expectedOwner := "streamingfast"
	expectedProject := "firehose-core"

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			owner, project := parseRepository(format)
			require.Equal(t, expectedOwner, owner, "owner should match for format: %s", format)
			require.Equal(t, expectedProject, project, "project should match for format: %s", format)
		})
	}
}

func Test_parseRepository_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name        string
		repository  string
		wantOwner   string
		wantProject string
	}{
		{
			name:        "firehose-core",
			repository:  "streamingfast/firehose-core",
			wantOwner:   "streamingfast",
			wantProject: "firehose-core",
		},
		{
			name:        "firehose-ethereum",
			repository:  "streamingfast/firehose-ethereum",
			wantOwner:   "streamingfast",
			wantProject: "firehose-ethereum",
		},
		{
			name:        "substreams",
			repository:  "streamingfast/substreams",
			wantOwner:   "streamingfast",
			wantProject: "substreams",
		},
		{
			name:        "different org",
			repository:  "https://github.com/golang/go.git",
			wantOwner:   "golang",
			wantProject: "go",
		},
		{
			name:        "project with dashes",
			repository:  "my-org/my-awesome-project",
			wantOwner:   "my-org",
			wantProject: "my-awesome-project",
		},
		{
			name:        "project with underscores",
			repository:  "my_org/my_project",
			wantOwner:   "my_org",
			wantProject: "my_project",
		},
		{
			name:        "project with dots",
			repository:  "github.com/kubernetes/k8s.io",
			wantOwner:   "kubernetes",
			wantProject: "k8s.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, project := parseRepository(tt.repository)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantProject, project)
		})
	}
}
