package hetznerbase

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"gopkg.in/yaml.v3"
)

// DefaultImageName is the stock OS image every cloud-init-bootstrapped node
// boots: the k3s and kubeadm distributions install onto a stock Ubuntu LTS
// rather than a custom snapshot or ISO. Hetzner resolves the name to the
// architecture-matching image server-side at creation
// ([hetzner.CreateServerOpts.ImageName]), so no client-side image lookup is
// needed.
const DefaultImageName = "ubuntu-24.04"

const maxDecodedUserDataBytes = 1 << 20

// ErrPrivateKeyInUserData is returned when a server spec would deliver private
// key material through provider-readable user-data. The cloud-init ssh_keys
// module is the deliberate exception: it carries the per-node SSH host identity
// used to pin the bootstrap connection, never cluster-signing PKI.
var ErrPrivateKeyInUserData = errors.New(
	"hetzner: provider user-data contains private key material",
)

var privateKeyPEMHeader = regexp.MustCompile(
	`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`,
)

// NodeSpec pairs a planned node's identity with the cloud-init user_data that
// bootstraps it — the distribution-agnostic per-node shape both the k3s and
// kubeadm provisioners produce before spec derivation places the nodes into
// the cluster's resolved infrastructure.
type NodeSpec struct {
	// Index is the node's zero-based bootstrap position (the
	// cluster-initialising control plane is 0).
	Index int
	// NodeType is the Hetzner node-type label value
	// ([hetzner.NodeTypeControlPlane] or [hetzner.NodeTypeWorker]).
	NodeType string
	// UserData is the cloud-init document delivered as the server's user_data.
	UserData string
	// Labels is the Hetzner label set applied to the server.
	Labels map[string]string
}

// NodeSpecsFrom maps a distro's per-node build output to the shared
// []NodeSpec the bring-up plan derives server specs from, applying toSpec to
// each node in order. It exists so each provisioner's composeNodes callback
// need not re-write the make-and-loop boilerplate — only the per-node field
// projection (toSpec), which differs by the distro's node type, lives at the
// call site.
func NodeSpecsFrom[Node any](nodes []Node, toSpec func(Node) NodeSpec) []NodeSpec {
	specs := make([]NodeSpec, len(nodes))
	for index, node := range nodes {
		specs[index] = toSpec(node)
	}

	return specs
}

// DeriveServerSpecs turns the per-node cloud-init user_data a provisioner
// composed into the ordered [hetzner.CreateServerOpts] fed to the Hetzner
// server-creation API — the composition step between "what to run on each
// node" and "which server runs it", shared by the k3s and kubeadm
// provisioners. For every node it derives the validated server name
// ([hetzner.NodeName], sharing the identity encoded in the node's labels),
// selects the configured per-role server type, boots [DefaultImageName], and
// places the server into the resolved infrastructure.
//
// The index carried by each [NodeSpec] is reused verbatim for the server name
// and matches the node's ksail.node.index label, so a node's name and labels
// always agree. DeriveServerSpecs is pure — no I/O, no network — and never
// returns a partial result: a name that exceeds the DNS-1123 label limit fails
// the whole derivation rather than provisioning a mis-named subset.
func DeriveServerSpecs(
	clusterName string,
	nodes []NodeSpec,
	opts v1alpha1.OptionsHetzner,
	infra ResolvedInfra,
) ([]hetzner.CreateServerOpts, error) {
	specs := make([]hetzner.CreateServerOpts, 0, len(nodes))

	for _, node := range nodes {
		err := validateProviderUserData(node.UserData)
		if err != nil {
			return nil, fmt.Errorf("validate user-data for node %d: %w", node.Index, err)
		}

		name, err := hetzner.NodeName(clusterName, node.NodeType, node.Index)
		if err != nil {
			return nil, fmt.Errorf("derive server name for node %d: %w", node.Index, err)
		}

		specs = append(specs, hetzner.CreateServerOpts{
			Name:             name,
			ServerType:       nodeServerType(opts, node.NodeType),
			ImageName:        DefaultImageName,
			Location:         opts.Location,
			Labels:           node.Labels,
			UserData:         node.UserData,
			NetworkID:        infra.NetworkID,
			PlacementGroupID: infra.PlacementGroupID,
			SSHKeyID:         infra.SSHKeyID,
			FirewallIDs:      firewallIDs(infra.FirewallID),
		})
	}

	return specs, nil
}

// validatePEMPrivateKeys rejects raw or encoded PEM private keys anywhere in
// the cloud-init YAML stream except a document's top-level ssh_keys module.
// That module is the intentional per-node host identity; every other private
// key would be retained in Hetzner's provider-readable user-data.
func validatePEMPrivateKeys(userData string) error {
	decoder := yaml.NewDecoder(strings.NewReader(userData))

	for {
		var document yaml.Node

		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("parse cloud-init user-data: %w", err)
		}

		if yamlNodeContainsPrivateKey(&document, true, make(map[*yaml.Node]struct{})) {
			return ErrPrivateKeyInUserData
		}
	}
}

// yamlNodeContainsPrivateKey walks scalar mapping keys and values while
// exempting only the top-level cloud-init ssh_keys value. Nested keys with the
// same name are not identities and remain subject to inspection. Active nodes
// stop recursive aliases from cycling without suppressing later sibling paths.
func yamlNodeContainsPrivateKey(
	node *yaml.Node,
	topLevel bool,
	active map[*yaml.Node]struct{},
) bool {
	if node == nil {
		return false
	}

	if _, visited := active[node]; visited {
		return false
	}

	active[node] = struct{}{}
	defer delete(active, node)

	switch node.Kind {
	case yaml.DocumentNode:
		return yamlChildrenContainPrivateKey(node.Content, true, active)
	case yaml.MappingNode:
		return yamlMappingContainsPrivateKey(node.Content, topLevel, active)
	case yaml.SequenceNode:
		return yamlChildrenContainPrivateKey(node.Content, false, active)
	case yaml.ScalarNode:
		return containsPrivateKeyMaterial(node.Value)
	case yaml.AliasNode:
		return yamlNodeContainsPrivateKey(node.Alias, false, active)
	}

	return false
}

func yamlChildrenContainPrivateKey(
	children []*yaml.Node,
	topLevel bool,
	active map[*yaml.Node]struct{},
) bool {
	for _, child := range children {
		if yamlNodeContainsPrivateKey(child, topLevel, active) {
			return true
		}
	}

	return false
}

func yamlMappingContainsPrivateKey(
	content []*yaml.Node,
	topLevel bool,
	active map[*yaml.Node]struct{},
) bool {
	for index := 0; index+1 < len(content); index += 2 {
		key := content[index]
		value := content[index+1]

		if yamlNodeContainsPrivateKey(key, false, active) {
			return true
		}

		if topLevel && key.Kind == yaml.ScalarNode && key.Value == "ssh_keys" {
			continue
		}

		if yamlNodeContainsPrivateKey(value, false, active) {
			return true
		}
	}

	return false
}

func containsPrivateKeyMaterial(value string) bool {
	if containsPrivateKeyPEM(value) {
		return true
	}

	decoded, ok := decodeBase64(value)
	if !ok {
		return false
	}

	return decodedContainsPrivateKey(decoded)
}

func decodeBase64(value string) ([]byte, bool) {
	compact := strings.Map(func(character rune) rune {
		if character == ' ' || character == '\n' || character == '\r' || character == '\t' {
			return -1
		}

		return character
	}, value)

	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(compact)
	}

	if err != nil || len(decoded) == 0 {
		return nil, false
	}

	return decoded, true
}

func decodedContainsPrivateKey(decoded []byte) bool {
	if containsPrivateKeyPEM(string(decoded)) {
		return true
	}

	if len(decoded) < 2 || decoded[0] != 0x1f || decoded[1] != 0x8b {
		return false
	}

	return gzipContainsPrivateKey(decoded)
}

func gzipContainsPrivateKey(compressed []byte) bool {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return false
	}

	decompressed, readErr := io.ReadAll(io.LimitReader(reader, maxDecodedUserDataBytes+1))

	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return false
	}

	// Provider user-data is small. An encoded scalar expanding beyond this bound
	// is refused rather than allowed to hide a marker past the inspected prefix.
	if len(decompressed) > maxDecodedUserDataBytes {
		return true
	}

	return containsPrivateKeyPEM(string(decompressed))
}

func containsPrivateKeyPEM(value string) bool {
	return privateKeyPEMHeader.MatchString(value)
}

// nodeServerType selects the configured Hetzner server type for a node's role.
// The config layer applies the defaults (see [v1alpha1.OptionsHetzner]), so the
// options arrive resolved here.
func nodeServerType(opts v1alpha1.OptionsHetzner, nodeType string) string {
	if nodeType == hetzner.NodeTypeWorker {
		return opts.WorkerServerType
	}

	return opts.ControlPlaneServerType
}

// firewallIDs wraps a resolved firewall ID as the single-element slice
// [hetzner.CreateServerOpts] expects, or nil when no firewall (a zero ID) is set.
func firewallIDs(firewallID int64) []int64 {
	if firewallID == 0 {
		return nil
	}

	return []int64{firewallID}
}

// ErrSigningTransportInUserData is returned when a server spec would deliver
// cluster-signing material through provider-readable user-data by a transport
// that carries no PEM block. kubeadm's --upload-certs stores the cluster PKI in
// a Secret and the certificate key decrypts it, so either one hands the provider
// the cluster identity as surely as the key itself would. A path to a private
// half of the cluster PKI means the renderer is writing that material rather
// than letting kubeadm mint it on the node.
var ErrSigningTransportInUserData = errors.New(
	"hetzner: provider user-data contains cluster-signing transport material",
)

// clusterPKIKeyPath matches a path to a private half of the cluster PKI. It
// keys on the directory plus the .key extension rather than enumerating the
// four file names, so a PKI key this package does not yet know about is still
// caught. The public .crt halves are deliberately not matched.
var clusterPKIKeyPath = regexp.MustCompile(`/etc/kubernetes/pki/[^\s"']*\.key`)

// signingTransportMarkers are the kubeadm certificate-transport settings in
// normalised form. Each is stored as normaliseTransportText renders it, so one
// entry covers every separator and casing spelling of that setting.
func signingTransportMarkers() []string {
	return []string{"certificatekey", "uploadcerts"}
}

// normaliseTransportText lowercases and drops the separators that distinguish
// otherwise identical spellings, collapsing certificateKey, certificate-key and
// certificate_key onto one token. Matching the normalised form is what keeps
// this guard from being evaded by a spelling it does not enumerate.
func normaliseTransportText(value string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(value))
}

// matchesSigningTransport reports whether the text carries a certificate
// transport marker or a cluster PKI private-key path.
func matchesSigningTransport(value string) bool {
	if clusterPKIKeyPath.MatchString(value) {
		return true
	}

	normalised := normaliseTransportText(value)
	for _, marker := range signingTransportMarkers() {
		if strings.Contains(normalised, marker) {
			return true
		}
	}

	return false
}

// validateSigningTransport rejects certificate-transport markers and cluster PKI
// key paths anywhere in the user-data.
//
// Unlike the PEM guard this applies no ssh_keys exemption: that module carries
// the node's SSH host identity, which legitimately contains a private key but
// never a kubeadm certificate transport, so exempting it here would leave a
// blind spot rather than avoid a false positive.
//
// The raw document is scanned first so the result cannot be changed by how the
// YAML is structured, then each scalar's decoded form is inspected so a marker
// hidden in a base64 or gzip payload is caught by the same unwrapping the PEM
// guard performs.
func validateSigningTransport(userData string) error {
	if matchesSigningTransport(userData) {
		return ErrSigningTransportInUserData
	}

	decoder := yaml.NewDecoder(strings.NewReader(userData))

	for {
		var document yaml.Node

		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("parse cloud-init user-data: %w", err)
		}

		if encodedSigningTransport(&document, make(map[*yaml.Node]struct{})) {
			return ErrSigningTransportInUserData
		}
	}
}

// decodedContainsSigningTransport reports whether a scalar's base64 or gzip
// payload carries a transport marker. The plaintext form is already covered by
// the raw-document scan, so only the decoded shapes are inspected here.
func decodedContainsSigningTransport(value string) bool {
	decoded, decodedOK := decodeBase64(value)
	if !decodedOK {
		return false
	}

	if matchesSigningTransport(string(decoded)) {
		return true
	}

	if len(decoded) < 2 || decoded[0] != 0x1f || decoded[1] != 0x8b {
		return false
	}

	expanded, oversize, expandedOK := gunzipLimited(decoded)
	if oversize {
		return true
	}

	if !expandedOK {
		return false
	}

	return matchesSigningTransport(expanded)
}

// gunzipLimited expands a gzip payload up to the inspection bound. oversize
// reports a payload expanding past that bound, which callers refuse rather than
// allow to hide a marker past the inspected prefix; ok reports whether expanded
// holds a usable result.
func gunzipLimited(compressed []byte) (string, bool, bool) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", false, false
	}

	decompressed, readErr := io.ReadAll(io.LimitReader(reader, maxDecodedUserDataBytes+1))

	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return "", false, false
	}

	if len(decompressed) > maxDecodedUserDataBytes {
		return "", true, false
	}

	return string(decompressed), false, true
}

// validateProviderUserData rejects any transport that would retain cluster
// signing material in Hetzner's provider-readable user-data. PEM material is
// checked first so a leaked key keeps reporting the more specific error.
func validateProviderUserData(userData string) error {
	err := validatePEMPrivateKeys(userData)
	if err != nil {
		return err
	}

	return validateSigningTransport(userData)
}

// encodedSigningTransport walks every scalar and inspects its decoded form.
// Active nodes stop recursive aliases from cycling.
func encodedSigningTransport(node *yaml.Node, active map[*yaml.Node]struct{}) bool {
	if node == nil {
		return false
	}

	if _, visited := active[node]; visited {
		return false
	}

	active[node] = struct{}{}
	defer delete(active, node)

	switch node.Kind {
	case yaml.ScalarNode:
		return decodedContainsSigningTransport(node.Value)
	case yaml.AliasNode:
		return encodedSigningTransport(node.Alias, active)
	case yaml.DocumentNode, yaml.SequenceNode, yaml.MappingNode:
		return encodedSigningTransportInChildren(node.Content, active)
	}

	return false
}

// encodedSigningTransportInChildren inspects each child in order.
func encodedSigningTransportInChildren(
	children []*yaml.Node,
	active map[*yaml.Node]struct{},
) bool {
	for _, child := range children {
		if encodedSigningTransport(child, active) {
			return true
		}
	}

	return false
}
