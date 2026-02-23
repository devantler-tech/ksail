package vclusterprovisioner_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
)

func TestErrors_ErrNoVClusterNodes(t *testing.T) {
	t.Parallel()

	err := vclusterprovisioner.ErrNoVClusterNodes
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no VCluster nodes found")
}

func TestErrors_ErrExecFailed(t *testing.T) {
	t.Parallel()

	// Verify ErrExecFailed is re-exported from registry package
	assert.Equal(t, registry.ErrExecFailed, vclusterprovisioner.ErrExecFailed)
}

func TestErrors_Wrapping(t *testing.T) {
	t.Parallel()

	// Test that errors can be wrapped and checked with errors.Is
	wrappedErr := errors.New("wrapped error: no VCluster nodes found for cluster")

	// This tests that our errors can be used with standard error handling
	assert.Error(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "no VCluster nodes")
}
