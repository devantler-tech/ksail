package talos

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
	"sigs.k8s.io/yaml"
)

var (
	errMigrationAlias = errors.New("expand YAML aliases before migrating a multi-document patch")
	errOIDCDeletion   = errors.New(
		"deletion selectors overlap legacy OIDC configuration; consolidate authentication and CA content before migrating",
	)
	errMigrationDeletion = errors.New(
		"multi-key list deletion cannot be normalized safely; move the deletion into a separate strategic-merge patch",
	)
	errRFC6902Migration = errors.New(
		"RFC 6902 patches cannot target Talos 1.14 multi-document configurations; " +
			"convert this patch to strategic-merge Talos configuration documents",
	)
	errMixedOIDCAuthentication = errors.New(
		"legacy OIDC flags overlap KubeAuthenticationConfig; consolidate authentication into one structured configuration",
	)
	errOIDCArgumentType = errors.New("legacy OIDC arguments must be strings")
	errOIDCCAOperation  = errors.New(
		"OIDC CA cannot be resolved from this file operation; provide the complete CA content with create or overwrite",
	)
)

// A record retains application scope and source order while each document is
// migrated independently. The SDK rejects duplicate document kinds in one patch.
type kubernetesPatchDocument struct {
	patch    Patch
	document map[string]any
}

func prepareKubernetesPatchSet(patches []Patch) ([]Patch, error) {
	records, err := decodeKubernetesPatchSet(patches)
	if err != nil {
		return nil, err
	}

	authentication, err := resolvePatchSetOIDC(records)
	if err != nil {
		return nil, err
	}

	prepared := make([]Patch, 0, len(records)+len(authentication))
	for _, record := range records {
		changed := removePatchOIDCArguments(record.document)

		patch := record.patch
		if changed {
			content, marshalErr := yaml.Marshal(record.document)
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal Kubernetes patch %q: %w", patch.Path, marshalErr)
			}

			patch.Content = content
		}

		prepared = append(prepared, patch)
	}

	return append(prepared, authentication...), nil
}

func decodeKubernetesPatchSet(patches []Patch) ([]kubernetesPatchDocument, error) {
	records := make([]kubernetesPatchDocument, 0, len(patches))
	for _, patch := range patches {
		documents, mapDocuments, err := decodeLegacyKubernetesDocuments(patch)
		if err != nil {
			return nil, err
		}

		if !mapDocuments {
			return nil, fmt.Errorf(
				"migrate Kubernetes patch %q: %w",
				patch.Path,
				errRFC6902Migration,
			)
		}

		contents, err := orderedKubernetesDocuments(patch, len(documents))
		if err != nil {
			return nil, err
		}

		for idx, document := range documents {
			if hasLegacyKubernetesValues(document) && ambiguousListDeletion(document) {
				return nil, fmt.Errorf(
					"migrate Kubernetes patch %q: %w",
					patch.Path,
					errMigrationDeletion,
				)
			}

			part := patch
			if len(documents) > 1 {
				part.Content = contents[idx]
			}

			records = append(records, kubernetesPatchDocument{patch: part, document: document})
		}
	}

	return records, nil
}

func resolvePatchSetOIDC(records []kubernetesPatchDocument) ([]Patch, error) {
	var (
		authentication []Patch
		sharedContent  []byte
	)

	for _, scope := range []PatchScope{PatchScopeCluster, PatchScopeControlPlane, PatchScopeWorker} {
		applicable := patchDocumentsForScope(records, scope)

		patch, found, err := resolveScopeOIDC(applicable, scope)
		if err != nil {
			return nil, err
		}

		if !found {
			continue
		}

		if scope == PatchScopeCluster {
			sharedContent = patch.Content
		} else if bytes.Equal(sharedContent, patch.Content) {
			continue
		}

		authentication = append(authentication, patch)
	}

	return authentication, nil
}

// Bundle applies all shared patches before the role patches, even when a runtime
// shared patch was appended after files from control-planes/ or workers/.
func patchDocumentsForScope(
	records []kubernetesPatchDocument,
	scope PatchScope,
) []kubernetesPatchDocument {
	applicable := make([]kubernetesPatchDocument, 0, len(records))
	for _, record := range records {
		if record.patch.Scope == PatchScopeCluster {
			applicable = append(applicable, record)
		}
	}

	if scope != PatchScopeCluster {
		for _, record := range records {
			if record.patch.Scope == scope {
				applicable = append(applicable, record)
			}
		}
	}

	return applicable
}

func resolveScopeOIDC(records []kubernetesPatchDocument, scope PatchScope) (Patch, bool, error) {
	arguments := map[string]any{}
	source := ""
	structured := false

	for _, record := range records {
		if record.document[kindField] == kubeAuthenticationConfigKind {
			structured = true
		}

		local := patchOIDCArguments(record.document)
		if len(local) > 0 {
			source = record.patch.Path

			maps.Copy(arguments, local)
		}
	}

	if len(arguments) == 0 {
		return Patch{}, false, nil
	}

	if structured {
		return Patch{}, false, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			source,
			errMixedOIDCAuthentication,
		)
	}

	err := rejectOIDCDeletions(records)
	if err != nil {
		return Patch{}, false, err
	}

	config, err := resolvedOIDCConfig(arguments, records, source)
	if err != nil {
		return Patch{}, false, fmt.Errorf(
			"resolve OIDC scope %d (shared settings must be complete; "+
				"put role-only settings together in control-planes/ or workers/): %w",
			scope,
			err,
		)
	}

	return Patch{Path: source, Scope: scope, Content: StructuredOIDCPatchYAML(config)}, true, nil
}

func resolvedOIDCConfig(
	arguments map[string]any,
	records []kubernetesPatchDocument,
	source string,
) (OIDCPatchConfig, error) {
	for key, value := range arguments {
		if _, ok := value.(string); !ok {
			return OIDCPatchConfig{}, fmt.Errorf(
				"migrate legacy OIDC patch %q argument %q: %w",
				source,
				key,
				errOIDCArgumentType,
			)
		}
	}

	config := OIDCPatchConfig{
		IssuerURL: popString(
			arguments,
			"oidc-issuer-url",
		),
		ClientID: popString(arguments, "oidc-client-id"),
		UsernameClaim: popString(
			arguments,
			"oidc-username-claim",
		),
		UsernamePrefix: popString(arguments, "oidc-username-prefix"),
		GroupsClaim: popString(
			arguments,
			"oidc-groups-claim",
		),
		GroupsPrefix: popString(arguments, "oidc-groups-prefix"),
	}
	if config.IssuerURL == "" || config.ClientID == "" {
		return OIDCPatchConfig{}, fmt.Errorf(
			"migrate legacy OIDC patch %q: %w",
			source,
			errLegacyOIDCRequiredFields,
		)
	}

	caPath := popString(arguments, "oidc-ca-file")
	if caPath != "" {
		content, err := resolveOIDCCA(records, caPath)
		if err != nil {
			return OIDCPatchConfig{}, fmt.Errorf(
				"migrate legacy OIDC patch %q for %q: %w",
				source,
				caPath,
				err,
			)
		}

		config.CertificateAuthority = content
	}

	return config, nil
}

func patchOIDCArguments(document map[string]any) map[string]any {
	cluster, _ := mapValue(document, "cluster")
	apiServer, _ := mapValue(cluster, "apiServer")
	arguments, _ := mapValue(apiServer, "extraArgs")
	oidc := map[string]any{}

	for key, value := range arguments {
		if supportedOIDCArgument(key) {
			oidc[key] = value
		}
	}

	return oidc
}

func supportedOIDCArgument(key string) bool {
	switch key {
	case "oidc-issuer-url",
		"oidc-client-id",
		"oidc-username-claim",
		"oidc-username-prefix",
		"oidc-groups-claim",
		"oidc-groups-prefix",
		"oidc-ca-file":
		return true
	default:
		return false
	}
}

func removePatchOIDCArguments(document map[string]any) bool {
	cluster, _ := mapValue(document, "cluster")
	apiServer, _ := mapValue(cluster, "apiServer")
	arguments, _ := mapValue(apiServer, "extraArgs")
	changed := false

	for key := range arguments {
		if supportedOIDCArgument(key) {
			delete(arguments, key)

			changed = true
		}
	}

	return changed
}

func resolveOIDCCA(records []kubernetesPatchDocument, path string) (string, error) {
	content := ""

	for _, record := range records {
		machine, _ := mapValue(record.document, "machine")
		if machine["$patch"] != nil || containsDeletion(machine["files"]) {
			return "", fmt.Errorf("CA patch %q: %w", record.patch.Path, errOIDCDeletion)
		}

		files, _ := machine["files"].([]any)
		for _, item := range files {
			file, ok := item.(map[string]any)
			if !ok || file["path"] != path {
				continue
			}

			if !literalCAOperation(file) {
				return "", fmt.Errorf("CA patch %q: %w", record.patch.Path, errOIDCCAOperation)
			}

			content, _ = file["content"].(string)
		}
	}

	if strings.TrimSpace(content) == "" {
		return "", errLegacyOIDCCAMissing
	}

	return content, nil
}

func nonemptyKubernetesPatches(patches []Patch) []Patch {
	nonempty := patches[:0]
	for _, patch := range patches {
		if len(bytes.TrimSpace(patch.Content)) > 0 {
			nonempty = append(nonempty, patch)
		}
	}

	return nonempty
}

// Keep YAML key order when splitting documents: Talos deletion selectors can
// depend on the first field of a list entry.
func orderedKubernetesDocuments(patch Patch, count int) ([][]byte, error) {
	if count <= 1 {
		return nil, nil
	}

	decoder := yamlv3.NewDecoder(bytes.NewReader(patch.Content))
	contents := make([][]byte, 0, count)

	for {
		var document yamlv3.Node

		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return contents, nil
		}

		if err != nil {
			return nil, fmt.Errorf("decode Kubernetes patch %q: %w", patch.Path, err)
		}

		if containsYAMLAlias(&document) {
			return nil, fmt.Errorf("migrate Kubernetes patch %q: %w", patch.Path, errMigrationAlias)
		}

		if len(document.Content) == 0 || document.Content[0].Kind != yamlv3.MappingNode ||
			len(document.Content[0].Content) == 0 {
			continue
		}

		content, err := yamlv3.Marshal(&document)
		if err != nil {
			return nil, fmt.Errorf("encode Kubernetes patch %q: %w", patch.Path, err)
		}

		contents = append(contents, content)
	}
}

func rejectOIDCDeletions(records []kubernetesPatchDocument) error {
	for _, record := range records {
		cluster, _ := mapValue(record.document, "cluster")

		apiServer, _ := mapValue(cluster, "apiServer")
		if cluster["$patch"] != nil || apiServer["$patch"] != nil ||
			containsDeletion(apiServer["extraArgs"]) {
			return fmt.Errorf("OIDC patch %q: %w", record.patch.Path, errOIDCDeletion)
		}
	}

	return nil
}

func containsDeletion(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if typed["$patch"] != nil {
			return true
		}

		for _, child := range typed {
			if containsDeletion(child) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(typed, containsDeletion) {
			return true
		}
	}

	return false
}

func ambiguousListDeletion(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if ambiguousListDeletion(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if entry, ok := child.(map[string]any); ok && entry["$patch"] != nil && len(entry) > 2 {
				return true
			}

			if ambiguousListDeletion(child) {
				return true
			}
		}
	}

	return false
}

func hasLegacyKubernetesValues(document map[string]any) bool {
	cluster, _ := mapValue(document, "cluster")
	if _, found := mapValue(cluster, "apiServer"); found {
		return true
	}

	network, _ := mapValue(cluster, "network")
	cni, _ := mapValue(network, "cni")

	return cni["name"] == "none"
}

func literalCAOperation(file map[string]any) bool {
	operation, _ := file["op"].(string)
	// Preserve legacy migration of omitted operations. Append requires machine
	// state, which is unavailable while generating configuration.
	return (operation == "" || operation == "create" || operation == "overwrite") &&
		file["$patch"] == nil
}

func containsYAMLAlias(node *yamlv3.Node) bool {
	if node.Kind == yamlv3.AliasNode {
		return true
	}

	for _, child := range node.Content {
		if containsYAMLAlias(child) {
			return true
		}
	}

	return false
}
