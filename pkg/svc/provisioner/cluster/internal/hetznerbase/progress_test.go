package hetznerbase_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask"
	"github.com/devantler-tech/ksail/v7/pkg/addressmask/addressmasktest"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPublicEndpoint is a documentation-range address (RFC 5737), which the
// address mask treats exactly like a public server address.
const testPublicEndpoint = "203.0.113.10"

func writeUpLine(t *testing.T, show string) string {
	t.Helper()
	t.Setenv(addressmask.ShowAddressesEnvVar, show)

	var output bytes.Buffer

	base := newBase(&fakeInfra{}, v1alpha1.OptionsHetzner{})
	base.LogWriter = &output

	_, err := fmt.Fprintf(
		base.ProgressForTest(),
		"Cluster %q is up at %s; kubeconfig merged into %q\n",
		"prod", "https://"+testPublicEndpoint+":6443", "/home/operator/.kube/config",
	)
	require.NoError(t, err)

	return output.String()
}

func TestProgressOutputNamesNoEndpointAddress(t *testing.T) {
	got := writeUpLine(t, "")

	assert.Empty(t, addressmasktest.PublicAddressesIn(got))
	assert.Equal(t,
		`Cluster "prod" is up at https://`+addressmask.HiddenLabel+
			`:6443; kubeconfig merged into "/home/operator/.kube/config"`+"\n",
		got,
	)
}

func TestProgressOutputShowsTheEndpointWhenOptedIn(t *testing.T) {
	assert.Contains(t, writeUpLine(t, "true"), testPublicEndpoint)
}

func TestProgressOutputWithoutAWriterIsDiscarded(t *testing.T) {
	t.Parallel()

	base := newBase(&fakeInfra{}, v1alpha1.OptionsHetzner{})
	base.LogWriter = nil

	written, err := base.ProgressForTest().Write([]byte("line\n"))
	require.NoError(t, err)
	assert.Equal(t, len("line\n"), written)
}
