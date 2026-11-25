package talosprovisioner

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
)

// RenderPlaceholderHcloudToken stands in for the Hetzner Cloud API token in an
// offline render. create embeds the real token in the control-plane VIP block
// when the floating IP is enabled; a render needs no credentials and must never
// write one to disk, so it embeds this placeholder instead.
//
//nolint:gosec // a placeholder that replaces the credential, not a credential.
const RenderPlaceholderHcloudToken = "ksail-render-placeholder-hcloud-token"

// ErrRenderProviderUnsupported reports a provider whose machine configuration
// the offline render cannot reproduce yet.
var ErrRenderProviderUnsupported = errors.New(
	"offline Talos render supports only the Hetzner provider",
)

// RenderOptions carries the values cluster create learns at runtime from the
// cloud. An offline render supplies them instead (or leaves them empty).
type RenderOptions struct {
	// EndpointIP is the control-plane endpoint create renders: the first
	// control-plane node's address, or the floating IP when it is enabled.
	// Empty keeps the endpoint KSail derives from the network CIDR.
	EndpointIP string
	// CertSANs is the certificate SAN set create renders with the floating IP
	// (the floating IP plus every control-plane node address). Empty adds none.
	CertSANs []string
	// Secrets is the PKI bundle to render with. Nil keeps the freshly generated
	// bundle, exactly as a first create does.
	Secrets *secrets.Bundle
	// HcloudToken is embedded in the VIP block when the floating IP is enabled.
	// Empty uses RenderPlaceholderHcloudToken.
	HcloudToken string
}

// RenderedMachineConfigs holds the machine configuration bytes create applies
// to the first control-plane node and the first worker node.
type RenderedMachineConfigs struct {
	// ControlPlaneNode is the node name the control-plane bytes are rendered for.
	ControlPlaneNode string
	ControlPlane     []byte
	// WorkerNode is the node name the worker bytes are rendered for.
	WorkerNode string
	Worker     []byte
}

// RenderMachineConfigs renders, offline, the Talos machine configuration
// cluster create would apply to the first node of each role. It uses the same
// regeneration and per-node marshalling create uses, with KSail's pinned Talos
// machinery, so the bytes equal what create sends for the same endpoint,
// SANs, secrets and token. It starts no cluster and calls no cloud API.
func RenderMachineConfigs(
	configs *talosconfigmanager.Configs,
	provider v1alpha1.Provider,
	hetznerOpts v1alpha1.OptionsHetzner,
	opts RenderOptions,
) (*RenderedMachineConfigs, error) {
	if configs == nil {
		return nil, errFloatingIPConfigsUnavailable
	}

	if provider != v1alpha1.ProviderHetzner {
		return nil, fmt.Errorf("%w: got %q", ErrRenderProviderUnsupported, provider)
	}

	rendered, err := configs.WithSecrets(opts.Secrets)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate configs with secrets: %w", err)
	}

	token := opts.HcloudToken
	if token == "" {
		token = RenderPlaceholderHcloudToken
	}

	rendered, err = hetznerEndpointConfigs(
		rendered, opts.EndpointIP, opts.CertSANs, hetznerOpts.FloatingIPEnabled,
		func() (string, error) { return token, nil },
	)
	if err != nil {
		return nil, err
	}

	clusterName := rendered.GetClusterName()

	out := &RenderedMachineConfigs{}

	out.ControlPlaneNode, out.ControlPlane, err = renderHetznerNode(
		rendered.ControlPlane(), clusterName, RoleControlPlane,
	)
	if err != nil {
		return nil, err
	}

	out.WorkerNode, out.Worker, err = renderHetznerNode(
		rendered.Worker(), clusterName, RoleWorker,
	)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// renderHetznerNode marshals a role config for that role's first Hetzner node,
// with the per-node hostname overlay create applies (see applyConfigToNode).
func renderHetznerNode(
	config talosconfig.Provider,
	clusterName, role string,
) (string, []byte, error) {
	nodeName, err := hetznerNodeName(clusterName, role, 1)
	if err != nil {
		return "", nil, err
	}

	cfgBytes, err := marshalConfigWithHostname(config, nodeName)
	if err != nil {
		return "", nil, err
	}

	return nodeName, cfgBytes, nil
}
