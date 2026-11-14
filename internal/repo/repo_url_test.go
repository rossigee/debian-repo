package repo

import (
	"strings"
	"testing"
)

// The public repository URL is built from the vhost or the public host, never
// the storage endpoint. With neither, the result is the path alone.
func TestBuildRepoURL(t *testing.T) {
	const storageEndpoint = "minio.internal:9000"

	cases := []struct {
		name       string
		vhost      string
		publicHost string
		pathPrefix string
		want       string
	}{
		{
			name:       "vhost wins over public host",
			vhost:      "debs.example.com",
			publicHost: "public.example.com",
			want:       "https://debs.example.com",
		},
		{
			name:       "vhost-less repo with prefix uses its public host",
			publicHost: "debs.example.com",
			pathPrefix: "/testing",
			want:       "https://debs.example.com/testing",
		},
		{
			name:       "trailing slash on prefix is trimmed",
			publicHost: "debs.example.com",
			pathPrefix: "/testing/",
			want:       "https://debs.example.com/testing",
		},
		{
			name:       "neither vhost nor public host yields the path alone",
			pathPrefix: "/testing",
			want:       "/testing",
		},
		{
			name: "neither vhost, public host nor prefix yields empty",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildRepoURL("https", tc.vhost, tc.publicHost, tc.pathPrefix)
			if got != tc.want {
				t.Errorf("buildRepoURL = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, storageEndpoint) {
				t.Errorf("buildRepoURL leaked storage endpoint: %q", got)
			}
		})
	}
}
