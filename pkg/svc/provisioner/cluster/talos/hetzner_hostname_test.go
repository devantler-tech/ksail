// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talosprovisioner_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// nodeRuntimeMode is a minimal config.validation RuntimeMode that reports the
// metal platform (an installation is required, not in a container). It mirrors
// the mode a Hetzner node booted from the public Talos ISO validates a config
// under, exercising the same container-level conflict validation that rejects a
// HostnameConfig document coexisting with a static machine.network.hostname.
type nodeRuntimeMode struct{}

func (nodeRuntimeMode) String() string        { return "metal" }
func (nodeRuntimeMode) RequiresInstall() bool { return true }
func (nodeRuntimeMode) InContainer() bool     { return false }

// countHostnameConfigDocs returns the number of standalone HostnameConfig
// documents in a (possibly multi-document) Talos config YAML.
func countHostnameConfigDocs(t *testing.T, cfgBytes []byte) int {
	t.Helper()

	decoder := yaml.NewDecoder(bytes.NewReader(cfgBytes))
	count := 0

	for {
		var doc map[string]any

		decodeErr := decoder.Decode(&doc)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		require.NoError(t, decodeErr)

		if kind, _ := doc["kind"].(string); kind == "HostnameConfig" {
			count++
		}
	}

	return count
}

// machineConfigView is a minimal view over a Talos MachineConfig document, used
// to assert on the fields the hostname patch must set and preserve without
// depending on the full machinery config interface surface.
type machineConfigView struct {
	Machine struct {
		Type    string `yaml:"type"`
		Network struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"network"`
	} `yaml:"machine"`
	Cluster struct {
		CA struct {
			Crt string `yaml:"crt"`
		} `yaml:"ca"`
	} `yaml:"cluster"`
}

// workerConfigBytes builds a real Talos worker machine config for a cluster.
func workerConfigBytes(t *testing.T) []byte {
	t.Helper()

	manager := talosconfigmanager.NewConfigManager("", "prod", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	workerBytes, err := configs.Worker().Bytes()
	require.NoError(t, err)

	return workerBytes
}

// firstMachineConfigDoc parses the MachineConfig document out of a (possibly
// multi-document) Talos config YAML. The MachineConfig is the document that
// carries machine.type, so it is selected by the presence of that field.
func firstMachineConfigDoc(t *testing.T, cfgBytes []byte) machineConfigView {
	t.Helper()

	decoder := yaml.NewDecoder(bytes.NewReader(cfgBytes))

	for {
		var doc machineConfigView

		err := decoder.Decode(&doc)
		if err != nil {
			break
		}

		if doc.Machine.Type != "" {
			return doc
		}
	}

	t.Fatalf("no MachineConfig document found in config")

	return machineConfigView{}
}

// TestPatchTalosHostname_SetsHostname verifies that PatchTalosHostname overlays
// machine.network.hostname onto a real Talos worker config. This is the fix for
// issue #4962: scaled-up Hetzner nodes booted from the public ISO have no cloud
// metadata to derive their hostname from, so the hostname must be set explicitly
// to the Hetzner server name for the Hetzner CCM to match the Node to the server.
func TestPatchTalosHostname_SetsHostname(t *testing.T) {
	t.Parallel()

	workerBytes := workerConfigBytes(t)

	patched, err := talosprovisioner.PatchTalosHostname(workerBytes, "prod-worker-4")
	require.NoError(t, err)

	doc := firstMachineConfigDoc(t, patched)
	assert.Equal(t, "prod-worker-4", doc.Machine.Network.Hostname,
		"hostname must match the Hetzner server name so the CCM can match the Node")
}

// TestPatchTalosHostname_PreservesConfig verifies that patching the hostname does
// not drop other machine config — the node still carries the cluster CA and
// remains a worker, so it can still join the existing cluster.
func TestPatchTalosHostname_PreservesConfig(t *testing.T) {
	t.Parallel()

	workerBytes := workerConfigBytes(t)
	before := firstMachineConfigDoc(t, workerBytes)
	require.NotEmpty(t, before.Cluster.CA.Crt, "precondition: worker config carries a cluster CA")

	patched, err := talosprovisioner.PatchTalosHostname(workerBytes, "prod-worker-4")
	require.NoError(t, err)

	after := firstMachineConfigDoc(t, patched)
	assert.Equal(t, "worker", after.Machine.Type, "patched worker config must remain a worker")
	assert.Equal(t, before.Cluster.CA.Crt, after.Cluster.CA.Crt,
		"patching the hostname must not regenerate or drop the cluster CA")
}

// TestPatchTalosHostname_InvalidConfigErrors verifies that PatchTalosHostname
// returns an error (rather than producing a node with the wrong identity) when
// given bytes that cannot be parsed as a Talos machine config. A lone "{" is an
// unterminated YAML flow mapping, which is a hard syntax error.
func TestPatchTalosHostname_InvalidConfigErrors(t *testing.T) {
	t.Parallel()

	_, err := talosprovisioner.PatchTalosHostname([]byte("{"), "prod-worker-4")
	require.Error(t, err)
}

// TestPatchTalosHostname_NodeAccepts verifies the patched config passes the same
// container-level validation the Talos node runs on ApplyConfiguration. Talos
// generated configs carry a standalone HostnameConfig document (auto: stable);
// once machine.network.hostname is set it conflicts with that document ("static
// hostname is already set in v1alpha1 config"), which is the regression in #4969
// that left scaled-up Hetzner workers unable to apply their config and join.
// The patch must remove the conflicting document so exactly one hostname
// representation remains and the node accepts the config.
func TestPatchTalosHostname_NodeAccepts(t *testing.T) {
	t.Parallel()

	workerBytes := workerConfigBytes(t)
	require.Positive(t, countHostnameConfigDocs(t, workerBytes),
		"precondition: generated worker config carries a standalone HostnameConfig document")

	patched, err := talosprovisioner.PatchTalosHostname(workerBytes, "prod-worker-4")
	require.NoError(t, err)

	assert.Zero(
		t,
		countHostnameConfigDocs(t, patched),
		"the conflicting HostnameConfig document must be removed so only one hostname representation remains",
	)

	provider, err := configloader.NewFromBytes(patched)
	require.NoError(t, err, "patched config must load")

	_, err = provider.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(
		t,
		err,
		"node-side validation must accept the patched config (no static-vs-HostnameConfig conflict)",
	)

	doc := firstMachineConfigDoc(t, patched)
	assert.Equal(t, "prod-worker-4", doc.Machine.Network.Hostname,
		"hostname must be set to the Hetzner server name so the CCM can match the Node")
}

// TestPatchTalosHostname_Idempotent verifies that re-patching a config that
// already has machine.network.hostname set still yields a node-acceptable config
// with the hostname set once. This is the scale-up and rolling-recreate case:
// applyConfigToNode is the shared chokepoint, and its base config (rebuilt from
// the running cluster's PKI) can already carry the static hostname, so a
// non-idempotent patch would reintroduce the #4969 conflict.
func TestPatchTalosHostname_Idempotent(t *testing.T) {
	t.Parallel()

	workerBytes := workerConfigBytes(t)

	once, err := talosprovisioner.PatchTalosHostname(workerBytes, "prod-worker-4")
	require.NoError(t, err)

	twice, err := talosprovisioner.PatchTalosHostname(once, "prod-worker-4")
	require.NoError(t, err)

	assert.Zero(t, countHostnameConfigDocs(t, twice),
		"re-patching must not reintroduce a HostnameConfig document")

	provider, err := configloader.NewFromBytes(twice)
	require.NoError(t, err, "re-patched config must load")

	_, err = provider.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(t, err, "re-patched config must remain node-acceptable")

	doc := firstMachineConfigDoc(t, twice)
	assert.Equal(t, "prod-worker-4", doc.Machine.Network.Hostname,
		"hostname must remain the server name after re-patching")
}

// TestHetznerNodeName_WithinLimit verifies a normal node name is formatted and
// accepted.
func TestHetznerNodeName_WithinLimit(t *testing.T) {
	t.Parallel()

	name, err := talosprovisioner.HetznerNodeName("prod", "worker", 4)
	require.NoError(t, err)
	assert.Equal(t, "prod-worker-4", name)
}

// TestHetznerNodeName_ExceedsLimit verifies that a cluster name which is itself
// valid (<= 63 chars) but whose "-<role>-<index>" suffix pushes the node name
// past the 63-character DNS-1123 label limit is rejected before provisioning —
// the case the Hetzner CCM name-matching depends on (issue #4962 review).
func TestHetznerNodeName_ExceedsLimit(t *testing.T) {
	t.Parallel()

	// A maximally-long but currently-valid cluster name (63 chars). Appending
	// "-worker-1" (9 chars) yields 72 > 63.
	clusterName := strings.Repeat("a", talosprovisioner.MaxNodeNameLength)

	name, err := talosprovisioner.HetznerNodeName(clusterName, "worker", 1)
	require.ErrorIs(t, err, talosprovisioner.ErrNodeNameTooLong)
	assert.Contains(t, name, clusterName,
		"formatted name is still returned so callers can use it in diagnostics")
}

// TestUserHostnameConfigSummary verifies that the origin-aware detector flags a
// user-authored HostnameConfig (auto: off or a static hostname) — so the override
// can be warned about, not silently discarded — while treating the Talos SDK
// default (auto: stable) and an absent HostnameConfig as nothing to warn about.
func TestUserHostnameConfigSummary(t *testing.T) {
	t.Parallel()

	machineDoc := "apiVersion: v1alpha1\nkind: HostnameConfig\n"

	tests := []struct {
		name string
		cfg  string
		want string
	}{
		{
			name: "auto off is user intent",
			cfg:  machineDoc + "auto: \"off\"\n",
			want: "auto: off",
		},
		{
			name: "static hostname is user intent",
			cfg:  "apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: my-node\n",
			want: "hostname: my-node",
		},
		{
			name: "auto stable is the SDK default (not flagged)",
			cfg:  machineDoc + "auto: stable\n",
			want: "",
		},
		{
			name: "no HostnameConfig document (not flagged)",
			cfg:  "apiVersion: v1alpha1\nkind: SideroLinkConfig\napiUrl: https://example\n",
			want: "",
		},
		{
			name: "HostnameConfig after a MachineConfig document is still detected",
			cfg: "version: v1alpha1\nmachine:\n  type: worker\n---\n" +
				machineDoc + "auto: \"off\"\n",
			want: "auto: off",
		},
		{
			name: "static hostname after a MachineConfig document is detected",
			cfg: "version: v1alpha1\nmachine:\n  type: worker\n---\n" +
				"apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: my-custom-hostname\n",
			want: "hostname: my-custom-hostname",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.UserHostnameConfigSummary([]byte(testCase.cfg))
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestUserHostnameConfigSummary_SDKDefaultGeneratedConfig verifies that a freshly
// generated Talos worker config — which carries the SDK default HostnameConfig
// (auto: stable) — is NOT flagged, so KSail does not warn on the common case where
// the user supplied no hostname strategy of their own.
func TestUserHostnameConfigSummary_SDKDefaultGeneratedConfig(t *testing.T) {
	t.Parallel()

	workerBytes := workerConfigBytes(t)
	require.Positive(t, countHostnameConfigDocs(t, workerBytes),
		"precondition: generated config carries the SDK default HostnameConfig document")

	assert.Empty(t, talosprovisioner.UserHostnameConfigSummary(workerBytes),
		"the SDK default (auto: stable) must not be flagged as user intent")
}

// TestWarnIfOverridingUserHostname_EmitsForUserConfig verifies the user-facing
// behavior of this change: when a user-authored HostnameConfig is present, KSail
// logs a visible warning that it is overriding it (rather than discarding silently).
func TestWarnIfOverridingUserHostname_EmitsForUserConfig(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&buf)
	prov.WarnIfOverridingUserHostnameForTest(
		[]byte("apiVersion: v1alpha1\nkind: HostnameConfig\nauto: \"off\"\n"),
	)

	assert.Contains(t, buf.String(), "Overriding user HostnameConfig (auto: off)",
		"a user HostnameConfig override must be surfaced, not silent")
}

// TestWarnIfOverridingUserHostname_SilentForSDKDefault verifies the warning is a
// no-op for the SDK default (auto: stable) — KSail strips that silently because it
// is exactly the setting that renames nodes, not a user hostname strategy.
func TestWarnIfOverridingUserHostname_SilentForSDKDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&buf)
	prov.WarnIfOverridingUserHostnameForTest(workerConfigBytes(t))

	assert.Empty(t, buf.String(),
		"the SDK default HostnameConfig must be stripped silently (no warning)")
}
