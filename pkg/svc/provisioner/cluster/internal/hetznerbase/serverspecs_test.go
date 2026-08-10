package hetznerbase_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const specTestClusterName = "test-cluster"

// twoNodeSpecs is a control-plane-plus-worker pair mirroring what the
// provisioners' user_data composition emits: a cluster-initialising control
// plane at index 0 and a worker at index 1, each carrying its cloud-init
// document and Hetzner labels.
func twoNodeSpecs() []hetznerbase.NodeSpec {
	return []hetznerbase.NodeSpec{
		{
			Index:    0,
			NodeType: hetzner.NodeTypeControlPlane,
			UserData: "#cloud-config\n# control plane",
			Labels: hetzner.NodeLabels(
				specTestClusterName, hetzner.NodeTypeControlPlane, 0,
			),
		},
		{
			Index:    1,
			NodeType: hetzner.NodeTypeWorker,
			UserData: "#cloud-config\n# worker",
			Labels: hetzner.NodeLabels(
				specTestClusterName, hetzner.NodeTypeWorker, 1,
			),
		},
	}
}

func specTestOptions() v1alpha1.OptionsHetzner {
	return v1alpha1.OptionsHetzner{
		ControlPlaneServerType: "cx23",
		WorkerServerType:       "cx33",
		Location:               "fsn1",
	}
}

func specTestInfra() hetznerbase.ResolvedInfra {
	return hetznerbase.ResolvedInfra{
		NetworkID:        100,
		FirewallID:       200,
		PlacementGroupID: 300,
		SSHKeyID:         400,
	}
}

func TestDeriveServerSpecs(t *testing.T) {
	t.Parallel()

	nodes := twoNodeSpecs()

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, nodes, specTestOptions(), specTestInfra(),
	)

	require.NoError(t, err)
	require.Len(t, specs, len(nodes))

	// Control plane (index 0): name and labels agree; cloud-init, per-role
	// server type, the stock boot image, and infra placement carried through.
	controlPlane := specs[0]
	assert.Equal(t, "test-cluster-controlplane-0", controlPlane.Name)
	assert.Equal(t, nodes[0].UserData, controlPlane.UserData)
	assert.Equal(t, nodes[0].Labels, controlPlane.Labels)
	assert.Equal(t, "cx23", controlPlane.ServerType)
	assert.Equal(t, hetznerbase.DefaultImageName, controlPlane.ImageName)
	assert.Zero(t, controlPlane.ImageID)
	assert.Equal(t, "fsn1", controlPlane.Location)
	assert.Equal(t, int64(100), controlPlane.NetworkID)
	assert.Equal(t, int64(300), controlPlane.PlacementGroupID)
	assert.Equal(t, int64(400), controlPlane.SSHKeyID)
	assert.Equal(t, []int64{200}, controlPlane.FirewallIDs)

	// Worker (index 1): the global bootstrap index is reused for the name
	// (matching the ksail.node.index label) and the worker server type applies.
	worker := specs[1]
	assert.Equal(t, "test-cluster-worker-1", worker.Name)
	assert.Equal(t, "cx33", worker.ServerType)
	assert.Equal(t, nodes[1].UserData, worker.UserData)
	assert.Equal(t, nodes[1].Labels, worker.Labels)
	assert.Equal(t, "1", worker.Labels[hetzner.LabelNodeIndex])
}

func TestDeriveServerSpecsNoFirewall(t *testing.T) {
	t.Parallel()

	infra := specTestInfra()
	infra.FirewallID = 0

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, twoNodeSpecs(), specTestOptions(), infra,
	)

	require.NoError(t, err)
	require.NotEmpty(t, specs)
	// A zero firewall ID attaches no firewall rather than a bogus firewall 0.
	assert.Nil(t, specs[0].FirewallIDs)
}

func TestDeriveServerSpecsEmpty(t *testing.T) {
	t.Parallel()

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, []hetznerbase.NodeSpec{}, specTestOptions(), specTestInfra(),
	)

	require.NoError(t, err)
	assert.Empty(t, specs)
}

func TestDeriveServerSpecsNameTooLong(t *testing.T) {
	t.Parallel()

	// A cluster name at the 63-char cap plus the "-controlplane-0" suffix exceeds
	// the DNS-1123 label limit, so the whole derivation fails (no partial result).
	longName := strings.Repeat("a", hetzner.MaxNodeNameLength)
	nodes := []hetznerbase.NodeSpec{
		{Index: 0, NodeType: hetzner.NodeTypeControlPlane},
	}

	specs, err := hetznerbase.DeriveServerSpecs(
		longName, nodes, specTestOptions(), specTestInfra(),
	)

	require.Error(t, err)
	require.ErrorIs(t, err, hetzner.ErrNodeNameTooLong)
	assert.Nil(t, specs)
}

func TestDeriveServerSpecsRejectsPrivateKeyMaterial(t *testing.T) {
	t.Parallel()

	privateKey := "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----"

	var compressed bytes.Buffer

	zipper := gzip.NewWriter(&compressed)
	_, err := zipper.Write([]byte(privateKey))
	require.NoError(t, err)
	require.NoError(t, zipper.Close())

	// These deliberately invalid synthetic bodies exercise only the PEM
	// envelopes; no credential is present in the fixture.
	//nolint:gosec // Synthetic PEM markers test the private-key rejection boundary.
	tests := map[string]string{
		"PKCS8": privateKey,
		"RSA":   "-----BEGIN RSA PRIVATE KEY-----\nAAAA\n-----END RSA PRIVATE KEY-----",
		"EC":    "-----BEGIN EC PRIVATE KEY-----\nAAAA\n-----END EC PRIVATE KEY-----",
		"OpenSSH": "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n" +
			"-----END OPENSSH PRIVATE KEY-----",
		"encrypted": "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n" +
			"-----END ENCRYPTED PRIVATE KEY-----",
		"embedded": "printf '%s' '-----BEGIN PRIVATE KEY-----' > /tmp/cluster.key",
		"base64":   base64.StdEncoding.EncodeToString([]byte(privateKey)),
		"base64 without padding": base64.RawStdEncoding.EncodeToString(
			[]byte(privateKey),
		),
		"gzip plus base64": base64.StdEncoding.EncodeToString(compressed.Bytes()),
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nodes := twoNodeSpecs()
			nodes[1].UserData = fmt.Sprintf(`#cloud-config
write_files:
  - path: /etc/kubernetes/pki/ca.key
    content: |-
      %s
`, strings.ReplaceAll(content, "\n", "\n      "))

			specs, err := hetznerbase.DeriveServerSpecs(
				specTestClusterName, nodes, specTestOptions(), specTestInfra(),
			)

			require.ErrorIs(t, err, hetznerbase.ErrPrivateKeyInUserData)
			assert.Nil(t, specs)
		})
	}
}

func TestDeriveServerSpecsRejectsPrivateKeyInMappingKey(t *testing.T) {
	t.Parallel()

	nodes := twoNodeSpecs()
	nodes[1].UserData = `#cloud-config
? |-
  -----BEGIN PRIVATE KEY-----
  AAAA
  -----END PRIVATE KEY-----
: harmless
`

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, nodes, specTestOptions(), specTestInfra(),
	)

	require.ErrorIs(t, err, hetznerbase.ErrPrivateKeyInUserData)
	assert.Nil(t, specs)
}

func TestDeriveServerSpecsRejectsPrivateKeyInLaterYAMLDocument(t *testing.T) {
	t.Parallel()

	nodes := twoNodeSpecs()
	nodes[1].UserData = `#cloud-config
hostname: worker
---
write_files:
  - path: /etc/kubernetes/pki/ca.key
    content: |-
      -----BEGIN PRIVATE KEY-----
      AAAA
      -----END PRIVATE KEY-----
`

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, nodes, specTestOptions(), specTestInfra(),
	)

	require.ErrorIs(t, err, hetznerbase.ErrPrivateKeyInUserData)
	assert.Nil(t, specs)
}

func TestDeriveServerSpecsHandlesCyclicYAMLAlias(t *testing.T) {
	t.Parallel()

	nodes := twoNodeSpecs()
	nodes[1].UserData = `#cloud-config
loop: &loop
  again: *loop
`

	specs, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, nodes, specTestOptions(), specTestInfra(),
	)

	require.NoError(t, err)
	assert.Len(t, specs, len(nodes))
}
