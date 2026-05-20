package image_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/stretchr/testify/assert"
)

func TestIsHelperContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role string
		want bool
	}{
		{name: "loadbalancer", role: "loadbalancer", want: true},
		{name: "noRole", role: "noRole", want: true},
		{name: "registry", role: "registry", want: true},
		{name: "master", role: "master", want: false},
		{name: "worker", role: "worker", want: false},
		{name: "agent", role: "agent", want: false},
		{name: "empty string", role: "", want: false},
		{name: "wrong case", role: "LOADBALANCER", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := image.IsHelperContainerForTest(tt.role)
			assert.Equal(t, tt.want, got)
		})
	}
}
