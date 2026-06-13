/**
 * KSail Context Names (single source for the VSCE)
 *
 * One prefix table mapping a KSail distribution to its kubeconfig context-name
 * convention, plus helpers to build a context name from a cluster and to parse a
 * cluster name back out of a context.
 *
 * SOURCE OF TRUTH — keep in lockstep with the Go side:
 *   pkg/apis/cluster/v1alpha1/distribution.go → (*Distribution).ContextName
 *   pkg/svc/detector/cluster/cluster.go       → DetectDistributionFromContext
 *
 *   Vanilla  (Kind):     kind-<name>
 *   K3s      (K3d):      k3d-<name>
 *   Talos:               admin@<name>
 *   VCluster (Vind):     vcluster-docker_<name>
 *   KWOK     (kwokctl):  kwok-<name>
 *
 * EKS is intentionally absent: eksctl writes <iam>@<cluster>.<region>.eksctl.io
 * contexts whose IAM identity is unknown at scaffold time, so no static prefix
 * can be formed (see distribution.go ContextName doc). The fallback returns the
 * bare cluster name for EKS and any unknown distribution.
 *
 * This module is intentionally vscode-free so it can be unit-tested directly.
 */

/**
 * Lowercased KSail distribution name, as it appears in `cluster list` JSON.
 */
export type DistributionKey =
  | "vanilla"
  | "k3s"
  | "talos"
  | "vcluster"
  | "kwok"
  | "eks";

/**
 * A KSail context-name convention: how to build it and how to detect it.
 */
interface ContextPrefixEntry {
  /** Lowercased distribution key (matches `cluster list` JSON, case-insensitively). */
  distribution: DistributionKey;
  /** Literal context prefix, e.g. "kind-". */
  prefix: string;
  /** Regex matching the prefix at the start of a context name. */
  pattern: RegExp;
}

/**
 * The single prefix table. Order matters only for parsing (longest-unique
 * prefixes are non-overlapping here, so any order is correct).
 */
const CONTEXT_PREFIXES: readonly ContextPrefixEntry[] = [
  { distribution: "vanilla", prefix: "kind-", pattern: /^kind-/ },
  { distribution: "k3s", prefix: "k3d-", pattern: /^k3d-/ },
  { distribution: "talos", prefix: "admin@", pattern: /^admin@/ },
  { distribution: "vcluster", prefix: "vcluster-docker_", pattern: /^vcluster-docker_/ },
  { distribution: "kwok", prefix: "kwok-", pattern: /^kwok-/ },
];

/**
 * Normalize an arbitrary distribution string (e.g. "Vanilla", "K3s", "VCluster")
 * to its lowercased key.
 */
function normalizeDistribution(distribution: string | undefined): string {
  return (distribution ?? "").trim().toLowerCase();
}

/**
 * Build the kubeconfig context name for a cluster from its distribution.
 *
 * Falls back to the bare cluster name when the distribution is EKS, unknown, or
 * absent (matching the Go ContextName behaviour for distributions without a
 * static prefix).
 */
export function resolveContext(clusterName: string, distribution: string | undefined): string {
  const key = normalizeDistribution(distribution);
  const entry = CONTEXT_PREFIXES.find((e) => e.distribution === key);
  if (!entry) {
    return clusterName;
  }
  return `${entry.prefix}${clusterName}`;
}

/**
 * Test whether a kubeconfig context name matches a KSail-managed pattern.
 */
export function isKSailContext(contextName: string): boolean {
  return CONTEXT_PREFIXES.some(({ pattern }) => pattern.test(contextName));
}

/**
 * Extract the cluster name from a KSail-managed kubeconfig context name, or
 * undefined when the context matches no known prefix.
 */
export function parseClusterName(contextName: string): string | undefined {
  for (const { pattern, prefix } of CONTEXT_PREFIXES) {
    if (pattern.test(contextName)) {
      return contextName.slice(prefix.length);
    }
  }
  return undefined;
}
