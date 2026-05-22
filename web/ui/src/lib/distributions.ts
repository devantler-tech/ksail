// Distribution × provider matrix, mirroring the operator's API validation
// (pkg/apis/cluster/v1alpha1/provider.go supportedProviders). Kubernetes (in-cluster) is listed
// first for the distributions that support it because that is the operator's default provider.

export const DISTRIBUTIONS = ["Vanilla", "K3s", "Talos", "VCluster", "KWOK", "EKS"] as const;

export type Distribution = (typeof DISTRIBUTIONS)[number];

const PROVIDERS: Record<Distribution, string[]> = {
  Vanilla: ["Kubernetes", "Docker"],
  K3s: ["Kubernetes", "Docker"],
  Talos: ["Kubernetes", "Docker", "Hetzner", "Omni"],
  VCluster: ["Kubernetes", "Docker"],
  KWOK: ["Kubernetes", "Docker"],
  EKS: ["AWS"],
};

// providersFor returns the providers valid for a distribution (the first is the recommended default).
export function providersFor(distribution: string): string[] {
  return PROVIDERS[distribution as Distribution] ?? ["Kubernetes"];
}
