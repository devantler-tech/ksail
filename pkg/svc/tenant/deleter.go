package tenant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

// DeleteOptions holds configuration for tenant deletion.
type DeleteOptions struct {
	// Name is the tenant name (required).
	Name string
	// OutputDir is the directory containing tenant subdirectories.
	OutputDir string
	// Unregister removes the tenant from kustomization.yaml resources.
	Unregister bool
	// KustomizationPath is the explicit path to kustomization.yaml.
	KustomizationPath string
	// DeleteRepo deletes the tenant's Git repository.
	DeleteRepo bool
	// GitProvider is the Git provider name (github, gitlab, gitea).
	GitProvider string
	// TenantRepo is the tenant repo as owner/repo-name.
	TenantRepo string
	// GitToken is the Git provider API token.
	GitToken string
}

// Delete removes a tenant's manifests and optionally unregisters and deletes the repo.
func Delete(ctx context.Context, opts DeleteOptions) error {
	err := validateDeleteOpts(opts)
	if err != nil {
		return err
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	_, statErr := os.Stat(tenantDir)
	if os.IsNotExist(statErr) {
		return fmt.Errorf("%w: %q", ErrTenantDirNotExist, tenantDir)
	}

	// Detect ArgoCD tenant before deletion so we can clean up RBAC.
	argoCD := isArgoCDTenant(tenantDir)

	if opts.Unregister {
		unregErr := UnregisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath)
		if unregErr != nil {
			if !errors.Is(unregErr, ErrKustomizationNotFound) {
				return fmt.Errorf("unregister tenant: %w", unregErr)
			}
		}
	}

	if argoCD {
		rbacErr := cleanupArgoCDRBAC(opts.OutputDir, opts.Name)
		if rbacErr != nil {
			return fmt.Errorf("cleanup ArgoCD RBAC: %w", rbacErr)
		}
	}

	removeErr := os.RemoveAll(tenantDir)
	if removeErr != nil {
		return fmt.Errorf("remove tenant directory: %w", removeErr)
	}

	if opts.DeleteRepo {
		return deleteRepo(ctx, opts)
	}

	return nil
}

func validateDeleteOpts(opts DeleteOptions) error {
	if opts.Name == "" {
		return ErrTenantNameRequired
	}

	if errs := validation.IsDNS1123Label(opts.Name); len(errs) > 0 {
		return fmt.Errorf(
			"%w: %s (%s)",
			ErrInvalidTenantName,
			opts.Name,
			strings.Join(errs, "; "),
		)
	}

	if opts.DeleteRepo {
		if opts.GitProvider == "" {
			return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
		}

		if opts.TenantRepo == "" {
			return fmt.Errorf("%w", ErrDeleteRepoTenantRepoRequired)
		}
	}

	return nil
}

func deleteRepo(ctx context.Context, opts DeleteOptions) error {
	if opts.GitProvider == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
	}

	if opts.TenantRepo == "" {
		return fmt.Errorf("%w", ErrDeleteRepoTenantRepoRequired)
	}

	owner, repo, err := gitprovider.ParseOwnerRepo(opts.TenantRepo)
	if err != nil {
		return fmt.Errorf("parse tenant-repo: %w", err)
	}

	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)

	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return fmt.Errorf("create git provider: %w", err)
	}

	err = provider.DeleteRepo(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("delete git repo: %w", err)
	}

	return nil
}

// isArgoCDTenant checks whether the tenant directory contains an ArgoCD AppProject
// (project.yaml), indicating this is an ArgoCD-managed tenant.
func isArgoCDTenant(tenantDir string) bool {
	projectPath := filepath.Join(tenantDir, "project.yaml")

	_, err := os.Stat(projectPath)

	return err == nil
}

// cleanupArgoCDRBAC removes the tenant's policy lines from the argocd-rbac-cm ConfigMap.
// The ConfigMap is discovered by scanning YAML files in outputDir for a Kubernetes ConfigMap
// with metadata.name "argocd-rbac-cm" (content-based, not filename-based).
// Handles multi-document YAML files where the ConfigMap may not be the first document.
// Skips silently if no matching file is found; propagates real I/O errors.
func cleanupArgoCDRBAC(outputDir, tenantName string) error {
	rbacPath, err := findRBACConfigMapFile(outputDir)
	if err != nil {
		if errors.Is(err, ErrRBACConfigMapNotFound) {
			return nil
		}

		return err
	}

	content, err := os.ReadFile(rbacPath) //nolint:gosec // path is from trusted directory scan
	if err != nil {
		return fmt.Errorf("read RBAC ConfigMap: %w", err)
	}

	updated, err := removeRBACPolicyFromContent(content, tenantName)
	if err != nil {
		return fmt.Errorf("remove RBAC policy: %w", err)
	}

	info, statErr := os.Stat(rbacPath)

	perm := os.FileMode(kustomizationFilePermissions)
	if statErr == nil {
		perm = info.Mode().Perm()
	}

	rbacPath = filepath.Clean(rbacPath)

	//nolint:gosec // rbacPath is constructed from os.ReadDir entries in the trusted outputDir
	writeErr := os.WriteFile(rbacPath, updated, perm)
	if writeErr != nil {
		return fmt.Errorf("write RBAC ConfigMap: %w", writeErr)
	}

	return nil
}

// removeRBACPolicyFromContent handles multi-document YAML files by splitting on document
// separators, applying RemoveArgoCDRBACPolicy to the matching ConfigMap document, and
// reassembling the full file content.
func removeRBACPolicyFromContent(content []byte, tenantName string) ([]byte, error) {
	docs := splitYAMLDocuments(content)
	if len(docs) <= 1 {
		result, err := RemoveArgoCDRBACPolicy(string(content), tenantName)
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

		updated, err := processConfigMapDoc(doc, trimmed, tenantName)
		if err != nil {
			return nil, err
		}

		docs[docIdx] = updated

		break
	}

	return bytes.Join(docs, []byte("\n---")), nil
}

// processConfigMapDoc applies RemoveArgoCDRBACPolicy to a single YAML document while
// preserving leading whitespace from the original split.
func processConfigMapDoc(originalDoc, trimmedDoc []byte, tenantName string) ([]byte, error) {
	docStr := string(originalDoc)

	prefix := ""
	if idx := strings.IndexFunc(
		docStr,
		func(r rune) bool { return r != '\n' && r != '\r' },
	); idx > 0 {
		prefix = docStr[:idx]
	}

	updated, err := RemoveArgoCDRBACPolicy(string(trimmedDoc), tenantName)
	if err != nil {
		return nil, err
	}

	return []byte(prefix + updated), nil
}

// splitYAMLDocuments splits YAML content into individual documents, handling files
// that start with a leading --- separator.
func splitYAMLDocuments(content []byte) [][]byte {
	if bytes.HasPrefix(content, []byte("---")) {
		content = append([]byte("\n"), content...)
	}

	return bytes.Split(content, []byte("\n---"))
}

// findRBACConfigMapFile scans YAML files in dir (non-recursive) looking for a
// Kubernetes ConfigMap with metadata.name "argocd-rbac-cm".
// Symlinks are skipped to prevent symlink-escape writes.
// Returns the path to the first matching file, or an error if none is found.
// Read errors on individual files are propagated rather than silently skipped.
func findRBACConfigMapFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		name := entry.Name()
		if !isYAMLFile(name) {
			continue
		}

		filePath := filepath.Join(dir, name)

		data, readErr := os.ReadFile(filePath) //nolint:gosec // path from trusted os.ReadDir
		if readErr != nil {
			return "", fmt.Errorf("read file %q: %w", filePath, readErr)
		}

		if contentContainsRBACConfigMap(data) {
			return filePath, nil
		}
	}

	return "", fmt.Errorf("%w in %q", ErrRBACConfigMapNotFound, dir)
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))

	return ext == ".yaml" || ext == ".yml"
}

// contentContainsRBACConfigMap checks whether any YAML document in the given content
// is a ConfigMap with metadata.name "argocd-rbac-cm".
func contentContainsRBACConfigMap(data []byte) bool {
	for _, doc := range splitYAMLDocuments(data) {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		if isRBACConfigMapDoc(doc) {
			return true
		}
	}

	return false
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

	return resource.Kind == "ConfigMap" && resource.Metadata.Name == rbacConfigMapName
}
