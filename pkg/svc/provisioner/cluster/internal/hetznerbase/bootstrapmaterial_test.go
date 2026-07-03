package hetznerbase_test

import (
	"net"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// mustParseAuthorizedKey parses a single-line authorized_keys entry, failing
// the test on malformed input.
func mustParseAuthorizedKey(t *testing.T, entry string) gossh.PublicKey {
	t.Helper()

	key, _, _, rest, err := gossh.ParseAuthorizedKey([]byte(entry))
	require.NoError(t, err)
	assert.Empty(t, rest)

	return key
}

func TestGenerateBootstrapMaterial(t *testing.T) {
	t.Parallel()

	material, err := hetznerbase.GenerateBootstrapMaterial()
	require.NoError(t, err)

	// The client half: a usable signer whose public key is what cloud-init
	// delivers into authorized_keys.
	require.NotNil(t, material.Signer)

	clientKey := mustParseAuthorizedKey(t, material.AuthorizedKey)
	assert.Equal(
		t,
		material.Signer.PublicKey().Marshal(),
		clientKey.Marshal(),
	)

	// The host identity: a PEM private key plus its authorized_keys-form public
	// half, ready for cloud-init's ssh_keys module.
	require.NotNil(t, material.HostKeys)
	assert.NotEmpty(t, material.HostKeys.ED25519Private)

	hostKey := mustParseAuthorizedKey(t, material.HostKeys.ED25519Public)

	// The callback pins exactly the delivered host identity: it accepts the
	// generated host key and rejects any other (e.g. the client key). The dial
	// address is a placeholder — FixedHostKey ignores it.
	addr := &net.TCPAddr{IP: net.IPv4(203, 0, 113, 1), Port: 22}

	require.NotNil(t, material.HostKeyCallback)
	require.NoError(t, material.HostKeyCallback("203.0.113.1:22", addr, hostKey))
	require.Error(t, material.HostKeyCallback("203.0.113.1:22", addr, clientKey))

	// Client and host identities are distinct keypairs — compromising the
	// delivered host key must not authenticate a client.
	assert.NotEqual(t, clientKey.Marshal(), hostKey.Marshal())
}
