package hetznerbase

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// BootstrapMaterial is the per-cluster SSH material a composed bring-up plan
// delivers and authenticates with: a bootstrap client keypair (public half
// delivered into the nodes' authorized_keys via cloud-init, private half
// authenticating the [BringUpPlan.Signer] dial) and a pre-generated host
// identity (delivered via the cloud-init ssh_keys module and pinned by
// [BringUpPlan.HostKeyCallback]) — so the first SSH connection verifies the
// exact host key the node booted with, never trust-on-first-use and never an
// insecure ignore.
type BootstrapMaterial struct {
	// Signer is the private half of the bootstrap client keypair.
	Signer gossh.Signer
	// AuthorizedKey is the single-line authorized_keys form of the bootstrap
	// client public key, delivered via cloud-init ssh_authorized_keys.
	AuthorizedKey string
	// HostKeys is the pre-generated SSH host identity delivered via the
	// cloud-init ssh_keys module (see [cloudinitbootstrap.HostKeys]).
	HostKeys *cloudinitbootstrap.HostKeys
	// HostKeyCallback pins the delivered host identity ([gossh.FixedHostKey]).
	HostKeyCallback gossh.HostKeyCallback
}

// GenerateBootstrapMaterial mints the two fresh ed25519 keypairs a cluster's
// live bring-up needs — the bootstrap client keypair and the node's host
// identity — and packages them for plan composition. Both are ephemeral
// per-create material: the client half lives only in process memory, and the
// host identity is handed to the node at first boot.
func GenerateBootstrapMaterial() (BootstrapMaterial, error) {
	client, err := sshbootstrap.GenerateKeyPair()
	if err != nil {
		return BootstrapMaterial{}, fmt.Errorf("generate bootstrap keypair: %w", err)
	}

	host, err := sshbootstrap.GenerateKeyPair()
	if err != nil {
		return BootstrapMaterial{}, fmt.Errorf("generate host keypair: %w", err)
	}

	return BootstrapMaterial{
		Signer:        client.Signer,
		AuthorizedKey: client.AuthorizedKey,
		HostKeys: &cloudinitbootstrap.HostKeys{
			ED25519Private: string(host.PrivateKeyPEM),
			ED25519Public:  host.AuthorizedKey,
		},
		HostKeyCallback: gossh.FixedHostKey(host.Signer.PublicKey()),
	}, nil
}

// ComposePlan runs the plan composition shared by the provisioners'
// composePlan callbacks: mint the per-cluster bootstrap material, hand it to
// the distro-specific composeNodes (which threads it into every node's
// cloud-init document and returns the per-node [NodeSpec]s), derive the live
// server specs ([DeriveServerSpecs]), and assemble the [BringUpPlan] the live
// bring-up runs from. Only the user_data composition differs between distros;
// everything else lives here once.
func ComposePlan(
	clusterName string,
	opts v1alpha1.OptionsHetzner,
	infra ResolvedInfra,
	remoteKubeconfigPath string,
	composeNodes func(material BootstrapMaterial) ([]NodeSpec, error),
) (BringUpPlan, error) {
	material, err := GenerateBootstrapMaterial()
	if err != nil {
		return BringUpPlan{}, fmt.Errorf("generate bootstrap material: %w", err)
	}

	nodes, err := composeNodes(material)
	if err != nil {
		return BringUpPlan{}, err
	}

	specs, err := DeriveServerSpecs(clusterName, nodes, opts, infra)
	if err != nil {
		return BringUpPlan{}, err
	}

	return BringUpPlan{
		Specs:                specs,
		Signer:               material.Signer,
		HostKeyCallback:      material.HostKeyCallback,
		RemoteKubeconfigPath: remoteKubeconfigPath,
	}, nil
}
