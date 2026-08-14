package talosgenerator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// Talos patch subdirectories under the patches root.
const (
	subdirCluster       = "cluster"
	subdirControlPlanes = "control-planes"
	subdirWorkers       = "workers"
)

// patchSpec declaratively describes one generated Talos patch file: the
// condition that enables it, where it is written (subdir/filename under the
// patches root), and a function that produces its content. The single table
// returned by patchSpecs replaces the previously hand-synced lists
// (getDirectoriesWithPatches, generateConditionalPatches) and the ~11 near
// identical writer functions, so adding a patch is one table entry rather than
// four coordinated edits.
type patchSpec struct {
	// when reports whether this patch should be generated for the given config.
	when func(*Config) bool
	// subdir is the patches subdirectory the file is written to (cluster,
	// control-planes, or workers).
	subdir string
	// filename is the patch file name within subdir.
	filename string
	// content produces the patch file content. It may read config-referenced
	// files (e.g. the OIDC CA file) and therefore returns an error.
	content func(*Config) (string, error)
}

// staticContent wraps a constant patch body in the content func signature.
func staticContent(body string) func(*Config) (string, error) {
	return func(*Config) (string, error) { return body, nil }
}

// patchSpecs returns the full, ordered table of conditional Talos patches. The
// order matches the previous generateConditionalPatches sequence so generated
// output is byte-identical.
func patchSpecs() []patchSpec {
	specs := append(clusterPatchSpecs(), kubeletPatchSpecs()...)
	specs = append(specs, schedulingAndImagePatchSpecs()...)

	return append(specs, ingressFirewallSpecs()...)
}

// clusterPatchSpecs returns the cluster-scoped patches that have no shared
// conditions (mirror registries, CNI, cluster name, CDI, cloud provider, OIDC).
func clusterPatchSpecs() []patchSpec {
	return []patchSpec{
		{
			when:     func(cfg *Config) bool { return len(cfg.MirrorRegistries) > 0 },
			subdir:   subdirCluster,
			filename: mirrorRegistriesFileName,
			content:  mirrorRegistriesContent,
		},
		{
			// Disable the default CNI when an alternative CNI is requested.
			when:     func(cfg *Config) bool { return cfg.DisableDefaultCNI },
			subdir:   subdirCluster,
			filename: disableCNIFileName,
			content: func(cfg *Config) (string, error) {
				content := talosconfigmanager.DisableDefaultCNIPatchYAML(
					cfg.MultiDocumentKubernetesConfig,
				)

				return content, nil
			},
		},
		{
			when:     func(cfg *Config) bool { return cfg.ClusterName != "" },
			subdir:   subdirCluster,
			filename: clusterNameFileName,
			content:  clusterNameContent,
		},
		{
			when:     func(cfg *Config) bool { return cfg.DisableCDI },
			subdir:   subdirCluster,
			filename: disableCDIFileName,
			content: staticContent(`machine:
  features:
    enableCDI: false
`),
		},
		{
			when:     func(cfg *Config) bool { return cfg.EnableExternalCloudProvider },
			subdir:   subdirCluster,
			filename: externalCloudProviderFileName,
			content:  staticContent(ExternalCloudProviderPatchYAML),
		},
		{
			when:     func(cfg *Config) bool { return cfg.EnableOIDC },
			subdir:   subdirCluster,
			filename: oidcFileName,
			content:  oidcContent,
		},
	}
}

// kubeletPatchSpecs returns the kubelet cert-rotation and its paired CSR-approver
// patches. They share one condition and appear adjacently (rotation first) so the
// pair can never drift out of sync.
func kubeletPatchSpecs() []patchSpec {
	enabled := func(cfg *Config) bool { return cfg.EnableKubeletCertRotation }

	return []patchSpec{
		{
			when:     enabled,
			subdir:   subdirCluster,
			filename: kubeletCertRotationFileName,
			content: staticContent(`machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"
`),
		},
		{
			// The csr-approver (installed via inlineManifests during bootstrap)
			// signs the rotated certs; it shares the cert-rotation condition.
			when:     enabled,
			subdir:   subdirCluster,
			filename: kubeletCSRApproverFileName,
			content:  func(*Config) (string, error) { return KubeletCSRApproverInlineManifestPatchYAML(), nil },
		},
	}
}

// schedulingAndImagePatchSpecs returns the allow-scheduling and image
// verification patches.
func schedulingAndImagePatchSpecs() []patchSpec {
	return []patchSpec{
		{
			// Allow scheduling on control planes when no workers are configured.
			when:     func(cfg *Config) bool { return cfg.WorkerNodes == 0 },
			subdir:   subdirCluster,
			filename: allowSchedulingFileName,
			content: staticContent(`cluster:
  allowSchedulingOnControlPlanes: true
`),
		},
		{
			when:     func(cfg *Config) bool { return cfg.EnableImageVerification },
			subdir:   subdirCluster,
			filename: imageVerificationFileName,
			content:  staticContent(imageVerificationTemplate),
		},
	}
}

// ingressFirewallSpecs returns the three ingress firewall patch entries (cluster
// default action + per-role rules). They share one condition and validate the
// model once per generate pass via validateIngressFirewallModel in Generate.
func ingressFirewallSpecs() []patchSpec {
	enabled := func(cfg *Config) bool { return cfg.EnableIngressFirewall }

	return []patchSpec{
		{
			when:     enabled,
			subdir:   subdirCluster,
			filename: ingressFirewallDefaultActionFileName,
			content:  staticContent(IngressFirewallDefaultActionYAML),
		},
		{
			when:     enabled,
			subdir:   subdirControlPlanes,
			filename: ingressFirewallRulesFileName,
			content: func(cfg *Config) (string, error) {
				return IngressFirewallCPRulesYAML(
					cfg.NetworkCIDR,
					cfg.CNIPort,
					cfg.AllowedCIDRs,
				), nil
			},
		},
		{
			when:     enabled,
			subdir:   subdirWorkers,
			filename: ingressFirewallRulesFileName,
			content: func(cfg *Config) (string, error) {
				return IngressFirewallWorkerRulesYAML(cfg.NetworkCIDR, cfg.CNIPort), nil
			},
		},
	}
}

// mirrorRegistriesContent builds the mirror-registries patch body, returning ""
// when there are no parseable specs so writePatchFile skips the file.
func mirrorRegistriesContent(cfg *Config) (string, error) {
	specs := registry.ParseMirrorSpecs(cfg.MirrorRegistries)
	if len(specs) == 0 {
		return "", nil
	}

	return generateMirrorPatchYAML(specs), nil
}

// clusterNameContent builds the cluster-name patch. The cluster name is
// pre-validated as DNS-1123 before reaching here, so direct construction is safe
// from injection.
func clusterNameContent(cfg *Config) (string, error) {
	return fmt.Sprintf(`cluster:
  clusterName: %s
`, cfg.ClusterName), nil
}

// oidcContent builds the version-appropriate OIDC configuration. Talos 1.14
// uses KubeAuthenticationConfig; older contracts retain API-server flags and a
// machine.files CA mount.
func oidcContent(cfg *Config) (string, error) {
	var builder strings.Builder

	if !cfg.MultiDocumentKubernetesConfig && cfg.OIDCCAFile != "" {
		err := writeOIDCCAMachineFiles(&builder, cfg.OIDCCAFile)
		if err != nil {
			return "", err
		}
	}

	if !cfg.MultiDocumentKubernetesConfig {
		writeOIDCAPIServerArgs(&builder, cfg)

		return builder.String(), nil
	}

	var caContent string

	if cfg.OIDCCAFile != "" {
		content, err := readOIDCCAFile(cfg.OIDCCAFile)
		if err != nil {
			return "", err
		}

		caContent = string(content)
	}

	builder.Write(talosconfigmanager.StructuredOIDCPatchYAML(
		talosconfigmanager.OIDCPatchConfig{
			IssuerURL:            cfg.OIDCIssuerURL,
			ClientID:             cfg.OIDCClientID,
			UsernameClaim:        cfg.OIDCUsernameClaim,
			UsernamePrefix:       cfg.OIDCUsernamePrefix,
			GroupsClaim:          cfg.OIDCGroupsClaim,
			GroupsPrefix:         cfg.OIDCGroupsPrefix,
			CertificateAuthority: caContent,
		},
	))

	return builder.String(), nil
}

// writePatchFile writes the patch for spec to subdir/filename under rootPath. It
// performs the exists/!force skip BEFORE generating content via spec.content, so a
// content func with side effects (e.g. the OIDC patch reads the CA file off disk)
// is never invoked for an already-present patch on a no-force re-run — keeping
// re-generation idempotent even when an input the content depends on has moved.
// It also skips when content resolves to empty. The stricter stat-error handling
// (propagate unexpected stat failures) is preserved so patches fail loudly on
// e.g. permission errors rather than silently swallowing them.
func writePatchFile(rootPath string, spec patchSpec, model *Config, force bool) error {
	path := filepath.Join(rootPath, spec.subdir, spec.filename)

	if !force {
		_, statErr := os.Stat(path)
		if statErr == nil {
			return nil
		}

		if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to stat %s: %w", spec.filename, statErr)
		}
	}

	content, err := spec.content(model)
	if err != nil {
		return err
	}

	if content == "" {
		return nil
	}

	err = os.WriteFile(path, []byte(content), filePerm)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", spec.filename, err)
	}

	return nil
}
