package tenant

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// GenerateRBACManifests generates RBAC manifests for a tenant.
// Returns a map of filename -> YAML content.
// Files: namespace.yaml, serviceaccount.yaml, rolebinding.yaml
func GenerateRBACManifests(opts Options) (map[string]string, error) {
	var namespaceDocs, saDocs, rbDocs []string

	for _, ns := range opts.Namespaces {
		nsYAML, err := marshalNamespace(ns)
		if err != nil {
			return nil, err
		}
		namespaceDocs = append(namespaceDocs, nsYAML)

		saYAML, err := marshalServiceAccount(opts.Name, ns)
		if err != nil {
			return nil, err
		}
		saDocs = append(saDocs, saYAML)

		rbYAML, err := marshalRoleBinding(opts.Name, ns, opts.ClusterRole)
		if err != nil {
			return nil, err
		}
		rbDocs = append(rbDocs, rbYAML)
	}

	return map[string]string{
		"namespace.yaml":      joinDocs(namespaceDocs),
		"serviceaccount.yaml": joinDocs(saDocs),
		"rolebinding.yaml":    joinDocs(rbDocs),
	}, nil
}

func marshalNamespace(name string) (string, error) {
	ns := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: ManagedByLabels(),
		},
	}
	b, err := yaml.Marshal(ns)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalServiceAccount(name, namespace string) (string, error) {
	sa := corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    ManagedByLabels(),
		},
	}
	b, err := yaml.Marshal(sa)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalRoleBinding(name, namespace, clusterRole string) (string, error) {
	rb := rbacv1.RoleBinding{
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
				Namespace: namespace,
			},
		},
	}
	b, err := yaml.Marshal(rb)
	if err != nil {
		return "", err
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
