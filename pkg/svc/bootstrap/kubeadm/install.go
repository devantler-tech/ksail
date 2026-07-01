package kubeadmbootstrap

import "strings"

// ConfigPath is where the rendered kubeadm configuration (see [Render]) is dropped
// on the node and where the first-boot bootstrap command reads it from. It is a
// fixed, ksail-managed location under /etc/kubernetes so an operator inspecting a
// node knows where the config came from.
const ConfigPath = "/etc/kubernetes/ksail/kubeadm-config.yaml"

// configPermissions is the mode of the on-node kubeadm config. The config carries
// the shared bootstrap token, so it is root read/write only.
const configPermissions = "0600"

// packageRepoBaseURL is the base of the Kubernetes community (pkgs.k8s.io) package
// repository. A node installs the kube* components from the stable channel of its
// pinned minor track, e.g. .../core:/stable:/v1.31/deb/.
const packageRepoBaseURL = "https://pkgs.k8s.io/core:/stable:/"

// aptSourceName is the cloud-init apt.sources entry name for the Kubernetes
// repository. cloud-init writes it to /etc/apt/sources.list.d/<name>.list.
const aptSourceName = "kubernetes"

// kubePackages returns the Kubernetes node components installed from the community
// repository, in a deterministic order (kubelet the node agent, kubeadm the
// bootstrapper, kubectl the CLI). The container runtime is installed separately
// from the distribution's own repository — see [containerdPackage]. A fresh slice
// is returned each call so a caller (e.g. [RenderInstall] building Packages) can
// append without aliasing a shared backing array.
func kubePackages() []string {
	return []string{"kubelet", "kubeadm", "kubectl"}
}

// containerdPackage is the CRI container runtime. It ships in the base
// distribution repositories (not pkgs.k8s.io), so it needs no extra apt source.
const containerdPackage = "containerd"

// AptSource is a declarative apt repository for cloud-init's apt.sources module:
// the module adds the Source deb line and trusts the signing key fetched from
// KeyURL, so the node needs no imperative `curl … | gpg --dearmor` key step. It is
// the declarative expression of "add the Kubernetes package repository".
type AptSource struct {
	// Name is the apt.sources entry name; cloud-init writes it to
	// /etc/apt/sources.list.d/<Name>.list.
	Name string
	// Source is the one-line apt sources entry, e.g.
	// "deb https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /".
	Source string
	// KeyURL is the URL of the repository's signing key (its Release.key), which
	// cloud-init fetches and trusts for this source.
	KeyURL string
}

// File is a file dropped onto the node before the install runs — the declarative
// expression of a cloud-init write_files entry. Content is written verbatim.
type File struct {
	// Path is the absolute on-node destination path.
	Path string
	// Permissions is the octal file mode, e.g. "0600".
	Permissions string
	// Content is the file body, written verbatim.
	Content string
}

// Install is the declarative, distribution-native installation of one kubeadm
// node: the apt repositories to add, the OS packages to install, the files to
// drop, and the commands to run once at first boot. It is transport-agnostic — a
// provisioner marshals it into a cloud-init document (apt.sources / packages /
// write_files / runcmd) or drives it over any other first-boot channel — which
// keeps this renderer free of any single transport's shape and fully
// unit-testable. It is the declarative sibling of the k3sbootstrap install
// command: where k3s has no distribution package and is installed by piping its
// script into a shell, kubeadm and the container runtime are real OS packages
// installed the declarative way (never `curl … | sh`).
type Install struct {
	// AptSources are the apt repositories added before packages install.
	AptSources []AptSource
	// Packages are the OS packages installed once the sources are in place.
	Packages []string
	// Files are dropped on the node before the commands run.
	Files []File
	// Commands are the shell commands run, in order, at first boot after the
	// packages are installed — enabling the runtime and running `kubeadm init`
	// (the cluster-initialising control plane) or `kubeadm join` (a joining node).
	Commands []string
}

// InstallConfig is the typed input for [RenderInstall]: the Kubernetes package
// track to install and the rendered kubeadm configuration the node bootstraps
// from. It performs no I/O.
type InstallConfig struct {
	// KubernetesVersion pins the Kubernetes release (e.g. "v1.31.0"); its minor
	// track (v1.31) selects the community package repository. Required — the
	// repository is per-minor, so the version cannot be defaulted here (unlike in a
	// kubeadm ClusterConfiguration, where kubeadm picks a default at run time).
	KubernetesVersion string
	// Role selects the first-boot bootstrap command: `kubeadm init` for
	// RoleServerInit, `kubeadm join` for RoleServer and RoleAgent. Required.
	Role Role
	// Config is the rendered kubeadm configuration YAML for this node (the output
	// of [Render]); it is dropped at [ConfigPath] and passed to `kubeadm --config`.
	// Required.
	Config string
}

// RenderInstall maps cfg to the declarative [Install] that brings a kubeadm node
// up at first boot: it adds the Kubernetes community package repository for the
// requested minor track, installs the container runtime and the kube* components,
// drops the rendered kubeadm config, and runs the role's bootstrap command
// (`kubeadm init` or `kubeadm join`) against that config.
//
// RenderInstall is pure — no I/O, no network — and never returns a
// partially-valid Install: any configuration error (see the package's sentinel
// errors) is reported instead. The returned Install is declarative data with no
// shell-quoting or injection surface of its own; the two fixed bootstrap commands
// interpolate no caller-supplied value (the config path is a package constant).
//
// The container-runtime CRI configuration, the CNI install, and the kubeconfig
// fetch are deliberately out of scope for this slice — they are post-install
// provisioner-lifecycle concerns (as they are for the k3s path), rendered by a
// later increment of devantler-tech/ksail#5513.
func RenderInstall(cfg InstallConfig) (Install, error) {
	track, err := minorTrack(cfg.KubernetesVersion)
	if err != nil {
		return Install{}, err
	}

	if !cfg.Role.valid() {
		return Install{}, ErrInvalidRole
	}

	if cfg.Config == "" {
		return Install{}, ErrMissingConfig
	}

	repoURL := packageRepoBaseURL + track + "/deb/"

	return Install{
		AptSources: []AptSource{{
			Name:   aptSourceName,
			Source: "deb " + repoURL + " /",
			KeyURL: repoURL + "Release.key",
		}},
		Packages: append([]string{containerdPackage}, kubePackages()...),
		Files: []File{{
			Path:        ConfigPath,
			Permissions: configPermissions,
			Content:     cfg.Config,
		}},
		Commands: bootstrapCommands(cfg.Role),
	}, nil
}

// bootstrapCommands returns the ordered first-boot commands for role: enable the
// container runtime, pin the kube* packages so an unattended upgrade cannot break
// the control plane mid-cluster, then run the role's kubeadm bootstrap command
// against the dropped config.
func bootstrapCommands(role Role) []string {
	bootstrap := "kubeadm init --config " + ConfigPath
	if role != RoleServerInit {
		bootstrap = "kubeadm join --config " + ConfigPath
	}

	return []string{
		"systemctl enable --now " + containerdPackage,
		"apt-mark hold " + strings.Join(kubePackages(), " "),
		bootstrap,
	}
}

// minorTrack derives the pkgs.k8s.io repository track (e.g. "v1.31") from a
// "vMAJOR.MINOR[.PATCH]" Kubernetes version. The community repository is published
// per minor version, so only the major and minor are significant; the patch (if
// present) is ignored. A version that is empty or not in the expected form is
// rejected so a node is never pointed at a repository URL that does not resolve.
func minorTrack(version string) (string, error) {
	if version == "" {
		return "", ErrMissingKubernetesVersion
	}

	rest, hadPrefix := strings.CutPrefix(version, "v")
	if !hadPrefix {
		return "", ErrInvalidKubernetesVersion
	}

	parts := strings.Split(rest, ".")

	const minParts = 2
	if len(parts) < minParts {
		return "", ErrInvalidKubernetesVersion
	}

	if !isNumeric(parts[0]) || !isNumeric(parts[1]) {
		return "", ErrInvalidKubernetesVersion
	}

	return "v" + parts[0] + "." + parts[1], nil
}

// isNumeric reports whether s is a non-empty run of ASCII digits, so a version
// component like "1" or "31" is accepted while "", "x", or "1beta" is not.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
