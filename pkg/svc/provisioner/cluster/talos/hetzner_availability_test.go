package talosprovisioner_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAvailabilityLookup = errors.New("lookup failed")

func TestAvailabilityCheckResultContinues(t *testing.T) {
	t.Parallel()

	unavailable := fmt.Errorf(
		"%w: %q is not available in fsn1, nbg1, hel1",
		hetzner.ErrServerTypeUnavailable,
		"cx23",
	)

	t.Run("no error passes without output", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		require.NoError(
			t,
			talosprovisioner.AvailabilityCheckResultForTest(&out, "server type(s) cx23", nil),
		)
		assert.Empty(t, out.String())
	})

	t.Run("unavailable everywhere warns and continues", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		err := talosprovisioner.AvailabilityCheckResultForTest(
			&out,
			"server type(s) cx23",
			unavailable,
		)

		require.NoError(t, err)
		assert.Contains(t, out.String(), "⚠ Hetzner reports server type(s) cx23 as unavailable")
		assert.Contains(t, out.String(), "fsn1, nbg1, hel1")
	})

	t.Run("unavailable everywhere with no log writer continues", func(t *testing.T) {
		t.Parallel()

		require.NoError(
			t,
			talosprovisioner.AvailabilityCheckResultForTest(
				nil,
				"worker server type \"cx23\"",
				unavailable,
			),
		)
	})
}

func TestAvailabilityCheckResultStops(t *testing.T) {
	t.Parallel()

	t.Run("server type not found still stops", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		notFound := fmt.Errorf("%w: %q", hetzner.ErrServerTypeNotFound, "cx999")

		err := talosprovisioner.AvailabilityCheckResultForTest(
			&out,
			"server type(s) cx999",
			notFound,
		)

		require.ErrorIs(t, err, hetzner.ErrServerTypeNotFound)
		assert.Contains(t, err.Error(), "server availability check failed for server type(s) cx999")
		assert.Empty(t, out.String())
	})

	t.Run("other failures still stop", func(t *testing.T) {
		t.Parallel()

		err := talosprovisioner.AvailabilityCheckResultForTest(
			nil,
			"server type(s) cx23",
			errAvailabilityLookup,
		)

		require.ErrorIs(t, err, errAvailabilityLookup)
	})
}
