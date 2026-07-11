package talos

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	yamlv3 "gopkg.in/yaml.v3"
	"sigs.k8s.io/yaml"
)

var (
	errLegacyOIDCExtraArgsMissing = errors.New(
		"cluster.apiServer.extraArgs with oidc-issuer-url is missing",
	)
	errLegacyOIDCRequiredFields            = errors.New("issuer URL and client ID are required")
	errLegacyOIDCCAMissing                 = errors.New("CA content is missing")
	errLegacyOIDCUnsupportedAPIServerField = errors.New(
		"API server field has no Talos 1.14 KubeAPIServerConfig equivalent",
	)
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
	documents       []map[string]any
	clusterDocument map[string]any
	cluster         map[string]any
	apiServer       map[string]any
	extraArgs       map[string]any
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

	apiServerDocument, err := values.migrateAPIServerDocument(patch.Path)
	if err != nil {
		return nil, err
	}

	values.removeMigratedValues()

	return marshalMigratedOIDCDocuments(
		patch.Path,
		values.documents,
		apiServerDocument,
		StructuredOIDCPatchYAML(config),
	)
}

func parseLegacyOIDCPatch(patch Patch) (*legacyOIDCPatchValues, error) {
	documents, err := decodeLegacyOIDCDocuments(patch)
	if err != nil {
		return nil, err
	}

	values, found := findLegacyOIDCValues(documents)
	if !found {
		return nil, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			patch.Path,
			errLegacyOIDCExtraArgsMissing,
		)
	}

	return values, nil
}

func decodeLegacyOIDCDocuments(patch Patch) ([]map[string]any, error) {
	decoder := yamlv3.NewDecoder(bytes.NewReader(patch.Content))
	documents := []map[string]any{}

	for {
		var document map[string]any

		decodeErr := decoder.Decode(&document)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		if decodeErr != nil {
			return nil, fmt.Errorf("decode legacy OIDC patch %q: %w", patch.Path, decodeErr)
		}

		if len(document) > 0 {
			documents = append(documents, document)
		}
	}

	return documents, nil
}

func findLegacyOIDCValues(documents []map[string]any) (*legacyOIDCPatchValues, bool) {
	for _, document := range documents {
		cluster, found := mapValue(document, "cluster")
		if !found {
			continue
		}

		apiServer, found := mapValue(cluster, "apiServer")
		if !found {
			continue
		}

		extraArgs, found := mapValue(apiServer, "extraArgs")
		if !found {
			continue
		}

		if _, found = extraArgs["oidc-issuer-url"]; !found {
			continue
		}

		return &legacyOIDCPatchValues{
			documents:       documents,
			clusterDocument: document,
			cluster:         cluster,
			apiServer:       apiServer,
			extraArgs:       extraArgs,
		}, true
	}

	return nil, false
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
		config.CertificateAuthority = machineFileContent(values.documents, caPath)
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

func (values *legacyOIDCPatchValues) migrateAPIServerDocument(
	patchPath string,
) (map[string]any, error) {
	removeEmptyMap(values.apiServer, "extraArgs", values.extraArgs)

	if len(values.apiServer) == 0 {
		return map[string]any{}, nil
	}

	document := map[string]any{
		"apiVersion": "v1alpha1",
		"kind":       "KubeAPIServerConfig",
	}

	for field, value := range values.apiServer {
		newField, supported := multiDocumentAPIServerFieldName(field)
		if !supported {
			return nil, fmt.Errorf(
				"migrate legacy OIDC patch %q field %q: %w",
				patchPath,
				field,
				errLegacyOIDCUnsupportedAPIServerField,
			)
		}

		document[newField] = value
	}

	return document, nil
}

func multiDocumentAPIServerFieldName(field string) (string, bool) {
	switch field {
	case "image", "extraArgs", "env", "resources":
		return field, true
	case "certSANs":
		return "certExtraSANs", true
	default:
		return "", false
	}
}

func (values *legacyOIDCPatchValues) removeMigratedValues() {
	delete(values.cluster, "apiServer")
	removeEmptyMap(values.clusterDocument, "cluster", values.cluster)
}

func marshalMigratedOIDCDocuments(
	patchPath string,
	legacyDocuments []map[string]any,
	apiServerDocument map[string]any,
	authenticationDocument []byte,
) ([]byte, error) {
	documents := make([][]byte, 0, len(legacyDocuments))

	for _, document := range legacyDocuments {
		if len(document) == 0 {
			continue
		}

		encoded, err := yaml.Marshal(document)
		if err != nil {
			return nil, fmt.Errorf("marshal migrated OIDC patch %q: %w", patchPath, err)
		}

		documents = append(documents, bytes.TrimSpace(encoded))
	}

	if len(apiServerDocument) > 0 {
		encoded, err := yaml.Marshal(apiServerDocument)
		if err != nil {
			return nil, fmt.Errorf("marshal migrated API server patch %q: %w", patchPath, err)
		}

		documents = append(documents, bytes.TrimSpace(encoded))
	}

	documents = append(documents, bytes.TrimSpace(authenticationDocument))

	return append(bytes.Join(documents, []byte("\n---\n")), '\n'), nil
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

func machineFileContent(documents []map[string]any, path string) string {
	for _, document := range documents {
		if content := machineFileContentInDocument(document, path); content != "" {
			return content
		}
	}

	return ""
}

func machineFileContentInDocument(document map[string]any, path string) string {
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
