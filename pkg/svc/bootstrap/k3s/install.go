package k3sbootstrap

import (
	"sort"
	"strings"
)

// installScriptURL is the canonical k3s install script. Piping it into `sh`
// (`curl -sfL https://get.k3s.io | … sh -s - …`) is the upstream-documented,
// Docker-free way to install k3s on a host, which is what a raw Hetzner server
// requires (k3d, by contrast, runs k3s inside a local Docker daemon).
const installScriptURL = "https://get.k3s.io"

// subcommandServer is the k3s subcommand for both control-plane roles
// (RoleServerInit and RoleServer); agents use the literal "agent".
const subcommandServer = "server"

// Role identifies how a node joins the cluster. The first control-plane node
// initialises the cluster (RoleServerInit); further control-plane nodes
// (RoleServer) and workers (RoleAgent) join an existing one via its ServerURL.
type Role string

const (
	// RoleServerInit is the first control-plane node, which bootstraps the cluster
	// with an embedded etcd (`server --cluster-init`).
	RoleServerInit Role = "server-init"
	// RoleServer is an additional control-plane node that joins the cluster the
	// first server initialised (`server --server <url>`).
	RoleServer Role = "server"
	// RoleAgent is a worker node (`agent`).
	RoleAgent Role = "agent"
)

// InstallConfig is the typed input for rendering a k3s install command. It is
// distribution- and transport-agnostic: it captures what to install, not how the
// command reaches the server.
type InstallConfig struct {
	// Version pins the k3s release (INSTALL_K3S_VERSION), e.g. "v1.30.2+k3s1".
	// Required — see ErrMissingVersion.
	Version string

	// Role is the node's cluster role. Required.
	Role Role

	// Token is the shared node token (K3S_TOKEN) every node authenticates with.
	// Required — see ErrMissingToken.
	Token string

	// ServerURL is the registration endpoint of an existing server,
	// e.g. "https://10.0.0.2:6443". Required for RoleServer and RoleAgent; must be
	// empty for RoleServerInit (see ErrMissingServerURL / ErrUnexpectedServerURL).
	ServerURL string

	// TLSSANs are additional Subject Alternative Names to add to the API server
	// certificate (--tls-san), e.g. a load-balancer IP or DNS name. Server roles
	// only.
	TLSSANs []string

	// Disable lists bundled components to disable (--disable), e.g. "traefik" or
	// "servicelb". Server roles only.
	Disable []string

	// WriteKubeconfigMode sets the mode of the generated kubeconfig
	// (--write-kubeconfig-mode), e.g. "0644". Optional; server roles only. Omitted
	// when empty.
	WriteKubeconfigMode string
}

// Render builds the shell command that installs and starts k3s for cfg. The
// command is a single line suitable for execution by a remote shell (over SSH)
// or as a cloud-init runcmd. Secrets (the token) are passed via environment
// variables rather than positional arguments so they do not appear in the node's
// process list; callers must still avoid logging the returned string verbatim.
//
// Render is pure and never returns a partially-valid command: any configuration
// error (see the package's sentinel errors) is reported instead.
func Render(cfg InstallConfig) (string, error) {
	err := cfg.validate()
	if err != nil {
		return "", err
	}

	env, subcommand, args := cfg.invocation()

	return assembleCommand(env, subcommand, args), nil
}

// invocation maps a validated cfg to the environment assignments, k3s
// subcommand, and trailing arguments of its install command. It assumes cfg has
// already passed validate, so Role is one of the known roles (RoleServerInit is
// the default branch).
func (cfg InstallConfig) invocation() ([]string, string, []string) {
	version := "INSTALL_K3S_VERSION=" + shellQuote(cfg.Version)
	token := "K3S_TOKEN=" + shellQuote(cfg.Token)

	switch cfg.Role {
	case RoleAgent:
		env := []string{version, "K3S_URL=" + shellQuote(cfg.ServerURL), token}

		return env, "agent", nil
	case RoleServer:
		return []string{version, token}, subcommandServer,
			cfg.serverArgs("--server", shellQuote(cfg.ServerURL))
	case RoleServerInit:
		return []string{version, token}, subcommandServer, cfg.serverArgs("--cluster-init")
	default: // unreachable: validate rejects any other role before invocation runs.
		return []string{version, token}, subcommandServer, cfg.serverArgs("--cluster-init")
	}
}

// serverArgs renders the control-plane-only flags shared by RoleServerInit and
// RoleServer: the role-specific prefix (--cluster-init or --server <url>),
// followed by --tls-san (sorted), --disable (sorted), then
// --write-kubeconfig-mode, in that deterministic order.
func (cfg InstallConfig) serverArgs(prefix ...string) []string {
	const flagsPerEntry = 2

	capacity := len(prefix) +
		flagsPerEntry*(len(cfg.TLSSANs)+len(cfg.Disable)) + flagsPerEntry
	args := make([]string, 0, capacity)
	args = append(args, prefix...)

	for _, san := range sortedCopy(cfg.TLSSANs) {
		args = append(args, "--tls-san", shellQuote(san))
	}

	for _, component := range sortedCopy(cfg.Disable) {
		args = append(args, "--disable", shellQuote(component))
	}

	if cfg.WriteKubeconfigMode != "" {
		args = append(args, "--write-kubeconfig-mode", shellQuote(cfg.WriteKubeconfigMode))
	}

	return args
}

// validate reports the first configuration error in cfg, or nil when cfg renders
// a well-formed command.
func (cfg InstallConfig) validate() error {
	if cfg.Version == "" {
		return ErrMissingVersion
	}

	if cfg.Token == "" {
		return ErrMissingToken
	}

	return cfg.validateRole()
}

// validateRole checks the ServerURL and option constraints specific to cfg.Role.
func (cfg InstallConfig) validateRole() error {
	switch cfg.Role {
	case RoleServerInit:
		if cfg.ServerURL != "" {
			return ErrUnexpectedServerURL
		}

		return nil
	case RoleServer:
		if cfg.ServerURL == "" {
			return ErrMissingServerURL
		}

		return nil
	case RoleAgent:
		return cfg.validateAgent()
	default:
		return ErrUnknownRole
	}
}

// validateAgent checks the constraints unique to an agent node: it must have a
// server to join and must not carry server-only options.
func (cfg InstallConfig) validateAgent() error {
	if cfg.ServerURL == "" {
		return ErrMissingServerURL
	}

	if len(cfg.TLSSANs) > 0 || len(cfg.Disable) > 0 || cfg.WriteKubeconfigMode != "" {
		return ErrAgentServerOnlyOption
	}

	return nil
}

// assembleCommand builds the install command for a node:
//
//	script="$(curl -sfL '<url>')" && printf '%s' "$script" | <env…> sh -s - <subcommand> <args…>
//
// The install script is captured into a shell variable first, then run, rather
// than piped straight into `sh`. A bare `curl … | sh` masks a download failure:
// when curl fails (a `-f` 4xx/5xx, a TLS error, no network) it prints nothing,
// so `sh` runs on empty input and exits 0, silently leaving k3s uninstalled on
// the node. With the capture, the assignment's exit status is curl's, and the
// `&&` gates execution on a successful download, so a failed fetch aborts the
// whole command. This is POSIX-portable — it needs no `set -o pipefail`, so it
// runs the same under dash or bash. `sh -s -` passes the trailing tokens as
// positional arguments to the script, which forwards them to k3s.
func assembleCommand(env []string, subcommand string, args []string) string {
	// Fixed tokens after the capture: `printf '%s' "$script" |` (4) and
	// "sh -s - <subcommand>" (4).
	const fixedTokens = 8

	run := make([]string, 0, len(env)+len(args)+fixedTokens)
	run = append(run, "printf", "'%s'", `"$script"`, "|")
	run = append(run, env...)
	run = append(run, "sh", "-s", "-", subcommand)
	run = append(run, args...)

	return `script="$(curl -sfL '` + installScriptURL + `')" && ` + strings.Join(run, " ")
}

// sortedCopy returns a sorted copy of in without mutating the caller's slice, so
// the rendered command is deterministic regardless of input order.
func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)

	return out
}

// shellQuote single-quotes s for safe inclusion in a POSIX shell command,
// escaping any embedded single quotes via the '\” idiom. Every interpolated
// value (version, token, URL, SANs, components) is quoted so shell metacharacters
// are never interpreted.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
