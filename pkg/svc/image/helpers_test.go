package image_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/stretchr/testify/assert"
)

func TestIsHelperContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role string
		want bool
	}{
		{role: "loadbalancer", want: true},
		{role: "noRole", want: true},
		{role: "registry", want: true},
		{role: "master", want: false},
		{role: "worker", want: false},
		{role: "agent", want: false},
		{role: "", want: false},
		{role: "LOADBALANCER", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			t.Parallel()

			got := image.IsHelperContainerForTest(tt.role)
			assert.Equal(t, tt.want, got)
		})
	}
}
