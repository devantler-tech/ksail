package tenant

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// GenerateRBACManifests generates RBAC manifests for a tenant.
// Returns a map of filename -> YAML content.
// Files: namespace.yaml, serviceaccount.yaml, rolebinding.yaml
//
// A single ServiceAccount is created in the primary namespace (Namespaces[0]).
// A RoleBinding is created in each namespace, referencing the primary-namespace SA.
func GenerateRBACManifests(opts Options) (map[string]string, error) {
	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	primaryNS := opts.Namespaces[0]
	roles := effectiveClusterRoles(opts)
	bindingNames := buildBindingNames(opts.Name, roles)

	var namespaceDocs, rbDocs []string

	for _, namespace := range opts.Namespaces {
		nsYAML, err := marshalNamespace(namespace, opts.PodSecurity)
		if err != nil {
			return nil, err
		}

		namespaceDocs = append(namespaceDocs, nsYAML)

		for i, role := range roles {
			rbYAML, err := marshalRoleBinding(bindingNames[i], opts.Name, namespace, primaryNS, role)
			if err != nil {
				return nil, err
			}

			rbDocs = append(rbDocs, rbYAML)
		}
	}

	// Single ServiceAccount in primary namespace.
	saYAML, err := marshalServiceAccount(opts.Name, primaryNS, opts.DisableTokenAutomount, opts.ImagePullSecrets)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"namespace.yaml":      joinDocs(namespaceDocs),
		"serviceaccount.yaml": saYAML,
		"rolebinding.yaml":    joinDocs(rbDocs),
	}, nil
}

// effectiveClusterRoles returns the ClusterRoles to bind, preferring the
// ClusterRoles slice and falling back to the legacy single ClusterRole field.
// Entries are trimmed and de-duplicated; empty entries are dropped.
func effectiveClusterRoles(opts Options) []string {
	raw := opts.ClusterRoles
	if len(raw) == 0 {
		if opts.ClusterRole != "" {
			raw = []string{opts.ClusterRole}
		} else {
			raw = []string{DefaultClusterRole}
		}
	}

	seen := make(map[string]bool, len(raw))
	result := make([]string, 0, len(raw))

	for _, role := range raw {
		role = strings.TrimSpace(role)
		if role == "" || seen[role] {
			continue
		}

		seen[role] = true

		result = append(result, role)
	}

	if len(result) == 0 {
		result = append(result, DefaultClusterRole)
	}

	return result
}

// buildBindingNames returns the RoleBinding metadata.name for each role.
// A single role keeps the tenant name (so it reads as "<tenant>"); multiple
// roles get "<tenant>-<sanitized-role>" suffixes. ClusterRole names may contain
// characters invalid for metadata.name (e.g. "system:auth-delegator"), so the
// role segment is sanitized to a DNS-1123 label and disambiguated on collision.
func buildBindingNames(tenant string, roles []string) []string {
	names := make([]string, len(roles))
	if len(roles) <= 1 {
		if len(roles) == 1 {
			names[0] = tenant
		}

		return names
	}

	used := make(map[string]bool, len(roles))

	for i, role := range roles {
		base := tenant + "-" + sanitizeNameSegment(role)
		name := base

		for dup := 2; used[name]; dup++ {
			name = fmt.Sprintf("%s-%d", base, dup)
		}

		used[name] = true
		names[i] = name
	}

	return names
}

// sanitizeNameSegment lowercases the input and replaces any character that is
// not a lowercase alphanumeric or '-' with '-', trimming leading/trailing '-'.
func sanitizeNameSegment(value string) string {
	var builder strings.Builder

	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}

	out := strings.Trim(builder.String(), "-")
	if out == "" {
		out = "role"
	}

	return out
}

func marshalNamespace(name, podSecurity string) (string, error) {
	labels := ManagedByLabels()
	for k, v := range k8s.PSSLabels(podSecurity) {
		labels[k] = v
	}

	namespace := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   name,
			"labels": labels,
		},
	}

	b, err := yaml.Marshal(namespace)
	if err != nil {
		return "", fmt.Errorf("marshal namespace: %w", err)
	}

	return string(b), nil
}

func marshalServiceAccount(
	name, namespaceName string,
	disableAutomount bool,
	imagePullSecrets []string,
) (string, error) {
	serviceAccount := map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespaceName,
			"labels":    ManagedByLabels(),
		},
	}

	if disableAutomount {
		serviceAccount["automountServiceAccountToken"] = false
	}

	if len(imagePullSecrets) > 0 {
		refs := make([]map[string]string, len(imagePullSecrets))
		for i, secret := range imagePullSecrets {
			refs[i] = map[string]string{"name": secret}
		}

		serviceAccount["imagePullSecrets"] = refs
	}

	b, err := yaml.Marshal(serviceAccount)
	if err != nil {
		return "", fmt.Errorf("marshal service account: %w", err)
	}

	return string(b), nil
}

func marshalRoleBinding(name, saName, namespace, saNamespace, clusterRole string) (string, error) {
	roleBinding := rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    ManagedByLabels(),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}

	b, err := yaml.Marshal(roleBinding)
	if err != nil {
		return "", fmt.Errorf("marshal role binding: %w", err)
	}

	return string(b), nil
}

func joinDocs(docs []string) string {
	trimmed := make([]string, len(docs))
	for i, d := range docs {
		trimmed[i] = strings.TrimRight(d, "\n")
	}

	return strings.Join(trimmed, "\n---\n") + "\n"
}
