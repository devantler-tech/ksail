/**
 * KSail Context Name Utilities
 *
 * Shared helpers for parsing KSail-managed kubeconfig context names.
 * Each distribution uses a specific prefix:
 *   - Vanilla (Kind):  "kind-{name}"
 *   - K3s (K3d):       "k3d-{name}"
 *   - Talos:           "admin@{name}"
 *   - VCluster (Vind): "vcluster-docker_{name}"
 */

/**
 * Known KSail context name prefixes and their lengths
 */
const KSAIL_CONTEXT_PREFIXES: readonly { pattern: RegExp; prefix: string }[] = [
  { pattern: /^kind-/, prefix: "kind-" },
  { pattern: /^k3d-/, prefix: "k3d-" },
  { pattern: /^admin@/, prefix: "admin@" },
  { pattern: /^vcluster-docker_/, prefix: "vcluster-docker_" },
];

/**
 * Test whether a kubeconfig context name matches a KSail-managed pattern.
 */
export function isKSailContext(contextName: string): boolean {
  return KSAIL_CONTEXT_PREFIXES.some(({ pattern }) => pattern.test(contextName));
}

/**
 * Extract the cluster name from a KSail-managed kubeconfig context name.
 * Returns `undefined` if the context name does not match any known pattern.
 */
export function parseClusterName(contextName: string): string | undefined {
  for (const { pattern, prefix } of KSAIL_CONTEXT_PREFIXES) {
    if (pattern.test(contextName)) {
      return contextName.slice(prefix.length);
    }
  }
  return undefined;
}
