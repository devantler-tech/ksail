package tenant

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"sigs.k8s.io/yaml"
)

// errFoundRBACCM is a sentinel used to stop YAML iteration once the
// argocd-rbac-cm ConfigMap is found; it is never returned to callers.
var errFoundRBACCM = errors.New("argocd-rbac-cm found")

const rbacCMFilePermissions = 0o600

// FindArgoCDRBACCM scans YAML files in the given directory for a ConfigMap
// named "argocd-rbac-cm" (apiVersion: v1, kind: ConfigMap).
// Returns the file path if found, or empty string if not found.
// Supports multi-document YAML files separated by "---".
//
// The scan uses fsutil.ForEachYAMLFile, which canonicalizes the directory and
// confines reads to it, so the returned path is symlink-safe.
func FindArgoCDRBACCM(dir string) (string, error) {
	var found string

	err := fsutil.ForEachYAMLFile(dir, func(filePath string, content []byte) error {
		if containsArgoCDRBACCM(content) {
			found = filePath

			return errFoundRBACCM
		}

		return nil
	})
	if err != nil && !errors.Is(err, errFoundRBACCM) {
		return "", fmt.Errorf("scanning %s for %s: %w", dir, rbacConfigMapName, err)
	}

	return found, nil
}

// containsArgoCDRBACCM checks whether YAML data (possibly multi-document)
// contains an argocd-rbac-cm ConfigMap. As a read-only detection caller it uses
// the lossy fsutil.SplitYAMLDocuments.
func containsArgoCDRBACCM(data []byte) bool {
	for _, doc := range fsutil.SplitYAMLDocuments(data) {
		var raw map[string]any

		err := yaml.Unmarshal(doc, &raw)
		if err != nil {
			continue
		}

		if isArgoCDRBACConfigMap(raw) {
			return true
		}
	}

	return false
}

// isArgoCDRBACConfigMap reports whether a decoded YAML document is the
// argocd-rbac-cm ConfigMap, matching on apiVersion, kind, and name.
func isArgoCDRBACConfigMap(raw map[string]any) bool {
	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)

	meta, _ := raw["metadata"].(map[string]any)
	name, _ := meta["name"].(string)

	return apiVersion == "v1" && kind == configMapKind && name == rbacConfigMapName
}

// MergeArgoCDRBACPolicyFile reads an existing argocd-rbac-cm file (or creates a new one)
// and merges the tenant's RBAC policy into it. Multi-document files are handled by
// rewriting only the matching ConfigMap document and reassembling the file.
func MergeArgoCDRBACPolicyFile(rbacCMPath, tenantName string) error {
	canonPath, err := fsutil.EvalCanonicalPath(rbacCMPath)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", rbacCMPath, err)
	}

	existingContent := []byte(nil)

	data, readErr := os.ReadFile(canonPath) //nolint:gosec // canonicalized above
	switch {
	case readErr == nil:
		existingContent = data
	case !os.IsNotExist(readErr):
		return fmt.Errorf("reading %s: %w", canonPath, readErr)
	}

	merged, err := mergeRBACPolicyIntoContent(existingContent, tenantName)
	if err != nil {
		return fmt.Errorf("merging RBAC policy: %w", err)
	}

	return writeRBACCMFile(canonPath, merged)
}

// RemoveArgoCDRBACPolicyFile reads an existing argocd-rbac-cm file and removes
// the tenant's RBAC policy from it. No-op if the file does not exist.
// Multi-document files are handled by rewriting only the matching ConfigMap
// document and reassembling the file.
func RemoveArgoCDRBACPolicyFile(rbacCMPath, tenantName string) error {
	canonPath, err := fsutil.EvalCanonicalPath(rbacCMPath)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", rbacCMPath, err)
	}

	data, readErr := os.ReadFile(canonPath) //nolint:gosec // canonicalized above
	if os.IsNotExist(readErr) {
		return nil
	}

	if readErr != nil {
		return fmt.Errorf("reading %s: %w", canonPath, readErr)
	}

	result, err := removeRBACPolicyFromContent(data, tenantName)
	if err != nil {
		return fmt.Errorf("removing RBAC policy: %w", err)
	}

	return writeRBACCMFile(canonPath, result)
}

// mergeRBACPolicyIntoContent merges a tenant's policy into argocd-rbac-cm content,
// handling multi-document YAML files by rewriting only the matching ConfigMap
// document and reassembling the file. Empty content produces a fresh ConfigMap.
func mergeRBACPolicyIntoContent(content []byte, tenantName string) ([]byte, error) {
	return transformRBACDocument(content, tenantName, MergeArgoCDRBACPolicy)
}

// removeRBACPolicyFromContent removes a tenant's policy from argocd-rbac-cm
// content, handling multi-document YAML files by rewriting only the matching
// ConfigMap document and reassembling the file.
func removeRBACPolicyFromContent(content []byte, tenantName string) ([]byte, error) {
	return transformRBACDocument(content, tenantName, RemoveArgoCDRBACPolicy)
}

// transformRBACDocument applies a content-level policy transform to the
// argocd-rbac-cm document. Single-document content (or empty content) is
// transformed as a whole; multi-document content is split, the matching
// ConfigMap document is rewritten in place, and the file is reassembled with the
// original "\n---" separators and leading whitespace preserved.
func transformRBACDocument(
	content []byte,
	tenantName string,
	transform func(existingContent, tenantName string) (string, error),
) ([]byte, error) {
	docs := splitRBACDocuments(content)
	if len(docs) <= 1 {
		result, err := transform(string(content), tenantName)
		if err != nil {
			return nil, err
		}

		return []byte(result), nil
	}

	for docIdx, doc := range docs {
		trimmed := bytes.TrimSpace(doc)
		if len(trimmed) == 0 {
			continue
		}

		if !isRBACConfigMapDoc(trimmed) {
			continue
		}

		updated, err := transformRBACConfigMapDoc(doc, trimmed, tenantName, transform)
		if err != nil {
			return nil, err
		}

		docs[docIdx] = updated

		break
	}

	return bytes.Join(docs, []byte("\n---")), nil
}

// transformRBACConfigMapDoc applies the policy transform to a single YAML
// document while preserving leading whitespace from the original split.
func transformRBACConfigMapDoc(
	originalDoc, trimmedDoc []byte,
	tenantName string,
	transform func(existingContent, tenantName string) (string, error),
) ([]byte, error) {
	docStr := string(originalDoc)

	prefix := ""
	if idx := strings.IndexFunc(
		docStr,
		func(r rune) bool { return r != '\n' && r != '\r' },
	); idx > 0 {
		prefix = docStr[:idx]
	}

	updated, err := transform(string(trimmedDoc), tenantName)
	if err != nil {
		return nil, err
	}

	return []byte(prefix + updated), nil
}

// splitRBACDocuments splits YAML content into individual documents for in-place
// rewriting, preserving the raw bytes so the file can be reassembled with the
// original "\n---" separators. Files that start with a leading "---" separator
// are normalized so the first document is not lost.
func splitRBACDocuments(content []byte) [][]byte {
	if bytes.HasPrefix(content, []byte("---")) {
		content = append([]byte("\n"), content...)
	}

	return bytes.Split(content, []byte("\n---"))
}

// isRBACConfigMapDoc checks if a single YAML document is a ConfigMap
// with metadata.name "argocd-rbac-cm".
func isRBACConfigMapDoc(data []byte) bool {
	var resource struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}

	err := yaml.Unmarshal(data, &resource)
	if err != nil {
		return false
	}

	return resource.Kind == configMapKind && resource.Metadata.Name == rbacConfigMapName
}

// writeRBACCMFile writes content to an argocd-rbac-cm file, preserving
// existing file permissions when the file already exists.
// The path is canonicalized to prevent symlink-based path traversal.
func writeRBACCMFile(path string, content []byte) error {
	safePath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", path, err)
	}

	perm := os.FileMode(rbacCMFilePermissions)

	info, statErr := os.Stat(safePath)
	if statErr == nil {
		perm = info.Mode().Perm()
	}

	//nolint:gosec // safePath is canonicalized via fsutil.EvalCanonicalPath
	err = os.WriteFile(safePath, content, perm)
	if err != nil {
		return fmt.Errorf("writing %s: %w", safePath, err)
	}

	return nil
}
