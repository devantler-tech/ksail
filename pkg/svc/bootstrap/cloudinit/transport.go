package cloudinitbootstrap

// UserDataProvider produces the cloud-init user_data that delivers a node's
// bootstrap commands at first boot. It is the provision-time delivery seam the
// K3s × Hetzner provisioner (devantler-tech/ksail#5512) depends on: the
// provisioner asks for the user_data, then passes it to
// hetzner.CreateServerOpts.UserData when creating each node.
//
// It is deliberately scoped to provision-time delivery. A transport that runs on
// an already-running server (e.g. over SSH) is a separate, post-provision seam —
// its dial path is integration-tested behind the Hetzner system-test lane (#4972)
// — so it is not folded into this interface.
type UserDataProvider interface {
	// UserData returns the cloud-init user_data that runs commands once at first
	// boot, installs sshAuthorizedKeys into the default user's authorized_keys
	// (nil for none), and delivers the pre-generated SSH host identity hostKeys
	// via the ssh_keys module (nil to let the node generate its own), or an error
	// if any of them is not valid. The keys are provision-time delivery too: they
	// let the post-provision SSH bootstrap seam authenticate — and pin the host
	// key — but are delivered declaratively at first boot.
	UserData(
		commands []string,
		sshAuthorizedKeys []string,
		hostKeys *HostKeys,
	) (string, error)
}

// Transport is the cloud-init implementation of [UserDataProvider]. The zero
// value is usable and applies [DefaultScriptPath] and [DefaultLogPath]; [New]
// returns it for explicit construction.
type Transport struct {
	// ScriptPath overrides where the boot script is written. Optional; defaults to
	// [DefaultScriptPath].
	ScriptPath string
	// LogPath overrides where the boot script's output is captured. Optional;
	// defaults to [DefaultLogPath].
	LogPath string
}

// New returns a cloud-init [Transport] using the default script and log paths.
func New() *Transport {
	return &Transport{}
}

// UserData renders commands (and any SSH authorized keys and pre-generated host
// identity) into a cloud-init user_data document, honouring the transport's
// script and log paths. It is pure and reaches no network, so the
// command-construction it performs is fully unit-testable; the document it
// returns is consumed by the provisioner at server-creation time.
func (t *Transport) UserData(
	commands []string,
	sshAuthorizedKeys []string,
	hostKeys *HostKeys,
) (string, error) {
	return BuildUserData(Config{
		Commands:          commands,
		SSHAuthorizedKeys: sshAuthorizedKeys,
		HostKeys:          hostKeys,
		ScriptPath:        t.ScriptPath,
		LogPath:           t.LogPath,
	})
}

// staticUserDataProviderCheck asserts at compile time that Transport satisfies
// UserDataProvider.
var _ UserDataProvider = (*Transport)(nil)
