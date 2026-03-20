/**
 * Cluster Explorer Contributor
 *
 * Annotates KSail-managed contexts with "(KSail)" label in the Kubernetes
 * extension's Cluster Explorer tree. Also provides pod log viewing support.
 */

import * as vscode from "vscode";
import type { ClusterExplorerV1_1, KubectlV1 } from "vscode-kubernetes-tools-api";
import { detectClusterStatus, listClusters, type ClusterStatus } from "../ksail/clusters.js";
import { getPodLogs } from "../ksail/kubectl.js";

// ── NodeUICustomizer ─────────────────────────────────────────────────

/**
 * KSail context name patterns for each distribution
 */
const KSAIL_CONTEXT_PATTERNS = [
  /^kind-/,      // Vanilla (Kind)
  /^k3d-/,       // K3s (K3d)
  /^admin@/,     // Talos
  /^vcluster_/,  // VCluster
];

/**
 * Result of creating a NodeUICustomizer
 */
export interface KSailNodeUICustomizerResult {
  customizer: ClusterExplorerV1_1.NodeUICustomizer;
  /** Invalidate the cached cluster data so the next refresh fetches fresh status */
  invalidateCache: () => void;
}

/**
 * Create a NodeUICustomizer that annotates KSail-managed contexts
 */
export function createKSailNodeUICustomizer(
  outputChannel: vscode.OutputChannel
): KSailNodeUICustomizerResult {
  // Cache known KSail cluster names and their statuses
  let cachedClusters: Map<string, ClusterStatus> | undefined;
  let cacheTimestamp = 0;
  let inflight: Promise<Map<string, ClusterStatus>> | undefined;
  const CACHE_TTL_MS = 30_000;

  async function getKSailClusters(): Promise<Map<string, ClusterStatus>> {
    const now = Date.now();
    if (cachedClusters && now - cacheTimestamp < CACHE_TTL_MS) {
      return cachedClusters;
    }

    // Deduplicate concurrent calls — reuse the same in-flight promise
    if (inflight) {
      return inflight;
    }

    inflight = (async () => {
      try {
        const clusters = await listClusters(outputChannel);
        const entries = await Promise.all(
          clusters.map(async (c) => {
            const status = await detectClusterStatus(c.name, c.provider);
            return [c.name, status] as const;
          })
        );
        cachedClusters = new Map(entries);
        cacheTimestamp = Date.now();
      } catch {
        cachedClusters = cachedClusters ?? new Map();
      }
      return cachedClusters;
    })();

    try {
      return await inflight;
    } finally {
      inflight = undefined;
    }
  }

  function invalidateCache(): void {
    cachedClusters = undefined;
    cacheTimestamp = 0;
  }

  const customizer: ClusterExplorerV1_1.NodeUICustomizer = {
    async customize(
      node: ClusterExplorerV1_1.ClusterExplorerNode,
      treeItem: vscode.TreeItem
    ): Promise<void> {
      if (node.nodeType !== "context") {
        return;
      }

      const contextName = node.name;

      // Quick pattern check first
      const matchesPattern = KSAIL_CONTEXT_PATTERNS.some((p) => p.test(contextName));
      if (!matchesPattern) {
        return;
      }

      // Extract cluster name from context name
      let clusterName: string;
      if (contextName.startsWith("kind-")) {
        clusterName = contextName.slice(5);
      } else if (contextName.startsWith("k3d-")) {
        clusterName = contextName.slice(4);
      } else if (contextName.startsWith("admin@")) {
        clusterName = contextName.slice(6);
      } else if (contextName.startsWith("vcluster_")) {
        clusterName = contextName.slice(9);
      } else {
        return;
      }

      // Verify it's actually a KSail-managed cluster
      const clusters = await getKSailClusters();
      if (!clusters.has(clusterName)) {
        return;
      }

      // Annotate with KSail label and cluster status
      const status = clusters.get(clusterName);
      const statusLabel = status === "running" ? " · Running"
        : status === "stopped" ? " · Stopped"
        : "";
      const existing = treeItem.description ? `${treeItem.description} ` : "";
      treeItem.description = `${existing}(KSail${statusLabel})`;

      // Append custom contextValue so view/item/context menus can target KSail nodes
      const existingCtx = treeItem.contextValue ? `${treeItem.contextValue} ` : "";
      treeItem.contextValue = `${existingCtx}ksail.cluster`;
    },
  };

  return { customizer, invalidateCache };
}

// ── Pod Logs (for ClusterExplorer context) ───────────────────────────

/**
 * Cached output channels for pod logs, keyed by "namespace/podName"
 */
const podLogChannels = new Map<string, vscode.OutputChannel>();

/**
 * Show pod logs in an output channel, reusing existing channels per pod
 */
export async function showPodLogs(
  kubectl: KubectlV1,
  namespace: string,
  podName: string
): Promise<void> {
  const key = `${namespace}/${podName}`;
  let logChannel = podLogChannels.get(key);

  if (!logChannel) {
    logChannel = vscode.window.createOutputChannel(`KSail: ${key}`);
    podLogChannels.set(key, logChannel);
  }

  logChannel.clear();
  logChannel.show();
  logChannel.appendLine(`Fetching logs for ${key}...`);

  const logs = await getPodLogs(kubectl, namespace, podName);
  logChannel.appendLine(logs);
}

/**
 * Dispose all cached pod log channels
 */
export function disposePodLogChannels(): void {
  for (const channel of podLogChannels.values()) {
    channel.dispose();
  }
  podLogChannels.clear();
}
