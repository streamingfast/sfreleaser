package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_checkCratesRegistryToken(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"legacy token", http.StatusNotFound, `{"errors":[{"detail":"Not Found"}]}`, ""},
		{"scoped token", http.StatusForbidden, `{"errors":[{"detail":"this token does not have the required permissions to perform this action"}]}`, ""},
		{"revoked token", http.StatusForbidden, `{"errors":[{"detail":"authentication failed"}]}`, "crates.io rejected it, it is revoked, expired or unknown"},
		{"other error", http.StatusInternalServerError, `{"errors":[{"detail":"database down"}]}`, "crates.io answered 500 Internal Server Error: database down"},
		{"no body", http.StatusBadGateway, ``, "crates.io answered 502 Bad Gateway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "cio_token", r.Header.Get("Authorization"))
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			err := checkCratesRegistryToken(context.Background(), server.URL, "cio_token")
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func Test_cratesRegistryToken(t *testing.T) {
	cargoHome := t.TempDir()
	t.Setenv("CARGO_HOME", cargoHome)
	t.Setenv("CARGO_REGISTRY_TOKEN", "")

	token, _ := cratesRegistryToken()
	assert.Equal(t, "", token)

	credentials := filepath.Join(cargoHome, "credentials.toml")
	require.NoError(t, os.WriteFile(credentials, []byte("[registry]\ntoken = \"cio_file\"\n"), 0o600))

	token, source := cratesRegistryToken()
	assert.Equal(t, "cio_file", token)
	assert.Equal(t, credentials, source)

	t.Setenv("CARGO_REGISTRY_TOKEN", "cio_env")
	token, source = cratesRegistryToken()
	assert.Equal(t, "cio_env", token)
	assert.Equal(t, "CARGO_REGISTRY_TOKEN", source)
}

func Test_mismatchedCrateVersions(t *testing.T) {
	versions, err := parseCargoMetadataVersions([]byte(`{"packages":[{"name":"substreams","version":"0.8.0"},{"name":"substreams-macro","version":"0.7.3"}]}`))
	require.NoError(t, err)

	assert.Empty(t, mismatchedCrateVersions(versions, []string{"substreams"}, "0.8.0"))
	assert.Equal(t, []string{"substreams-macro is 0.7.3", "missing (not in the workspace)"}, mismatchedCrateVersions(versions, []string{"substreams", "substreams-macro", "missing"}, "0.8.0"))
}

func Test_alreadyPublishedCrates(t *testing.T) {
	output := "warning: crate sf-demo-core@0.1.0 already exists on `kellnr` index\nwarning: crate sf-demo@0.1.0 already exists on `kellnr` index\n   Uploading sf-demo-core v0.1.0\n"

	assert.Equal(t, []string{"sf-demo-core@0.1.0", "sf-demo@0.1.0"}, alreadyPublishedCrates(output))
	assert.Empty(t, alreadyPublishedCrates("   Uploading sf-demo v0.2.0\nwarning: aborting upload due to dry run\n"))
}
