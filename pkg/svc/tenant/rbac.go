package tenant

import (
	"fmt"
	"strings"

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

	var namespaceDocs, rbDocs []string

	for _, namespace := range opts.Namespaces {
		nsYAML, err := marshalNamespace(namespace)
		if err != nil {
			return nil, err
		}

		namespaceDocs = append(namespaceDocs, nsYAML)

		rbYAML, err := marshalRoleBinding(opts.Name, namespace, primaryNS, opts.ClusterRole)
		if err != nil {
			return nil, err
		}

		rbDocs = append(rbDocs, rbYAML)
	}

	// Single ServiceAccount in primary namespace.
	saYAML, err := marshalServiceAccount(opts.Name, primaryNS)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"namespace.yaml":      joinDocs(namespaceDocs),
		"serviceaccount.yaml": saYAML,
		"rolebinding.yaml":    joinDocs(rbDocs),
	}, nil
}

func marshalNamespace(name string) (string, error) {
	namespace := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   name,
			"labels": ManagedByLabels(),
		},
	}

	b, err := yaml.Marshal(namespace)
	if err != nil {
		return "", fmt.Errorf("marshal namespace: %w", err)
	}

	return string(b), nil
}

func marshalServiceAccount(name, namespaceName string) (string, error) {
	serviceAccount := map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespaceName,
			"labels":    ManagedByLabels(),
		},
	}

	b, err := yaml.Marshal(serviceAccount)
	if err != nil {
		return "", fmt.Errorf("marshal service account: %w", err)
	}

	return string(b), nil
}

func marshalRoleBinding(name, namespace, saNamespace, clusterRole string) (string, error) {
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
				Name:      name,
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
