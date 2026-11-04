package operator

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// Node-identity detection: infers a cluster's distribution and provider from its Node objects, for
// the self-registered host cluster whose spec is empty on purpose (the operator does not own its
// lifecycle). The rules match the web UI's detection (web/ui/src/lib/clusterIdentity.ts), so the
// host Cluster resource and the UI report the same answer from the same evidence. Each rule is a
// documented marker of that distribution, never a naming convention.
//
// The two detections take consensus differently, because the evidence differs. A distribution rule
// must hold on EVERY node before it counts, so a mixed cluster reports nothing rather than a guess.
// A provider is named by spec.providerID, which the cloud controller manager fills in per node, so
// at least one node must carry one and every non-empty one must map to the same known provider; a
// node still missing it supplies no evidence rather than blocking the result. Either way an unknown
// result is the empty value: a wrong identity is worse than none, because the user acts on it.

const (
	// kwokNodeAnnotation is set to "fake" on the simulated nodes KWOK manages.
	kwokNodeAnnotation = "kwok.x-k8s.io/node"
	// eksNodegroupLabel is set on EKS managed node-group nodes.
	eksNodegroupLabel = "eks.amazonaws.com/nodegroup"
	// gkeNodepoolLabel is set on GKE node-pool nodes.
	gkeNodepoolLabel = "cloud.google.com/gke-nodepool"
	// aksClusterLabel is set on AKS nodes.
	aksClusterLabel = "kubernetes.azure.com/cluster"
	// kindProviderIDPrefix is the provider ID scheme kind's node provider sets.
	kindProviderIDPrefix = "kind://"
	// kindDockerProviderIDPrefix is the provider ID kind sets on Docker-backed nodes; kind writes its
	// node backend (docker, podman or nerdctl) as the first path segment.
	kindDockerProviderIDPrefix = kindProviderIDPrefix + "docker/"
)

// distributionRule names a distribution and the node marker that proves it.
type distributionRule struct {
	distribution v1alpha1.Distribution
	matches      func(node *corev1.Node) bool
}

// distributionRules are checked in order; the first rule every node satisfies wins.
//
//nolint:gochecknoglobals // immutable rule table, read-only after initialisation
var distributionRules = []distributionRule{
	{
		distribution: v1alpha1.DistributionTalos,
		matches: func(node *corev1.Node) bool {
			return strings.HasPrefix(node.Status.NodeInfo.OSImage, "Talos")
		},
	},
	{
		distribution: v1alpha1.DistributionK3s,
		matches: func(node *corev1.Node) bool {
			return strings.Contains(node.Status.NodeInfo.KubeletVersion, "+k3s")
		},
	},
	{
		distribution: v1alpha1.DistributionEKS,
		matches: func(node *corev1.Node) bool {
			_, labelled := node.Labels[eksNodegroupLabel]

			return strings.Contains(node.Status.NodeInfo.KubeletVersion, "-eks-") || labelled
		},
	},
	{
		distribution: v1alpha1.DistributionGKE,
		matches: func(node *corev1.Node) bool {
			_, labelled := node.Labels[gkeNodepoolLabel]

			return strings.Contains(node.Status.NodeInfo.KubeletVersion, "-gke.") || labelled
		},
	},
	{
		distribution: v1alpha1.DistributionAKS,
		matches: func(node *corev1.Node) bool {
			_, labelled := node.Labels[aksClusterLabel]

			return labelled
		},
	},
	{
		distribution: v1alpha1.DistributionKWOK,
		matches: func(node *corev1.Node) bool {
			return node.Annotations[kwokNodeAnnotation] == "fake"
		},
	},
	// A kind node provider sets a kind:// provider ID, and kind runs upstream Kubernetes (KSail's
	// Vanilla). A kubeadm-built Vanilla cluster has no equally reliable node marker, so it reads as
	// unknown.
	{
		distribution: v1alpha1.DistributionVanilla,
		matches: func(node *corev1.Node) bool {
			return strings.HasPrefix(node.Spec.ProviderID, kindProviderIDPrefix)
		},
	},
}

// providerPrefix maps a spec.providerID scheme to the provider it names.
type providerPrefix struct {
	prefix   string
	provider v1alpha1.Provider
}

//nolint:gochecknoglobals // immutable prefix table, read-only after initialisation
var providerPrefixes = []providerPrefix{
	{prefix: "hcloud://", provider: v1alpha1.ProviderHetzner},
	{prefix: "aws://", provider: v1alpha1.ProviderAWS},
	{prefix: "gce://", provider: v1alpha1.ProviderGCP},
	{prefix: "azure://", provider: v1alpha1.ProviderAzure},
	{prefix: kindDockerProviderIDPrefix, provider: v1alpha1.ProviderDocker},
}

// detectDistribution returns the distribution every node agrees on, or "" when there are no nodes
// or no rule holds on all of them.
func detectDistribution(nodes []corev1.Node) v1alpha1.Distribution {
	if len(nodes) == 0 {
		return ""
	}

	for _, rule := range distributionRules {
		if allNodes(nodes, rule.matches) {
			return rule.distribution
		}
	}

	return ""
}

// detectProvider returns the provider every non-empty spec.providerID names, or "" when no node
// carries one, one names an unknown scheme, or two name different providers.
func detectProvider(nodes []corev1.Node) v1alpha1.Provider {
	var detected v1alpha1.Provider

	for i := range nodes {
		providerID := nodes[i].Spec.ProviderID
		if providerID == "" {
			continue
		}

		provider := providerOf(providerID)
		if provider == "" || (detected != "" && provider != detected) {
			return ""
		}

		detected = provider
	}

	return detected
}

// providerOf maps one spec.providerID to its provider, or "" for an unknown scheme.
func providerOf(providerID string) v1alpha1.Provider {
	for _, entry := range providerPrefixes {
		if strings.HasPrefix(providerID, entry.prefix) {
			return entry.provider
		}
	}

	return ""
}

func allNodes(nodes []corev1.Node, matches func(node *corev1.Node) bool) bool {
	for i := range nodes {
		if !matches(&nodes[i]) {
			return false
		}
	}

	return true
}
