package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
)

// VClusterConfigFile is the default vCluster configuration filename.
const VClusterConfigFile = "vcluster.yaml"

// generateVClusterConfig generates the vcluster.yaml configuration file.
// VCluster uses a plain YAML values file (Helm-style) rather than a typed API struct,
// so scaffolding generates a minimal empty config with a descriptive comment.
func (s *Scaffolder) generateVClusterConfig(output string, force bool) error {
	configPath := filepath.Join(output, VClusterConfigFile)

	skip, existed, previousModTime := s.checkFileExistsAndSkip(
		configPath,
		VClusterConfigFile,
		force,
	)
	if skip {
		return nil
	}

	// Write a YAML values file with the default Kubernetes version override.
	// The version is read from the embedded Dockerfile in the vcluster
	// configmanager package, which is automatically updated by Dependabot.
	// Users can change this version in the generated file.
	header := "# vCluster Helm values configuration.\n" +
		"# See https://www.vcluster.com/docs/configure/vcluster-yaml" +
		" for available options.\n" +
		"controlPlane:\n" +
		"  distro:\n" +
		"    k8s:\n" +
		"      image:\n" +
		fmt.Sprintf("        tag: %s\n", vclusterconfigmanager.DefaultKubernetesVersion)

	// Emit a single API server extraArgs block combining (a) the MutatingAdmissionPolicy
	// feature gate / v1beta1 admissionregistration API, required only by Calico v3.30+,
	// and (b) OIDC flags when configured. A single block avoids duplicate apiServer keys;
	// the block is omitted entirely when neither applies.
	featureGates := s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICalico
	header += buildVClusterAPIServerArgs(featureGates, &s.KSailConfig.Spec.Cluster.OIDC)

	// Add volume mounts to inject the OIDC CA certificate into the VCluster pod
	if s.KSailConfig.Spec.Cluster.OIDC.Enabled() && s.KSailConfig.Spec.Cluster.OIDC.CAFile != "" {
		header += buildVClusterOIDCVolumes()
	}

	content := []byte(header)

	err := os.WriteFile(configPath, content, filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVClusterConfigGeneration, err)
	}

	if force && existed {
		err = ensureOverwriteModTime(configPath, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", VClusterConfigFile, err)
		}
	}

	s.notifyFileAction(VClusterConfigFile, existed)

	return nil
}

// buildVClusterAPIServerArgs generates the controlPlane.distro.k8s.apiServer.extraArgs
// YAML fragment for the VCluster values file. When featureGates is true it enables the
// MutatingAdmissionPolicy feature gate / admissionregistration.k8s.io/v1beta1 API
// (required by Calico v3.30+'s CRD chart); OIDC flags are appended when OIDC is
// configured. The whole apiServer block is omitted when neither applies, and a single
// block is emitted otherwise to keep the generated YAML valid. Values are escaped with
// %q to handle YAML-special characters safely. When a CA file is configured, the OIDC CA
// arg references OIDCCAContainerPath and a hostPath volume mount is added (VCluster runs
// on a Docker-based host cluster where the host CA file is already mounted at
// OIDCCAContainerPath on all nodes).
func buildVClusterAPIServerArgs(featureGates bool, oidc *v1alpha1.OIDCSpec) string {
	const (
		featureGatesArg  = "--feature-gates=MutatingAdmissionPolicy=true"
		runtimeConfigArg = "--runtime-config=admissionregistration.k8s.io/v1beta1=true"
	)

	var args []string

	if featureGates {
		args = append(args, featureGatesArg, runtimeConfigArg)
	}

	args = append(args, vClusterOIDCArgs(oidc)...)

	if len(args) == 0 {
		return ""
	}

	var builder strings.Builder

	builder.WriteString("      apiServer:\n        extraArgs:\n")

	for _, arg := range args {
		fmt.Fprintf(&builder, "          - %q\n", arg)
	}

	return builder.String()
}

// vClusterOIDCArgs returns the kube-apiserver OIDC flags for the given config, or nil
// when OIDC is not enabled.
func vClusterOIDCArgs(oidc *v1alpha1.OIDCSpec) []string {
	if oidc == nil || !oidc.Enabled() {
		return nil
	}

	args := []string{
		"--oidc-issuer-url=" + oidc.IssuerURL,
		"--oidc-client-id=" + oidc.ClientID,
	}

	if oidc.UsernameClaim != "" {
		args = append(args, "--oidc-username-claim="+oidc.UsernameClaim)
	}

	if oidc.UsernamePrefix != "" {
		args = append(args, "--oidc-username-prefix="+oidc.UsernamePrefix)
	}

	if oidc.GroupsClaim != "" {
		args = append(args, "--oidc-groups-claim="+oidc.GroupsClaim)
	}

	if oidc.GroupsPrefix != "" {
		args = append(args, "--oidc-groups-prefix="+oidc.GroupsPrefix)
	}

	if oidc.CAFile != "" {
		args = append(args, "--oidc-ca-file="+v1alpha1.OIDCCAContainerPath)
	}

	return args
}

// buildVClusterOIDCVolumes generates the YAML fragment for VCluster volume mounts
// that inject the OIDC CA certificate into the VCluster control plane pod.
// Uses controlPlane.statefulSet.persistence.addVolumeMounts/addVolumes per the
// vCluster Helm values schema. A hostPath volume is used because the host
// Kubernetes node (Kind/K3d) already has the CA file mounted at OIDCCAContainerPath.
func buildVClusterOIDCVolumes() string {
	return "  statefulSet:\n" +
		"    persistence:\n" +
		"      addVolumeMounts:\n" +
		"        - name: oidc-ca\n" +
		"          mountPath: " + v1alpha1.OIDCCAContainerPath + "\n" +
		"          readOnly: true\n" +
		"      addVolumes:\n" +
		"        - name: oidc-ca\n" +
		"          hostPath:\n" +
		"            path: " + v1alpha1.OIDCCAContainerPath + "\n" +
		"            type: File\n"
}
