package clusterupdate_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
)

func TestExtractTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "image with tag",
			image: "kindest/node:v1.35.1",
			want:  "v1.35.1",
		},
		{
			name:  "image with tag and digest",
			image: "kindest/node:v1.35.1@sha256:abcdef1234567890",
			want:  "v1.35.1",
		},
		{
			name:  "image without tag",
			image: "kindest/node",
			want:  "",
		},
		{
			name:  "image with only digest",
			image: "kindest/node@sha256:abcdef1234567890",
			want:  "",
		},
		{
			name:  "registry with port and tag",
			image: "registry.example.com:5000/myimage:v2.0.0",
			want:  "v2.0.0",
		},
		{
			name:  "empty string",
			image: "",
			want:  "",
		},
		{
			name:  "k3s image with suffix",
			image: "rancher/k3s:v1.35.3-k3s1",
			want:  "v1.35.3-k3s1",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := clusterupdate.ExtractTag(testCase.image)
			assert.Equal(t, testCase.want, got)
		})
	}
}
