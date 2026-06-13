package docker

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// schemeDescriptor bundles the per-scheme behavior previously scattered across
// the parallel switch statements in listContainers, extractClusterName, and
// extractRole. Adding a new distribution means adding one descriptor entry
// instead of touching three switches plus constants.
type schemeDescriptor struct {
	// list returns the containers belonging to clusterName for this scheme.
	list func(p *Provider, ctx context.Context, clusterName string) ([]container.Summary, error)
	// clusterName extracts the cluster name from a container, or "" if none.
	clusterName func(ctr container.Summary) string
	// role extracts the node role from a container, or "" if unknown.
	role func(ctr container.Summary) string
}

// schemeDescriptors maps each LabelScheme to its behavior descriptor. It is the
// single source of truth for per-scheme dispatch; a missing entry surfaces as
// ErrUnknownLabelScheme from listContainers.
//
//nolint:gochecknoglobals // read-only dispatch table; avoids per-call allocation.
var schemeDescriptors = map[LabelScheme]schemeDescriptor{
	LabelSchemeKind: {
		list:        (*Provider).listKindContainers,
		clusterName: extractKindClusterName,
		role:        extractKindRole,
	},
	LabelSchemeK3d: {
		list:        (*Provider).listK3dContainers,
		clusterName: func(ctr container.Summary) string { return ctr.Labels[LabelK3dCluster] },
		role:        func(ctr container.Summary) string { return ctr.Labels[LabelK3dRole] },
	},
	LabelSchemeTalos: {
		list:        (*Provider).listTalosContainers,
		clusterName: func(ctr container.Summary) string { return ctr.Labels[LabelTalosClusterName] },
		role:        func(ctr container.Summary) string { return ctr.Labels[LabelTalosType] },
	},
	LabelSchemeVCluster: {
		list:        (*Provider).listVClusterContainers,
		clusterName: extractVClusterName,
		role:        extractVClusterRole,
	},
	LabelSchemeKWOK: {
		list:        (*Provider).listKWOKContainers,
		clusterName: extractKWOKClusterName,
		role:        extractKWOKRole,
	},
}

// extractKindClusterName extracts the cluster name from a Kind container name.
// Kind uses container names of the form <cluster>-control-plane or
// <cluster>-worker[N] (Kind doesn't use labels).
func extractKindClusterName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}

	name := strings.TrimPrefix(ctr.Names[0], "/")
	// Look for Kind-style suffixes.
	for _, suffix := range []string{"-control-plane", "-worker"} {
		if idx := strings.Index(name, suffix); idx > 0 {
			return name[:idx]
		}
	}

	return ""
}

// extractKindRole determines the role of a Kind container from its name.
func extractKindRole(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}

	name := strings.TrimPrefix(ctr.Names[0], "/")
	if strings.Contains(name, "control-plane") {
		return roleControlPlane
	}

	if strings.Contains(name, "worker") {
		return "worker"
	}

	return ""
}
