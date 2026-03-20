/**
 * Cluster Explorer Contributor
 *
 * Adds KSail status nodes (health, pods, GitOps) under active context nodes
 * in the Kubernetes extension's Cluster Explorer tree. Replaces the standalone
 * "KSail: Cluster Status" view, making status information feel native.
 */

import * as vscode from "vscode";
import type { ClusterExplorerV1_1, KubectlV1 } from "vscode-kubernetes-tools-api";
import {
  fetchClusterStatus,
  getPodLogs,
  type ClusterHealth,
  type ClusterStatusSnapshot,
  type GitOpsStatus,
  type NamespacePodSummary,
  type PodInfo,
} from "../ksail/kubectl.js";
import { listClusters } from "../ksail/clusters.js";

// ── Custom Node Types ────────────────────────────────────────────────

/**
 * Root "KSail Status" grouping node shown under each active context
 */
class KSailStatusNode implements ClusterExplorerV1_1.Node {
  constructor(
    private readonly kubectl: KubectlV1,
    private readonly outputChannel: vscode.OutputChannel
  ) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    const snapshot = await fetchClusterStatus(this.kubectl);
    return buildStatusNodes(this.kubectl, snapshot);
  }

  getTreeItem(): vscode.TreeItem {
    const item = new vscode.TreeItem(
      "KSail Status",
      vscode.TreeItemCollapsibleState.Expanded
    );
    item.iconPath = new vscode.ThemeIcon("pulse");
    item.contextValue = "ksail.status";
    return item;
  }
}

/**
 * Cluster health indicator node
 */
class HealthNode implements ClusterExplorerV1_1.Node {
  constructor(private readonly health: ClusterHealth) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return [];
  }

  getTreeItem(): vscode.TreeItem {
    const { label, icon } = healthPresentation(this.health);
    const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
    item.iconPath = icon;
    item.contextValue = "ksail.health";
    item.tooltip = `Cluster health: ${this.health}`;
    return item;
  }
}

/**
 * Section header node (Pods, GitOps)
 */
class SectionNode implements ClusterExplorerV1_1.Node {
  constructor(
    private readonly label: string,
    private readonly sectionIcon: vscode.ThemeIcon,
    private readonly childNodes: ClusterExplorerV1_1.Node[]
  ) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return this.childNodes;
  }

  getTreeItem(): vscode.TreeItem {
    const item = new vscode.TreeItem(
      this.label,
      vscode.TreeItemCollapsibleState.Collapsed
    );
    item.iconPath = this.sectionIcon;
    item.contextValue = "ksail.section";
    return item;
  }
}

/**
 * Namespace node containing pods
 */
class NamespaceNode implements ClusterExplorerV1_1.Node {
  constructor(
    private readonly summary: NamespacePodSummary,
    private readonly pods: PodInfo[],
    private readonly kubectl: KubectlV1
  ) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return this.pods.map((pod) => new PodNode(pod, this.kubectl));
  }

  getTreeItem(): vscode.TreeItem {
    const parts: string[] = [];
    if (this.summary.running > 0) {
      parts.push(`${this.summary.running} running`);
    }
    if (this.summary.pending > 0) {
      parts.push(`${this.summary.pending} pending`);
    }
    if (this.summary.failed > 0) {
      parts.push(`${this.summary.failed} failed`);
    }
    const desc = parts.join(", ") || `${this.summary.total} pods`;

    const item = new vscode.TreeItem(
      this.summary.namespace,
      vscode.TreeItemCollapsibleState.Collapsed
    );
    item.description = desc;
    item.iconPath = new vscode.ThemeIcon("symbol-namespace");
    item.contextValue = "ksail.namespace";
    item.tooltip = new vscode.MarkdownString(
      `**${this.summary.namespace}**\n\n` +
        `- Running: ${this.summary.running}\n` +
        `- Pending: ${this.summary.pending}\n` +
        `- Failed: ${this.summary.failed}\n` +
        `- Total: ${this.summary.total}`
    );
    return item;
  }
}

/**
 * Individual pod node
 */
class PodNode implements ClusterExplorerV1_1.Node {
  constructor(
    private readonly pod: PodInfo,
    private readonly kubectl: KubectlV1
  ) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return [];
  }

  getTreeItem(): vscode.TreeItem {
    const item = new vscode.TreeItem(
      this.pod.name,
      vscode.TreeItemCollapsibleState.None
    );
    item.description = `${this.pod.phase} (${this.pod.ready})`;
    item.iconPath = podIcon(this.pod.phase);
    item.contextValue = `ksail.pod.${this.pod.phase.toLowerCase()}`;
    item.tooltip = new vscode.MarkdownString(
      `**${this.pod.name}**\n\n` +
        `- Namespace: ${this.pod.namespace}\n` +
        `- Phase: ${this.pod.phase}\n` +
        `- Ready: ${this.pod.ready}\n` +
        `- Restarts: ${this.pod.restarts}`
    );

    // Clicking a failed/pending pod opens logs
    if (this.pod.phase === "Failed" || this.pod.phase === "Pending") {
      item.command = {
        command: "ksail.status.showPodLogs",
        title: "Show Pod Logs",
        arguments: [this.pod.namespace, this.pod.name],
      };
    }

    return item;
  }
}

/**
 * GitOps reconciliation item
 */
class GitOpsNode implements ClusterExplorerV1_1.Node {
  constructor(private readonly status: GitOpsStatus) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return [];
  }

  getTreeItem(): vscode.TreeItem {
    let desc: string;
    if (this.status.ready === "True") {
      desc = "Ready";
    } else if (this.status.ready === "False") {
      desc = `Failed: ${this.status.status}`;
    } else {
      desc = this.status.status || "Unknown";
    }

    const item = new vscode.TreeItem(
      this.status.name,
      vscode.TreeItemCollapsibleState.None
    );
    item.description = desc;
    item.iconPath = gitopsIcon(this.status.ready);
    item.contextValue = "ksail.gitops";
    item.tooltip = new vscode.MarkdownString(
      `**${this.status.kind}/${this.status.name}**\n\n` +
        `- Namespace: ${this.status.namespace}\n` +
        `- Ready: ${this.status.ready}\n` +
        `- Status: ${this.status.status}`
    );
    return item;
  }
}

/**
 * Informational message node
 */
class InfoNode implements ClusterExplorerV1_1.Node {
  constructor(
    private readonly message: string,
    private readonly icon?: vscode.ThemeIcon
  ) {}

  async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
    return [];
  }

  getTreeItem(): vscode.TreeItem {
    const item = new vscode.TreeItem(
      this.message,
      vscode.TreeItemCollapsibleState.None
    );
    item.iconPath = this.icon ?? new vscode.ThemeIcon("info");
    item.contextValue = "ksail.info";
    return item;
  }
}

// ── Presentation Helpers ─────────────────────────────────────────────

function healthPresentation(health: ClusterHealth): { label: string; icon: vscode.ThemeIcon } {
  switch (health) {
    case "Healthy":
      return {
        label: "Healthy",
        icon: new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed")),
      };
    case "Degraded":
      return {
        label: "Degraded",
        icon: new vscode.ThemeIcon("warning", new vscode.ThemeColor("editorWarning.foreground")),
      };
    case "Error":
      return {
        label: "Error",
        icon: new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed")),
      };
    default:
      return {
        label: "Unknown",
        icon: new vscode.ThemeIcon("question"),
      };
  }
}

function podIcon(phase: string): vscode.ThemeIcon {
  switch (phase) {
    case "Running":
      return new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
    case "Pending":
      return new vscode.ThemeIcon("loading~spin");
    case "Failed":
      return new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
    case "Succeeded":
      return new vscode.ThemeIcon("check");
    default:
      return new vscode.ThemeIcon("circle-outline");
  }
}

function gitopsIcon(ready: string): vscode.ThemeIcon {
  switch (ready) {
    case "True":
      return new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
    case "False":
      return new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
    default:
      return new vscode.ThemeIcon("loading~spin");
  }
}

// ── Build Status Tree ────────────────────────────────────────────────

function buildStatusNodes(
  kubectl: KubectlV1,
  snapshot: ClusterStatusSnapshot
): ClusterExplorerV1_1.Node[] {
  if (snapshot.error) {
    return [
      new InfoNode("No cluster connected", new vscode.ThemeIcon("info")),
      new InfoNode("Create or start a cluster to see status", new vscode.ThemeIcon("lightbulb")),
    ];
  }

  if (snapshot.pods.length === 0 && !snapshot.gitopsEngine) {
    return [new InfoNode("No pods found", new vscode.ThemeIcon("info"))];
  }

  const nodes: ClusterExplorerV1_1.Node[] = [];

  // Health indicator
  nodes.push(new HealthNode(snapshot.health));

  // Pod section
  if (snapshot.podSummaries.length > 0) {
    const totalRunning = snapshot.podSummaries.reduce((s, n) => s + n.running, 0);
    const totalPending = snapshot.podSummaries.reduce((s, n) => s + n.pending, 0);
    const totalFailed = snapshot.podSummaries.reduce((s, n) => s + n.failed, 0);
    const totalPods = snapshot.podSummaries.reduce((s, n) => s + n.total, 0);

    const podLabel =
      `Pods (${totalRunning}/${totalPods} running` +
      (totalPending > 0 ? `, ${totalPending} pending` : "") +
      (totalFailed > 0 ? `, ${totalFailed} failed` : "") +
      ")";

    const podsByNamespace = new Map<string, PodInfo[]>();
    for (const pod of snapshot.pods) {
      const list = podsByNamespace.get(pod.namespace) ?? [];
      list.push(pod);
      podsByNamespace.set(pod.namespace, list);
    }

    const namespaceNodes = snapshot.podSummaries.map((summary) => {
      const namespacePods = podsByNamespace.get(summary.namespace) ?? [];
      return new NamespaceNode(summary, namespacePods, kubectl);
    });

    nodes.push(new SectionNode(podLabel, new vscode.ThemeIcon("symbol-class"), namespaceNodes));
  }

  // GitOps section
  if (snapshot.gitopsEngine && snapshot.gitopsStatuses.length > 0) {
    const readyCount = snapshot.gitopsStatuses.filter((s) => s.ready === "True").length;
    const total = snapshot.gitopsStatuses.length;
    const gitopsLabel = `${snapshot.gitopsEngine} (${readyCount}/${total} ready)`;

    const gitopsNodes = snapshot.gitopsStatuses.map((s) => new GitOpsNode(s));
    nodes.push(new SectionNode(gitopsLabel, new vscode.ThemeIcon("git-merge"), gitopsNodes));
  } else if (snapshot.gitopsEngine) {
    nodes.push(
      new SectionNode(
        `${snapshot.gitopsEngine} (no resources)`,
        new vscode.ThemeIcon("git-merge"),
        [new InfoNode("No reconciliation resources found")]
      )
    );
  }

  return nodes;
}

// ── NodeContributor ──────────────────────────────────────────────────

/**
 * Create a NodeContributor that adds KSail status nodes under active contexts
 */
export function createKSailNodeContributor(
  kubectl: KubectlV1,
  outputChannel: vscode.OutputChannel
): ClusterExplorerV1_1.NodeContributor {
  return {
    contributesChildren(
      parent: ClusterExplorerV1_1.ClusterExplorerNode | undefined
    ): boolean {
      // Add children under active context nodes only
      return parent !== undefined && parent.nodeType === "context";
    },

    async getChildren(): Promise<ClusterExplorerV1_1.Node[]> {
      return [new KSailStatusNode(kubectl, outputChannel)];
    },
  };
}

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
 * Create a NodeUICustomizer that annotates KSail-managed contexts
 */
export function createKSailNodeUICustomizer(
  outputChannel: vscode.OutputChannel
): ClusterExplorerV1_1.NodeUICustomizer {
  // Cache known KSail cluster names to avoid repeated lookups
  let cachedKSailNames: Set<string> | undefined;
  let cacheTimestamp = 0;
  const CACHE_TTL_MS = 30_000;

  async function getKSailClusterNames(): Promise<Set<string>> {
    const now = Date.now();
    if (cachedKSailNames && now - cacheTimestamp < CACHE_TTL_MS) {
      return cachedKSailNames;
    }

    try {
      const clusters = await listClusters(outputChannel);
      cachedKSailNames = new Set(clusters.map((c) => c.name));
      cacheTimestamp = now;
    } catch {
      cachedKSailNames = cachedKSailNames ?? new Set();
    }

    return cachedKSailNames;
  }

  return {
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
      const ksailNames = await getKSailClusterNames();
      if (!ksailNames.has(clusterName)) {
        return;
      }

      // Annotate the context node
      const existing = treeItem.description ? `${treeItem.description} ` : "";
      treeItem.description = `${existing}(KSail)`;
    },
  };
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
