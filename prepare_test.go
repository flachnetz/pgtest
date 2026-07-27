package pgtest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		version string
		system  string
		arch    string
		distro  string
		wantURL string
	}{
		{
			name:    "linux debian amd64 two-part version",
			version: "18.4",
			system:  "linux",
			arch:    "amd64",
			distro:  "debian",
			wantURL: "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v18.4.0/postgres-linux-debian-amd64.tar.gz",
		},
		{
			name:    "linux alpine arm64 three-part version",
			version: "18.4.1",
			system:  "linux",
			arch:    "arm64",
			distro:  "alpine",
			wantURL: "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v18.4.1/postgres-linux-alpine-arm64.tar.gz",
		},
		{
			name:    "darwin amd64",
			version: "18.4",
			system:  "darwin",
			arch:    "amd64",
			distro:  "debian",
			wantURL: "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v18.4.0/postgres-darwin-amd64.tar.gz",
		},
		{
			name:    "darwin arm64",
			version: "18.4",
			system:  "darwin",
			arch:    "arm64",
			distro:  "debian",
			wantURL: "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v18.4.0/postgres-darwin-arm64.tar.gz",
		},
		{
			name:    "linux debian arm64",
			version: "18.4.0",
			system:  "linux",
			arch:    "arm64",
			distro:  "debian",
			wantURL: "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v18.4.0/postgres-linux-debian-arm64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgresDownloadURL(tt.version, tt.system, tt.arch, tt.distro)
			require.Equal(t, tt.wantURL, got)
		})
	}
}

func TestPostgresDownloadURLReachable(t *testing.T) {
	tests := []struct {
		name    string
		version string
		system  string
		arch    string
		distro  string
	}{
		{"linux-debian-amd64", "18.4", "linux", "amd64", "debian"},
		{"linux-debian-arm64", "18.4", "linux", "arm64", "debian"},
		{"linux-alpine-amd64", "18.4", "linux", "amd64", "alpine"},
		{"linux-alpine-arm64", "18.4", "linux", "arm64", "alpine"},
		{"darwin-amd64", "18.4", "darwin", "amd64", ""},
		{"darwin-arm64", "18.4", "darwin", "arm64", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			url := postgresDownloadURL(tt.version, tt.system, tt.arch, tt.distro)

			resp, err := http.Head(url)
			require.NoError(t, err, "HEAD %s", url)
			resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode, "HEAD %s", url)
			require.GreaterOrEqual(t, resp.ContentLength, int64(15*1024*1024), "expected at least 15MB")
		})
	}
}
