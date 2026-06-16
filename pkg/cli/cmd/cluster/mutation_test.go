package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
)

func TestAllDistributions(t *testing.T) {
	t.Parallel()

	dists := cluster.ExportAllDistributions()
	assert.NotEmpty(t, dists)
	assert.GreaterOrEqual(t, len(dists), 4)
}

func TestAllProviders(t *testing.T) {
	t.Parallel()

	providers := cluster.ExportAllProviders()
	assert.NotEmpty(t, providers)
	assert.GreaterOrEqual(t, len(providers), 3)
}
