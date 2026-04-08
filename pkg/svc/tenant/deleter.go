package tenant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
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
	// GitRepo is the tenant repo as owner/repo-name.
	GitRepo string
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

	return nil
}

func deleteRepo(ctx context.Context, opts DeleteOptions) error {
	if opts.GitProvider == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
	}

	if opts.GitRepo == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitRepoRequired)
	}

	owner, repo, err := gitprovider.ParseOwnerRepo(opts.GitRepo)
	if err != nil {
		return fmt.Errorf("parse git-repo: %w", err)
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
	docs := bytes.Split(content, []byte("\n---"))
	if len(docs) <= 1 {
		result, err := RemoveArgoCDRBACPolicy(string(content), tenantName)
		if err != nil {
			return nil, err
		}

		return []byte(result), nil
	}

	for i, doc := range docs {
		trimmed := bytes.TrimSpace(doc)
		if len(trimmed) == 0 {
			continue
		}

		var resource struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}

		if unmarshalErr := yaml.Unmarshal(trimmed, &resource); unmarshalErr != nil {
			continue
		}

		if resource.Kind == "ConfigMap" && resource.Metadata.Name == rbacConfigMapName {
			docStr := string(doc)
			// Preserve leading whitespace/newline from original split.
			prefix := ""
			if idx := strings.IndexFunc(docStr, func(r rune) bool { return r != '\n' && r != '\r' }); idx > 0 {
				prefix = docStr[:idx]
			}

			updated, err := RemoveArgoCDRBACPolicy(string(trimmed), tenantName)
			if err != nil {
				return nil, err
			}

			docs[i] = []byte(prefix + updated)

			break
		}
	}

	return bytes.Join(docs, []byte("\n---")), nil
}

// findRBACConfigMapFile scans YAML files in dir (non-recursive) looking for a
// Kubernetes ConfigMap with metadata.name "argocd-rbac-cm".
// Returns the path to the first matching file, or an error if none is found.
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

		if isRBACConfigMap(filePath) {
			return filePath, nil
		}
	}

	return "", fmt.Errorf("%w in %q", ErrRBACConfigMapNotFound, dir)
}

func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))

	return ext == ".yaml" || ext == ".yml"
}

// isRBACConfigMap reads a YAML file and checks if any document in it is a ConfigMap
// with metadata.name "argocd-rbac-cm". Handles multi-document YAML (separated by ---).
func isRBACConfigMap(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // path is from trusted directory listing
	if err != nil {
		return false
	}

	var resource struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}

	for _, doc := range bytes.Split(data, []byte("\n---")) {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		resource.Kind = ""
		resource.Metadata.Name = ""

		if unmarshalErr := yaml.Unmarshal(doc, &resource); unmarshalErr != nil {
			continue
		}

		if resource.Kind == "ConfigMap" && resource.Metadata.Name == rbacConfigMapName {
			return true
		}
	}

	return false
}
