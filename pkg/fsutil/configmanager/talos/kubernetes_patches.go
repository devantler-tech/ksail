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
	errLegacyAPIServerMultipleDocuments = errors.New(
		"multiple legacy cluster.apiServer documents are not supported",
	)
	errLegacyOIDCRequiredFields        = errors.New("issuer URL and client ID are required")
	errLegacyOIDCCAMissing             = errors.New("CA content is missing")
	errLegacyOIDCUnsupportedExtraArg   = errors.New("unsupported legacy OIDC extra argument")
	errLegacyUnsupportedAPIServerField = errors.New(
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
			content, found, migrationErr := migrateLegacyAPIServerPatch(migrated[idx])
			if migrationErr != nil {
				return nil, migrationErr
			}

			if found {
				migrated[idx].Content = content
			}
		}
	}

	return migrated, nil
}

type legacyAPIServerPatchValues struct {
	documents       []map[string]any
	clusterDocument map[string]any
	cluster         map[string]any
	apiServer       map[string]any
	extraArgs       map[string]any
}

func migrateLegacyAPIServerPatch(patch Patch) ([]byte, bool, error) {
	documents, mapDocuments, err := decodeLegacyKubernetesDocuments(patch)
	if err != nil {
		return nil, false, err
	}

	if !mapDocuments {
		return patch.Content, false, nil
	}

	values, found, err := findLegacyAPIServerValues(documents)
	if err != nil {
		return nil, false, fmt.Errorf(
			"migrate legacy API server patch %q: %w",
			patch.Path,
			err,
		)
	}

	if !found {
		return patch.Content, false, nil
	}

	authenticationDocument, err := values.migrateOIDCAuthenticationDocument(patch.Path)
	if err != nil {
		return nil, false, err
	}

	apiServerDocument, err := values.migrateAPIServerDocument(patch.Path)
	if err != nil {
		return nil, false, err
	}

	values.removeMigratedValues()

	content, err := marshalMigratedKubernetesDocuments(
		patch.Path,
		documents,
		apiServerDocument,
		authenticationDocument,
	)
	if err != nil {
		return nil, false, err
	}

	return content, true, nil
}

func decodeLegacyKubernetesDocuments(patch Patch) ([]map[string]any, bool, error) {
	decoder := yamlv3.NewDecoder(bytes.NewReader(patch.Content))
	documents := []map[string]any{}

	for {
		var value any

		decodeErr := decoder.Decode(&value)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		if decodeErr != nil {
			return nil, false, fmt.Errorf(
				"decode legacy Kubernetes patch %q: %w",
				patch.Path,
				decodeErr,
			)
		}

		if value == nil {
			continue
		}

		document, isMap := value.(map[string]any)
		if !isMap {
			return nil, false, nil
		}

		if len(document) > 0 {
			documents = append(documents, document)
		}
	}

	return documents, true, nil
}

func findLegacyAPIServerValues(
	documents []map[string]any,
) (*legacyAPIServerPatchValues, bool, error) {
	var values *legacyAPIServerPatchValues

	for _, document := range documents {
		cluster, found := mapValue(document, "cluster")
		if !found {
			continue
		}

		apiServer, found := mapValue(cluster, "apiServer")
		if !found {
			continue
		}

		extraArgs, _ := mapValue(apiServer, "extraArgs")

		if values != nil {
			return nil, false, errLegacyAPIServerMultipleDocuments
		}

		values = &legacyAPIServerPatchValues{
			documents:       documents,
			clusterDocument: document,
			cluster:         cluster,
			apiServer:       apiServer,
			extraArgs:       extraArgs,
		}
	}

	return values, values != nil, nil
}

func (values *legacyAPIServerPatchValues) migrateOIDCAuthenticationDocument(
	patchPath string,
) ([]byte, error) {
	var authenticationDocument []byte

	if _, hasOIDCIssuer := values.extraArgs["oidc-issuer-url"]; hasOIDCIssuer {
		config, err := values.oidcConfig(patchPath)
		if err != nil {
			return nil, err
		}

		authenticationDocument = StructuredOIDCPatchYAML(config)
	}

	if extraArg := unsupportedLegacyOIDCExtraArg(values.extraArgs); extraArg != "" {
		return nil, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w %q",
			patchPath,
			errLegacyOIDCUnsupportedExtraArg,
			extraArg,
		)
	}

	return authenticationDocument, nil
}

func (values *legacyAPIServerPatchValues) oidcConfig(patchPath string) (OIDCPatchConfig, error) {
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

func unsupportedLegacyOIDCExtraArg(extraArgs map[string]any) string {
	unsupported := ""

	for extraArg := range extraArgs {
		if !strings.HasPrefix(extraArg, "oidc-") {
			continue
		}

		if unsupported == "" || extraArg < unsupported {
			unsupported = extraArg
		}
	}

	return unsupported
}

func (values *legacyAPIServerPatchValues) migrateAPIServerDocument(
	patchPath string,
) (map[string]any, error) {
	if extraArgs, found := mapValue(values.apiServer, "extraArgs"); found {
		removeEmptyMap(values.apiServer, "extraArgs", extraArgs)
	}

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
				"migrate legacy API server patch %q field %q: %w",
				patchPath,
				field,
				errLegacyUnsupportedAPIServerField,
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

func (values *legacyAPIServerPatchValues) removeMigratedValues() {
	delete(values.cluster, "apiServer")
	removeEmptyMap(values.clusterDocument, "cluster", values.cluster)
}

func marshalMigratedKubernetesDocuments(
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
			return nil, fmt.Errorf("marshal migrated Kubernetes patch %q: %w", patchPath, err)
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

	if len(authenticationDocument) > 0 {
		documents = append(documents, bytes.TrimSpace(authenticationDocument))
	}

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
