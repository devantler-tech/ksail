// Live cluster identity: infers a cluster's distribution and provider from its Node objects, for
// clusters whose Cluster object carries no spec (a kubeconfig context ksail does not manage). A
// cluster's nodes say what it runs on through fields every conformant cluster populates:
// status.nodeInfo (OS image, kubelet version), spec.providerID (set by the cloud controller manager
// or the local node provider), and the labels and annotations managed services and simulators put
// on their nodes. Each rule is a documented marker of that distribution, never a naming convention.
//
// Every rule must hold on EVERY node before it counts, so a mixed or partly-initialised cluster
// reports "unknown" rather than a guess. An unknown result renders as "—"; a wrong label is worse
// than no label, because the user acts on it.

import type { Cluster, K8sObject } from "../api.ts";

// ClusterIdentity is what the nodes prove about a cluster. A field is undefined when no rule matched.
export interface ClusterIdentity {
  distribution?: string;
  provider?: string;
}

interface NodeFacts {
  osImage: string;
  kubeletVersion: string;
  providerID: string;
  labels: Record<string, unknown>;
  annotations: Record<string, unknown>;
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : undefined;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function nodeFacts(node: K8sObject): NodeFacts {
  const info = record(record(node.status)?.nodeInfo);

  return {
    osImage: str(info?.osImage),
    kubeletVersion: str(info?.kubeletVersion),
    providerID: str(record(node.spec)?.providerID),
    labels: record(node.metadata?.labels) ?? {},
    annotations: record(node.metadata?.annotations) ?? {},
  };
}

// DISTRIBUTION_RULES are checked in order; the first rule every node satisfies wins. Values are the
// v1alpha1 distribution names the rest of the UI uses.
const DISTRIBUTION_RULES: { distribution: string; matches: (node: NodeFacts) => boolean }[] = [
  { distribution: "Talos", matches: (node) => node.osImage.startsWith("Talos") },
  { distribution: "K3s", matches: (node) => node.kubeletVersion.includes("+k3s") },
  {
    distribution: "EKS",
    matches: (node) => node.kubeletVersion.includes("-eks-") || "eks.amazonaws.com/nodegroup" in node.labels,
  },
  {
    distribution: "GKE",
    matches: (node) => node.kubeletVersion.includes("-gke.") || "cloud.google.com/gke-nodepool" in node.labels,
  },
  { distribution: "AKS", matches: (node) => "kubernetes.azure.com/cluster" in node.labels },
  // KWOK manages simulated nodes that carry this annotation.
  { distribution: "KWOK", matches: (node) => node.annotations["kwok.x-k8s.io/node"] === "fake" },
  // A kind node provider sets a kind:// provider ID, and kind runs upstream Kubernetes (ksail's
  // Vanilla). A kubeadm-built Vanilla cluster has no equally reliable node marker, so it reads "—".
  { distribution: "Vanilla", matches: (node) => node.providerID.startsWith("kind://") },
];

// PROVIDER_PREFIXES maps a spec.providerID scheme to the v1alpha1 provider name.
const PROVIDER_PREFIXES: { prefix: string; provider: string }[] = [
  { prefix: "hcloud://", provider: "Hetzner" },
  { prefix: "aws://", provider: "AWS" },
  { prefix: "gce://", provider: "GCP" },
  { prefix: "azure://", provider: "Azure" },
  { prefix: "kind://", provider: "Docker" },
];

function providerOf(providerID: string): string | undefined {
  return PROVIDER_PREFIXES.find((entry) => providerID.startsWith(entry.prefix))?.provider;
}

// detectDistribution returns the distribution every node agrees on, or undefined.
export function detectDistribution(nodes: K8sObject[]): string | undefined {
  if (nodes.length === 0) {
    return undefined;
  }

  const facts = nodes.map(nodeFacts);

  return DISTRIBUTION_RULES.find((rule) => facts.every(rule.matches))?.distribution;
}

// detectProvider returns the provider every node's providerID names, or undefined. A node the cloud
// controller manager has not initialised yet has no providerID; it neither confirms nor contradicts
// the others, but at least one node must carry one.
export function detectProvider(nodes: K8sObject[]): string | undefined {
  const providers = nodes.map((node) => nodeFacts(node).providerID).filter((id) => id !== "").map(providerOf);

  if (providers.length === 0) {
    return undefined;
  }

  const [first] = providers;

  return first !== undefined && providers.every((provider) => provider === first) ? first : undefined;
}

// detectClusterIdentity combines both detections.
export function detectClusterIdentity(nodes: K8sObject[]): ClusterIdentity {
  return { distribution: detectDistribution(nodes), provider: detectProvider(nodes) };
}

// IdentityField is one displayed value and whether the nodes (not the spec) supplied it.
export interface IdentityField {
  value: string;
  detected: boolean;
}

// displayIdentity merges a cluster's spec with its detected identity. The spec always wins: it is
// what ksail was told to build, and detection only fills fields the spec leaves empty.
export function displayIdentity(
  cluster: Cluster,
  detected: ClusterIdentity | undefined,
): { distribution: IdentityField; provider: IdentityField } {
  const spec = cluster.spec?.cluster;
  const field = (fromSpec: string | undefined, fromNodes: string | undefined): IdentityField =>
    fromSpec ? { value: fromSpec, detected: false } : { value: fromNodes || "—", detected: Boolean(fromNodes) };

  return {
    distribution: field(spec?.distribution, detected?.distribution),
    provider: field(spec?.provider, detected?.provider),
  };
}
