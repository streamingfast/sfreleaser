package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseGitHubRemoteURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"https://github.com/streamingfast/substreams-rs.git", "streamingfast", "substreams-rs", true},
		{"https://github.com/streamingfast/substreams-rs", "streamingfast", "substreams-rs", true},
		{"https://github.com/streamingfast/substreams-rs/", "streamingfast", "substreams-rs", true},
		{"https://x-access-token:abc@github.com/streamingfast/substreams-rs.git", "streamingfast", "substreams-rs", true},
		{"git@github.com:streamingfast/substreams-rs.git", "streamingfast", "substreams-rs", true},
		{"git@github.com:streamingfast/substreams-rs", "streamingfast", "substreams-rs", true},
		{"ssh://git@github.com/streamingfast/substreams-rs.git", "streamingfast", "substreams-rs", true},
		{"https://github.com/streamingfast/firehose.core.git", "streamingfast", "firehose.core", true},
		{"https://gitlab.com/streamingfast/substreams-rs.git", "", "", false},
		{"https://github.com/streamingfast", "", "", false},
		{"/local/path/substreams-rs", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo, ok := parseGitHubRemoteURL(tt.url)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func Test_validateWorkflowFilename(t *testing.T) {
	require.NoError(t, validateWorkflowFilename("publish-release.yml"))
	require.NoError(t, validateWorkflowFilename("release.yaml"))
	require.Error(t, validateWorkflowFilename(".github/workflows/publish-release.yml"))
	require.Error(t, validateWorkflowFilename("publish-release"))
	require.Error(t, validateWorkflowFilename(""))
}

func Test_parseCargoMetadataPublishableCrates(t *testing.T) {
	metadata := `{"packages":[
		{"name":"substreams","publish":null},
		{"name":"substreams-macro","publish":null},
		{"name":"examples","publish":[]},
		{"name":"private","publish":["my-registry"]},
		{"name":"both","publish":["my-registry","crates-io"]}
	]}`

	crates, err := parseCargoMetadataPublishableCrates([]byte(metadata))
	require.NoError(t, err)
	assert.Equal(t, []string{"substreams", "substreams-macro", "both"}, crates)

	_, err = parseCargoMetadataPublishableCrates([]byte("not json"))
	require.Error(t, err)
}

func Test_trustedPublisherConfig_sameAs(t *testing.T) {
	env := "release"
	otherEnv := "Release"
	base := trustedPublisherConfig{RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "publish-release.yml"}

	assert.True(t, base.sameAs(trustedPublisherConfig{Crate: "x", RepositoryOwner: "StreamingFast", RepositoryName: "Substreams-RS", WorkflowFilename: "publish-release.yml"}))
	assert.False(t, base.sameAs(trustedPublisherConfig{RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "Publish-release.yml"}))
	assert.False(t, base.sameAs(trustedPublisherConfig{RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "publish-release.yml", Environment: &env}))

	withEnv := base
	withEnv.Environment = &env
	assert.True(t, withEnv.sameAs(trustedPublisherConfig{RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "publish-release.yml", Environment: &otherEnv}))
}

type fakeCratesIO struct {
	mu       sync.Mutex
	configs  map[string][]trustedPublisherConfig
	trusted  map[string]bool
	requests []string
	fail     map[string]fakeFailure
}

type fakeFailure struct {
	status int
	detail string
}

func newFakeCratesIO(t *testing.T) (*fakeCratesIO, *httptest.Server) {
	fake := &fakeCratesIO{configs: map[string][]trustedPublisherConfig{}, trusted: map[string]bool{}, fail: map[string]fakeFailure{}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()

		assert.Equal(t, "cio_token", r.Header.Get("Authorization"))
		assert.Contains(t, r.Header.Get("User-Agent"), "sfreleaser")

		key := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		fake.requests = append(fake.requests, key)

		if failure, found := fake.fail[key]; found {
			w.WriteHeader(failure.status)
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"detail": failure.detail}}})
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/trusted_publishing/github_configs":
			json.NewEncoder(w).Encode(map[string]any{
				"github_configs": fake.configs[r.URL.Query().Get("crate")],
				"meta":           map[string]any{"total": len(fake.configs[r.URL.Query().Get("crate")]), "next_page": nil},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/trusted_publishing/github_configs":
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			body, _ := io.ReadAll(r.Body)

			var request struct {
				GitHubConfig map[string]any `json:"github_config"`
			}
			require.NoError(t, json.Unmarshal(body, &request))
			assert.Contains(t, request.GitHubConfig, "environment", "environment is always sent, null when unset")

			var config struct {
				GitHubConfig trustedPublisherConfig `json:"github_config"`
			}
			require.NoError(t, json.Unmarshal(body, &config))
			fake.configs[config.GitHubConfig.Crate] = append(fake.configs[config.GitHubConfig.Crate], config.GitHubConfig)
			json.NewEncoder(w).Encode(map[string]any{"github_config": config.GitHubConfig})

		case r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			assert.JSONEq(t, `{"crate":{"trustpub_only":true}}`, string(body))
			fake.trusted[r.URL.Path[len("/crates/"):]] = true
			w.Write([]byte(`{"crate":{}}`))

		default:
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return fake, server
}

func Test_setupCrateTrustedPublisher(t *testing.T) {
	config := trustedPublisherConfig{RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "publish-release.yml"}

	t.Run("creates missing config", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", config, false, false))

		require.Len(t, fake.configs["substreams"], 1)
		assert.Equal(t, "substreams", fake.configs["substreams"][0].Crate)
		assert.Nil(t, fake.configs["substreams"][0].Environment)
		assert.Equal(t, []string{
			"GET /trusted_publishing/github_configs?crate=substreams",
			"POST /trusted_publishing/github_configs",
		}, fake.requests)
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", config, false, false))
		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", config, false, false))

		assert.Len(t, fake.configs["substreams"], 1)
		assert.Len(t, fake.requests, 3)
	})

	t.Run("keeps other configs and adds ours", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		fake.configs["substreams"] = []trustedPublisherConfig{{Crate: "substreams", RepositoryOwner: "streamingfast", RepositoryName: "substreams-rs", WorkflowFilename: "old.yml"}}
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", config, false, false))
		assert.Len(t, fake.configs["substreams"], 2)
	})

	t.Run("dry run changes nothing", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", config, true, true))

		assert.Empty(t, fake.configs)
		assert.Empty(t, fake.trusted)
		assert.Equal(t, []string{"GET /trusted_publishing/github_configs?crate=substreams"}, fake.requests)
	})

	t.Run("trusted publishing only", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams-macro", config, true, false))

		assert.True(t, fake.trusted["substreams-macro"])
		assert.Equal(t, "PATCH /crates/substreams-macro", fake.requests[2])
	})

	t.Run("environment is sent", func(t *testing.T) {
		fake, server := newFakeCratesIO(t)
		client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

		env := "release"
		withEnv := config
		withEnv.Environment = &env
		require.NoError(t, setupCrateTrustedPublisher(context.Background(), client, "substreams", withEnv, false, false))

		require.NotNil(t, fake.configs["substreams"][0].Environment)
		assert.Equal(t, "release", *fake.configs["substreams"][0].Environment)
	})

	errorCases := []struct {
		name    string
		key     string
		failure fakeFailure
		wantErr string
	}{
		{"not an owner", "POST /trusted_publishing/github_configs", fakeFailure{http.StatusBadRequest, "You are not an owner of this crate"}, "adding the Trusted Publisher: crates.io answered 400 Bad Request: You are not an owner of this crate"},
		{"revoked token", "GET /trusted_publishing/github_configs?crate=substreams", fakeFailure{http.StatusForbidden, "authentication failed"}, "listing its Trusted Publishers: crates.io rejected the token, it is revoked, expired or unknown"},
		{"missing scope", "GET /trusted_publishing/github_configs?crate=substreams", fakeFailure{http.StatusForbidden, "this token does not have the required permissions to perform this action"}, "listing its Trusted Publishers: the token cannot manage this crate, it needs the 'trusted-publishing' scope and this crate in its crate scopes"},
		{"unknown crate", "GET /trusted_publishing/github_configs?crate=substreams", fakeFailure{http.StatusNotFound, "crate `substreams` does not exist"}, "listing its Trusted Publishers: crates.io answered 404 Not Found: crate `substreams` does not exist"},
		{"legacy token on trustpub only", "PATCH /crates/substreams", fakeFailure{http.StatusForbidden, "legacy tokens are not allowed"}, "requiring Trusted Publishing: crates.io answered 403 Forbidden: legacy tokens are not allowed"},
		{"no error body", "POST /trusted_publishing/github_configs", fakeFailure{http.StatusBadGateway, ""}, "adding the Trusted Publisher: crates.io answered 502 Bad Gateway"},
	}

	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			fake, server := newFakeCratesIO(t)
			fake.fail[tt.key] = tt.failure
			client := &cratesIOClient{baseURL: server.URL, token: "cio_token", http: server.Client()}

			err := setupCrateTrustedPublisher(context.Background(), client, "substreams", config, true, false)
			require.EqualError(t, err, tt.wantErr)
		})
	}
}
