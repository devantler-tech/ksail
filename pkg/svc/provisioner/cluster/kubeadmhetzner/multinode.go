package kubeadmhetzner

import (
	"fmt"
	"net"
	"strings"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
)

// kubeadmAPIPort is the port the kubeadm API server serves on — the standard
// Kubernetes secure port a joining node's discovery dials.
const kubeadmAPIPort = "6443"

// joinNameSuffix completes the cluster's stable join name (see [JoinName]). The
// reserved-for-internal-use ".internal" TLD guarantees the name never collides
// with a public DNS zone.
const joinNameSuffix = "-api.ksail.internal"

// Where the pre-seeded shared PKI lands on the cluster-initialising control
// plane. kubeadm reuses existing material at its canonical PKI paths instead
// of minting its own, which is what fixes the cluster identity to the material
// [GenerateClusterPKI] produced at compose time. Certificates and the
// service-account public key are public (world-readable, like kubeadm's own);
// private keys are owner-only.
const (
	caCertPath        = "/etc/kubernetes/pki/ca.crt"
	caCertPermissions = "0644"
	caKeyPath         = "/etc/kubernetes/pki/ca.key"
	caKeyPermissions  = "0600"

	frontProxyCACertPath  = "/etc/kubernetes/pki/front-proxy-ca.crt"
	frontProxyCAKeyPath   = "/etc/kubernetes/pki/front-proxy-ca.key"
	etcdCACertPath        = "/etc/kubernetes/pki/etcd/ca.crt"
	etcdCAKeyPath         = "/etc/kubernetes/pki/etcd/ca.key"
	serviceAccountKeyPath = "/etc/kubernetes/pki/sa.key"
	serviceAccountPubPath = "/etc/kubernetes/pki/sa.pub"
)

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
// mints the cluster's full shared PKI ([GenerateClusterPKI] — cluster CA,
// front-proxy CA, etcd CA, service-account keypair) and seeds it into the
// node's PKI directory so the whole cluster identity — and the discovery hash
// the joining nodes pin — is fixed before any server boots; the node's
// API-server certificate additionally carries the cluster's stable join name
// as a SAN (see [JoinName]). The generated PKI is retained on the Provisioner
// for [Provisioner.ComposeJoiningNodes], which the shared flow always calls
// after this within one create.
func (p *Provisioner) ComposeInitNode(
	clusterName, token string,
	material hetznerbase.BootstrapMaterial,
) (hetznerbase.NodeSpec, error) {
	clusterPKI, err := GenerateClusterPKI()
	if err != nil {
		return hetznerbase.NodeSpec{}, err
	}

	p.clusterPKI = &clusterPKI

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
		ServerInitFiles:   pkiSeedFiles(clusterPKI),
	})
	if err != nil {
		return hetznerbase.NodeSpec{}, fmt.Errorf("compose init control-plane node: %w", err)
	}

	return nodeSpecsFrom(nodes)[0], nil
}

// pkiSeedFiles maps the pre-generated shared PKI onto the cloud-init files the
// cluster-initialising control plane seeds at kubeadm's canonical paths.
// Certificates and the service-account public key are world-readable (matching
// kubeadm's own permissions); private keys are owner-only.
func pkiSeedFiles(clusterPKI ClusterPKI) []cloudinitbootstrap.File {
	return []cloudinitbootstrap.File{
		{Path: caCertPath, Permissions: caCertPermissions, Content: string(clusterPKI.CA.CertPEM)},
		{Path: caKeyPath, Permissions: caKeyPermissions, Content: string(clusterPKI.CA.KeyPEM)},
		{
			Path:        frontProxyCACertPath,
			Permissions: caCertPermissions,
			Content:     string(clusterPKI.FrontProxyCA.CertPEM),
		},
		{
			Path:        frontProxyCAKeyPath,
			Permissions: caKeyPermissions,
			Content:     string(clusterPKI.FrontProxyCA.KeyPEM),
		},
		{
			Path:        etcdCACertPath,
			Permissions: caCertPermissions,
			Content:     string(clusterPKI.EtcdCA.CertPEM),
		},
		{
			Path:        etcdCAKeyPath,
			Permissions: caKeyPermissions,
			Content:     string(clusterPKI.EtcdCA.KeyPEM),
		},
		{
			Path:        serviceAccountKeyPath,
			Permissions: caKeyPermissions,
			Content:     string(clusterPKI.ServiceAccount.KeyPEM),
		},
		{
			Path:        serviceAccountPubPath,
			Permissions: caCertPermissions,
			Content:     string(clusterPKI.ServiceAccount.PubPEM),
		},
	}
}

// ComposeJoiningNodes composes the kubeadm joining nodes — additional control
// planes first, then agents — that register against the init control plane
// reachable at joinAddress (its private-network IPv4), satisfying
// [hetznerbase.MultiNodeComposer]. It plans the full topology so the joining
// nodes keep their global bootstrap indices, threads the stable join name
// (pinned to joinAddress in each node's /etc/hosts) and the pre-seeded CA's
// discovery hash into their JoinConfigurations, and returns only the joining
// nodes — the init node at index 0 is already up. Additional control planes are
// refused until their required private PKI can be transferred outside provider
// user-data.
func (p *Provisioner) ComposeJoiningNodes(
	clusterName, token string,
	joinAddress net.IP,
	material hetznerbase.BootstrapMaterial,
) ([]hetznerbase.NodeSpec, error) {
	if p.ControlPlanes > 1 {
		return nil, ErrHAControlPlaneNotImplemented
	}

	if p.clusterPKI == nil {
		return nil, ErrJoiningNodesComposedFirst
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
			CACertHashes:      []string{p.clusterPKI.CA.DiscoveryHash},
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
