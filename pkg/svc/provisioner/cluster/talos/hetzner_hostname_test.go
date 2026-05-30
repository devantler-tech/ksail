// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package talosprovisioner_test

import (
	"bytes"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

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
func workerConfigBytes(t *testing.T, clusterName string) []byte {
	t.Helper()

	manager := talosconfigmanager.NewConfigManager("", clusterName, "1.32.0", "10.5.0.0/24")

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

	workerBytes := workerConfigBytes(t, "prod")

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

	workerBytes := workerConfigBytes(t, "prod")
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
