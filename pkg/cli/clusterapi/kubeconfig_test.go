package clusterapi_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
)

func TestLocalServiceDoesNotExposeKubeconfigProvider(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	// The local web UI API is intentionally unauthenticated and protected only by its loopback bind.
	// Do not expose credential-bearing kubeconfig bytes through that API surface.
	_, ok := any(service).(api.KubeconfigProvider)
	assert.False(t, ok)
}
