package talosprovisioner_test

import (
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// talosVersionLifecycleGA is the first Talos release whose nodes implement the
// LifecycleService/ImageService upgrade APIs.
const talosVersionLifecycleGA = "v1.13.0"

// TestSupportsLifecycleUpgradeAPI verifies that the upgrade path dispatch picks
// the LifecycleService/ImageService APIs only for Talos >= 1.13 and otherwise
// falls back to the legacy MachineService.Upgrade API. The v1.12.4 → false case
// is the regression guard for the reported "unknown service machine.ImageService"
// failure when upgrading a cluster still running an older Talos release.
func TestSupportsLifecycleUpgradeAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "1.13 GA uses lifecycle API", version: talosVersionLifecycleGA, want: true},
		{name: "1.13 patch uses lifecycle API", version: "v1.13.3", want: true},
		{name: "newer minor uses lifecycle API", version: "v1.14.2", want: true},
		{name: "next major uses lifecycle API", version: "v2.0.0", want: true},
		{name: "tag without v prefix is parsed", version: "1.13.3", want: true},
		{name: "1.12 falls back to legacy (regression guard)", version: "v1.12.4", want: false},
		{name: "older 1.12 patch falls back to legacy", version: "v1.12.0", want: false},
		{name: "much older minor falls back to legacy", version: "v1.11.5", want: false},
		{name: "pre-1.13 alpha falls back to legacy", version: "v1.13.0-alpha.2", want: false},
		{name: "empty tag falls back to legacy", version: "", want: false},
		{name: "unparseable tag falls back to legacy", version: "not-a-version", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.SupportsLifecycleUpgradeAPIForTest(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}
