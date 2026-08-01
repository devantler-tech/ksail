package kubeadmhetzner

import (
	"fmt"
	"net"
	"strings"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeadmAPIPort is the port the kubeadm API server serves on — the standard
// Kubernetes secure port a joining node's discovery dials.
const kubeadmAPIPort = "6443"

// joinNameSuffix completes the cluster's stable join name (see [JoinName]). The
// reserved-for-internal-use ".internal" TLD guarantees the name never collides
// with a public DNS zone.
const joinNameSuffix = "-api.ksail.internal"

// staticMultiNodeComposerCheck asserts at compile time that *Provisioner
// implements the optional [hetznerbase.MultiNodeComposer] capability, so the
// shared create flow routes a kubeadm topology with agents to the two-phase
// bring-up instead of rejecting it. Kubeadm × Hetzner deliberately does not
// implement [hetznerbase.HAControlPlaneComposer]: joining control planes need
// private cluster PKI, and cloud-init user-data is not an acceptable transport
// for cluster signing keys.
var _ hetznerbase.MultiNodeComposer = (*Provisioner)(nil)

// ControlPlaneJoinCompletePath returns the sentinel the kubeadm first-boot
// bootstrap writes once its `kubeadm join` command succeeds. It is retained as
// the kubeadm control-plane join sentinel for a future HA implementation that
// can transfer private cluster PKI without using cloud-init user-data.
func (p *Provisioner) ControlPlaneJoinCompletePath() string {
	return kubeadmbootstrap.BootstrapSentinelPath
}

// JoinName returns the cluster's stable join name: the DNS name the joining
// nodes dial the cluster-initialising control plane by, and the extra SAN its
// API-server serving certificate carries.
//
// # Why a name and not the IP
//
// A joining node registers against the init control plane's *private-network*
// IPv4, which Hetzner assigns only when that server is created — after every
// node's kubeadm configuration (and thus the init node's certificate SAN list)
// has been composed. The IP therefore cannot appear in the serving certificate,
// and a joiner dialing it raw would fail TLS hostname verification after token
// discovery. A compose-time-stable NAME closes the gap from both sides: the
// init node's certificate carries it up front (kubeadm renders CertSANs into
// the serving cert), and each joining node pins it to the resolved private
// address in /etc/hosts (see [hostsPinCommand]) before `kubeadm join` dials it.
// No extra cloud resource (pre-allocated/floating IP) and no boot-time
// certificate mutation is needed, and the same name is the natural
// ControlPlaneEndpoint for the later HA increment.
func JoinName(clusterName string) string {
	return clusterName + joinNameSuffix
}

// ComposeInitNode composes the single cluster-initialising kubeadm control
// plane (bootstrap index 0), satisfying [hetznerbase.MultiNodeComposer]. It
// lets kubeadm mint its own cluster PKI on the node and carries only the
// cluster's stable join name as an extra API-server SAN (see [JoinName]). No
// cluster-signing material is transported through provider user-data; the
// joining nodes derive their public CA discovery pin later from the admin
// kubeconfig retrieved over the pinned bootstrap SSH connection.
func (p *Provisioner) ComposeInitNode(
	clusterName, token string,
	material hetznerbase.BootstrapMaterial,
) (hetznerbase.NodeSpec, error) {
	joinName := JoinName(clusterName)

	// A reduced single-node plan: the init node's own configuration is identical
	// in every topology (the join settings apply only to joining nodes), and the
	// full plan cannot be expanded yet — its join endpoint resolves at run time.
	nodes, err := BuildNodeUserData(Input{
		ClusterName: clusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             token,
			KubernetesVersion: p.kubernetesVersion,
			CertSANs:          []string{joinName},
			ControlPlaneCount: 1,
			AgentCount:        0,
		},
		SSHAuthorizedKeys: []string{material.AuthorizedKey},
		HostKeys:          material.HostKeys,
	})
	if err != nil {
		return hetznerbase.NodeSpec{}, fmt.Errorf("compose init control-plane node: %w", err)
	}

	return nodeSpecsFrom(nodes)[0], nil
}

// ComposeJoiningNodes composes the kubeadm joining nodes — additional control
// planes first, then agents — that register against the init control plane
// reachable at joinAddress (its private-network IPv4), satisfying
// [hetznerbase.MultiNodeComposer]. It plans the full topology so the joining
// nodes keep their global bootstrap indices, threads the stable join name
// (pinned to joinAddress in each node's /etc/hosts) and the kubeadm-minted CA's
// discovery hash into their JoinConfigurations, and returns only the joining
// nodes — the init node at index 0 is already up. The discovery hash is derived
// from the admin kubeconfig that the shared flow retrieved over pinned SSH.
// Additional control planes are refused until their required private PKI can be
// transferred outside provider user-data.
func (p *Provisioner) ComposeJoiningNodes(
	clusterName, token string,
	joinAddress net.IP,
	initKubeconfig []byte,
	material hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	if p.ControlPlanes > 1 {
		return nil, ErrHAControlPlaneNotImplemented
	}

	discoveryHash, err := discoveryHashFromAdminKubeconfig(initKubeconfig)
	if err != nil {
		return nil, err
	}

	joinName := JoinName(clusterName)

	nodes, err := BuildNodeUserData(Input{
		ClusterName: clusterName,
		Plan: kubeadmbootstrap.PlanInput{
			Token:             token,
			KubernetesVersion: p.kubernetesVersion,
			CertSANs:          []string{joinName},
			ControlPlaneCount: p.ControlPlanes,
			AgentCount:        p.Agents,
			APIServerEndpoint: net.JoinHostPort(joinName, kubeadmAPIPort),
			CACertHashes:      []string{discoveryHash},
		},
		SSHAuthorizedKeys: []string{material.AuthorizedKey},
		HostKeys:          material.HostKeys,
		JoinPrelude:       []string{hostsPinCommand(joinAddress, joinName)},
	})
	if err != nil {
		return nil, fmt.Errorf("compose joining nodes: %w", err)
	}

	return nodeSpecsFrom(nodes)[1:], nil
}

// discoveryHashFromAdminKubeconfig extracts the current cluster's CA
// certificate from kubeadm's admin.conf and returns the public-key pin kubeadm
// join verifies. The kubeconfig arrives over the authenticated, host-key-pinned
// SSH bootstrap channel; only its public CA certificate is projected into the
// joining nodes' provider user-data.
func discoveryHashFromAdminKubeconfig(data []byte) (string, error) {
	config, err := clientcmd.Load(data)
	if err != nil {
		return "", fmt.Errorf("%w: parse admin kubeconfig: %w", ErrInvalidAdminKubeconfig, err)
	}

	currentContext := config.Contexts[config.CurrentContext]
	if currentContext == nil {
		return "", fmt.Errorf("%w: current context is missing", ErrInvalidAdminKubeconfig)
	}

	cluster := config.Clusters[currentContext.Cluster]
	if cluster == nil || len(cluster.CertificateAuthorityData) == 0 {
		return "", fmt.Errorf("%w: current cluster CA data is missing", ErrInvalidAdminKubeconfig)
	}

	discoveryHash, err := discoveryHashFromCertificate(cluster.CertificateAuthorityData)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidAdminKubeconfig, err)
	}

	return discoveryHash, nil
}

// hostsPinCommand renders the first-boot command that pins the cluster's stable
// join name to the init control plane's private address in /etc/hosts, making
// the name the joining node's kubeadm configuration dials resolvable — durably,
// so the kubelet's post-join API connections keep resolving it too. The payload
// is single-quoted with embedded quotes escaped, so shell safety holds here
// regardless of what the name-composition path upstream accepted.
func hostsPinCommand(joinAddress net.IP, joinName string) string {
	return "echo " + shellSingleQuote(joinAddress.String()+" "+joinName) + " >> /etc/hosts"
}

// shellSingleQuote wraps s in single quotes, escaping any embedded single
// quote, so the result splices into a shell command as one literal word.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
