package talos

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"sigs.k8s.io/yaml"
)

var (
	errLegacyOIDCClusterMissing   = errors.New("cluster is missing")
	errLegacyOIDCAPIServerMissing = errors.New("cluster.apiServer is missing")
	errLegacyOIDCExtraArgsMissing = errors.New("cluster.apiServer.extraArgs is missing")
	errLegacyOIDCRequiredFields   = errors.New("issuer URL and client ID are required")
	errLegacyOIDCCAMissing        = errors.New("CA content is missing")
)

const (
	legacyDisableDefaultCNIPatchYAML = `cluster:
  network:
    cni:
      name: none
`
	multiDocumentDisableDefaultCNIPatchYAML = `apiVersion: v1alpha1
kind: KubeFlannelCNIConfig
$patch: delete
`
	legacyAPIServerFeatureGatesPatchYAML = `cluster:
  apiServer:
    extraArgs:
      feature-gates: MutatingAdmissionPolicy=true
      runtime-config: admissionregistration.k8s.io/v1beta1=true
`
	multiDocumentAPIServerFeatureGatesPatchYAML = `apiVersion: v1alpha1
kind: KubeAPIServerConfig
extraArgs:
  feature-gates: MutatingAdmissionPolicy=true
  runtime-config: admissionregistration.k8s.io/v1beta1=true
`
)

// OIDCPatchConfig contains the legacy OIDC flag values that map to a
// KubeAuthenticationConfig document in Talos 1.14.
type OIDCPatchConfig struct {
	IssuerURL            string
	ClientID             string
	UsernameClaim        string
	UsernamePrefix       string
	GroupsClaim          string
	GroupsPrefix         string
	CertificateAuthority string
}

// DisableDefaultCNIPatchYAML returns the version-appropriate Talos patch for
// disabling the built-in Flannel CNI.
func DisableDefaultCNIPatchYAML(multiDocument bool) string {
	if multiDocument {
		return multiDocumentDisableDefaultCNIPatchYAML
	}

	return legacyDisableDefaultCNIPatchYAML
}

// StructuredOIDCPatchYAML maps the legacy kube-apiserver OIDC flags to the
// structured authentication document required by Talos 1.14.
func StructuredOIDCPatchYAML(config OIDCPatchConfig) []byte {
	usernameClaim := config.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "sub"
	}

	usernamePrefix := config.UsernamePrefix
	if usernamePrefix == "-" {
		usernamePrefix = ""
	} else if usernamePrefix == "" && usernameClaim != "email" {
		usernamePrefix = config.IssuerURL + "#"
	}

	var builder strings.Builder
	builder.WriteString("apiVersion: v1alpha1\n")
	builder.WriteString("kind: KubeAuthenticationConfig\n")
	builder.WriteString("configuration:\n")
	builder.WriteString("  apiVersion: apiserver.config.k8s.io/v1beta1\n")
	builder.WriteString("  kind: AuthenticationConfiguration\n")
	builder.WriteString("  anonymous:\n")
	builder.WriteString("    enabled: true\n")
	builder.WriteString("    conditions:\n")
	builder.WriteString("      - path: /livez\n")
	builder.WriteString("      - path: /readyz\n")
	builder.WriteString("      - path: /healthz\n")
	builder.WriteString("  jwt:\n")
	builder.WriteString("    - issuer:\n")
	_, _ = fmt.Fprintf(&builder, "        url: %q\n", config.IssuerURL)
	builder.WriteString("        audiences:\n")
	_, _ = fmt.Fprintf(&builder, "          - %q\n", config.ClientID)

	if config.CertificateAuthority != "" {
		builder.WriteString("        certificateAuthority: |\n")

		for line := range strings.SplitSeq(
			strings.TrimRight(config.CertificateAuthority, "\n"),
			"\n",
		) {
			_, _ = fmt.Fprintf(&builder, "          %s\n", line)
		}
	}

	builder.WriteString("      claimMappings:\n")
	builder.WriteString("        username:\n")
	_, _ = fmt.Fprintf(&builder, "          claim: %q\n", usernameClaim)
	_, _ = fmt.Fprintf(&builder, "          prefix: %q\n", usernamePrefix)

	if config.GroupsClaim != "" {
		builder.WriteString("        groups:\n")
		_, _ = fmt.Fprintf(&builder, "          claim: %q\n", config.GroupsClaim)
		_, _ = fmt.Fprintf(&builder, "          prefix: %q\n", config.GroupsPrefix)
	}

	return []byte(builder.String())
}

func migrateKubernetesPatchesForContract(
	patches []Patch,
	versionContract *talosconfig.VersionContract,
) ([]Patch, error) {
	if versionContract == nil || !versionContract.MultidocKubernetesConfigSupported() {
		return patches, nil
	}

	migrated := make([]Patch, len(patches))
	copy(migrated, patches)

	for idx := range migrated {
		content := strings.TrimSpace(string(migrated[idx].Content))

		switch content {
		case strings.TrimSpace(legacyDisableDefaultCNIPatchYAML):
			migrated[idx].Content = []byte(multiDocumentDisableDefaultCNIPatchYAML)
		case strings.TrimSpace(legacyAPIServerFeatureGatesPatchYAML):
			migrated[idx].Content = []byte(multiDocumentAPIServerFeatureGatesPatchYAML)
		default:
			if bytes.Contains(migrated[idx].Content, []byte("oidc-issuer-url:")) {
				content, migrationErr := migrateLegacyOIDCPatch(migrated[idx])
				if migrationErr != nil {
					return nil, migrationErr
				}

				migrated[idx].Content = content
			}
		}
	}

	return migrated, nil
}

type legacyOIDCPatchValues struct {
	document  map[string]any
	cluster   map[string]any
	apiServer map[string]any
	extraArgs map[string]any
}

func migrateLegacyOIDCPatch(patch Patch) ([]byte, error) {
	values, err := parseLegacyOIDCPatch(patch)
	if err != nil {
		return nil, err
	}

	config, err := values.oidcConfig(patch.Path)
	if err != nil {
		return nil, err
	}

	values.removeMigratedValues()

	structured := StructuredOIDCPatchYAML(config)
	if len(values.document) == 0 {
		return structured, nil
	}

	legacyRemainder, err := yaml.Marshal(values.document)
	if err != nil {
		return nil, fmt.Errorf("marshal migrated OIDC patch %q: %w", patch.Path, err)
	}

	return bytes.Join([][]byte{
		bytes.TrimSpace(legacyRemainder),
		structured,
	}, []byte("\n---\n")), nil
}

func parseLegacyOIDCPatch(patch Patch) (*legacyOIDCPatchValues, error) {
	var document map[string]any

	err := yaml.Unmarshal(patch.Content, &document)
	if err != nil {
		return nil, fmt.Errorf("migrate legacy OIDC patch %q: %w", patch.Path, err)
	}

	cluster, found := mapValue(document, "cluster")
	if !found {
		return nil, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			patch.Path,
			errLegacyOIDCClusterMissing,
		)
	}

	apiServer, found := mapValue(cluster, "apiServer")
	if !found {
		return nil, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			patch.Path,
			errLegacyOIDCAPIServerMissing,
		)
	}

	extraArgs, found := mapValue(apiServer, "extraArgs")
	if !found {
		return nil, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			patch.Path,
			errLegacyOIDCExtraArgsMissing,
		)
	}

	return &legacyOIDCPatchValues{
		document:  document,
		cluster:   cluster,
		apiServer: apiServer,
		extraArgs: extraArgs,
	}, nil
}

func (values *legacyOIDCPatchValues) oidcConfig(patchPath string) (OIDCPatchConfig, error) {
	config := OIDCPatchConfig{
		IssuerURL:      popString(values.extraArgs, "oidc-issuer-url"),
		ClientID:       popString(values.extraArgs, "oidc-client-id"),
		UsernameClaim:  popString(values.extraArgs, "oidc-username-claim"),
		UsernamePrefix: popString(values.extraArgs, "oidc-username-prefix"),
		GroupsClaim:    popString(values.extraArgs, "oidc-groups-claim"),
		GroupsPrefix:   popString(values.extraArgs, "oidc-groups-prefix"),
	}
	caPath := popString(values.extraArgs, "oidc-ca-file")

	if config.IssuerURL == "" || config.ClientID == "" {
		return OIDCPatchConfig{}, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			patchPath,
			errLegacyOIDCRequiredFields,
		)
	}

	if caPath != "" {
		config.CertificateAuthority = machineFileContent(values.document, caPath)
		if config.CertificateAuthority == "" {
			return OIDCPatchConfig{}, fmt.Errorf(
				"migrate legacy OIDC patch %q for %q: %w",
				patchPath,
				caPath,
				errLegacyOIDCCAMissing,
			)
		}
	}

	return config, nil
}

func (values *legacyOIDCPatchValues) removeMigratedValues() {
	removeEmptyMap(values.apiServer, "extraArgs", values.extraArgs)
	removeEmptyMap(values.cluster, "apiServer", values.apiServer)
	removeEmptyMap(values.document, "cluster", values.cluster)
}

func mapValue(parent map[string]any, key string) (map[string]any, bool) {
	value, found := parent[key]
	if !found {
		return nil, false
	}

	result, isMap := value.(map[string]any)

	return result, isMap
}

func popString(values map[string]any, key string) string {
	value, found := values[key]
	if !found {
		return ""
	}

	delete(values, key)

	result, _ := value.(string)

	return result
}

func removeEmptyMap(parent map[string]any, key string, value map[string]any) {
	if len(value) == 0 {
		delete(parent, key)
	}
}

func machineFileContent(document map[string]any, path string) string {
	machine, found := mapValue(document, "machine")
	if !found {
		return ""
	}

	files, found := machine["files"].([]any)
	if !found {
		return ""
	}

	for _, item := range files {
		file, isMap := item.(map[string]any)
		if !isMap {
			continue
		}

		filePath, _ := file["path"].(string)
		if filePath != path {
			continue
		}

		content, _ := file["content"].(string)

		return content
	}

	return ""
}
