/**
 * Cluster Status Tree View
 *
 * Displays real-time cluster health, pod summaries, and GitOps reconciliation
 * state in the KSail sidebar.
 */

import * as vscode from "vscode";
import {
  fetchClusterStatus,
  getPodLogs,
  type ClusterHealth,
  type ClusterStatusSnapshot,
  type GitOpsStatus,
  type NamespacePodSummary,
  type PodInfo,
} from "../ksail/kubectl.js";

/**
 * Union type for all tree item kinds
 */
export type StatusTreeItem =
  | HealthItem
  | SectionItem
  | NamespaceItem
  | PodItem
  | GitOpsItem
  | InfoItem;

/**
 * Overall cluster health indicator
 */
export class HealthItem extends vscode.TreeItem {
  constructor(health: ClusterHealth) {
    const label = HealthItem.label(health);
    super(label, vscode.TreeItemCollapsibleState.None);
    this.iconPath = HealthItem.icon(health);
    this.contextValue = "clusterHealth";
    this.tooltip = `Cluster health: ${health}`;
  }

  private static label(health: ClusterHealth): string {
    switch (health) {
      case "Healthy":
        return "✅ Healthy";
      case "Degraded":
        return "⚠️ Degraded";
      case "Error":
        return "❌ Error";
      default:
        return "❔ Unknown";
    }
  }

  private static icon(health: ClusterHealth): vscode.ThemeIcon {
    switch (health) {
      case "Healthy":
        return new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
      case "Degraded":
        return new vscode.ThemeIcon("warning", new vscode.ThemeColor("editorWarning.foreground"));
      case "Error":
        return new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
      default:
        return new vscode.ThemeIcon("question");
    }
  }
}

/**
 * Section header (Pods, GitOps)
 */
export class SectionItem extends vscode.TreeItem {
  constructor(
    label: string,
    public readonly sectionType: "pods" | "gitops",
    public readonly children: StatusTreeItem[]
  ) {
    super(label, vscode.TreeItemCollapsibleState.Expanded);
    this.contextValue = `section-${sectionType}`;
  }
}

/**
 * Namespace item containing pods
 */
export class NamespaceItem extends vscode.TreeItem {
  constructor(
    public readonly summary: NamespacePodSummary,
    public readonly namespacePods: PodInfo[]
  ) {
    super(summary.namespace, vscode.TreeItemCollapsibleState.Collapsed);
    this.description = NamespaceItem.buildDescription(summary);
    this.contextValue = "namespace";
    this.iconPath = new vscode.ThemeIcon("symbol-namespace");
    this.tooltip = new vscode.MarkdownString(
      `**${summary.namespace}**\n\n` +
        `- Running: ${summary.running}\n` +
        `- Pending: ${summary.pending}\n` +
        `- Failed: ${summary.failed}\n` +
        `- Total: ${summary.total}`
    );
  }

  private static buildDescription(summary: NamespacePodSummary): string {
    const parts: string[] = [];
    if (summary.running > 0) {
      parts.push(`${summary.running} running`);
    }
    if (summary.pending > 0) {
      parts.push(`${summary.pending} pending`);
    }
    if (summary.failed > 0) {
      parts.push(`${summary.failed} failed`);
    }
    return parts.join(", ") || `${summary.total} pods`;
  }
}

/**
 * Individual pod item
 */
export class PodItem extends vscode.TreeItem {
  constructor(public readonly pod: PodInfo) {
    super(pod.name, vscode.TreeItemCollapsibleState.None);
    this.description = `${pod.phase} (${pod.ready})`;
    this.contextValue = `pod-${pod.phase.toLowerCase()}`;
    this.iconPath = PodItem.icon(pod.phase);
    this.tooltip = new vscode.MarkdownString(
      `**${pod.name}**\n\n` +
        `- Namespace: ${pod.namespace}\n` +
        `- Phase: ${pod.phase}\n` +
        `- Ready: ${pod.ready}\n` +
        `- Restarts: ${pod.restarts}`
    );

    // Clicking a failed/pending pod opens logs
    if (pod.phase === "Failed" || pod.phase === "Pending") {
      this.command = {
        command: "ksail.status.showPodLogs",
        title: "Show Pod Logs",
        arguments: [pod.namespace, pod.name],
      };
    }
  }

  private static icon(phase: string): vscode.ThemeIcon {
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
}

/**
 * GitOps reconciliation item
 */
export class GitOpsItem extends vscode.TreeItem {
  constructor(public readonly status: GitOpsStatus) {
    super(status.name, vscode.TreeItemCollapsibleState.None);
    this.description = GitOpsItem.buildDescription(status);
    this.contextValue = "gitopsResource";
    this.iconPath = GitOpsItem.icon(status.ready);
    this.tooltip = new vscode.MarkdownString(
      `**${status.kind}/${status.name}**\n\n` +
        `- Namespace: ${status.namespace}\n` +
        `- Ready: ${status.ready}\n` +
        `- Status: ${status.status}`
    );
  }

  private static buildDescription(status: GitOpsStatus): string {
    if (status.ready === "True") {
      return "Ready";
    }
    if (status.ready === "False") {
      return `Failed: ${status.status}`;
    }
    return status.status || "Unknown";
  }

  private static icon(ready: string): vscode.ThemeIcon {
    switch (ready) {
      case "True":
        return new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
      case "False":
        return new vscode.ThemeIcon("error", new vscode.ThemeColor("testing.iconFailed"));
      default:
        return new vscode.ThemeIcon("loading~spin");
    }
  }
}

/**
 * Informational message item (e.g., "No cluster running")
 */
export class InfoItem extends vscode.TreeItem {
  constructor(message: string, icon?: vscode.ThemeIcon) {
    super(message, vscode.TreeItemCollapsibleState.None);
    this.contextValue = "info";
    this.iconPath = icon ?? new vscode.ThemeIcon("info");
  }
}

/**
 * Tree data provider for cluster status
 */
export class ClusterStatusTreeDataProvider
  implements vscode.TreeDataProvider<StatusTreeItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<
    StatusTreeItem | undefined | null | void
  >();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private snapshot: ClusterStatusSnapshot | undefined;
  private pollTimer: ReturnType<typeof setInterval> | undefined;
  private treeView: vscode.TreeView<StatusTreeItem> | undefined;
  private isPolling = false;

  constructor(private outputChannel: vscode.OutputChannel) {}

  /**
   * Set the tree view instance for programmatic access
   */
  setTreeView(treeView: vscode.TreeView<StatusTreeItem>): void {
    this.treeView = treeView;
  }

  /**
   * Start polling for cluster status updates
   */
  startPolling(intervalMs = 10_000): void {
    this.stopPolling();
    // Initial fetch
    this.refreshStatus();
    this.pollTimer = setInterval(() => this.refreshStatus(), intervalMs);
  }

  /**
   * Stop polling
   */
  stopPolling(): void {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = undefined;
    }
  }

  /**
   * Manual refresh
   */
  refresh(): void {
    this.refreshStatus();
  }

  /**
   * Fetch and update status
   */
  private async refreshStatus(): Promise<void> {
    if (this.isPolling) {
      return;
    }

    this.isPolling = true;

    try {
      this.snapshot = await fetchClusterStatus();
    } catch (error) {
      this.outputChannel.appendLine(
        `Failed to fetch cluster status: ${error instanceof Error ? error.message : String(error)}`
      );
      this.snapshot = {
        health: "Unknown",
        podSummaries: [],
        pods: [],
        gitopsStatuses: [],
        gitopsEngine: undefined,
        error: "Failed to connect to cluster",
      };
    } finally {
      this.isPolling = false;
      this._onDidChangeTreeData.fire();
    }
  }

  /**
   * Get the current health status (for status bar)
   */
  getHealth(): ClusterHealth {
    return this.snapshot?.health ?? "Unknown";
  }

  /**
   * Get the current snapshot (for status bar detail)
   */
  getSnapshot(): ClusterStatusSnapshot | undefined {
    return this.snapshot;
  }

  getTreeItem(element: StatusTreeItem): vscode.TreeItem {
    return element;
  }

  getChildren(element?: StatusTreeItem): StatusTreeItem[] {
    if (!element) {
      return this.getRootChildren();
    }

    if (element instanceof SectionItem) {
      return element.children;
    }

    if (element instanceof NamespaceItem) {
      return element.namespacePods.map((pod) => new PodItem(pod));
    }

    return [];
  }

  private getRootChildren(): StatusTreeItem[] {
    if (!this.snapshot) {
      return [new InfoItem("Loading cluster status...", new vscode.ThemeIcon("loading~spin"))];
    }

    if (this.snapshot.error) {
      return [
        new InfoItem(
          "No cluster connected",
          new vscode.ThemeIcon("info")
        ),
        new InfoItem(
          "Create or start a cluster to see status",
          new vscode.ThemeIcon("lightbulb")
        ),
      ];
    }

    if (this.snapshot.pods.length === 0 && !this.snapshot.gitopsEngine) {
      return [
        new InfoItem(
          "No pods found",
          new vscode.ThemeIcon("info")
        ),
      ];
    }

    const items: StatusTreeItem[] = [];

    // Health indicator
    items.push(new HealthItem(this.snapshot.health));

    // Pod summaries section
    if (this.snapshot.podSummaries.length > 0) {
      const totalRunning = this.snapshot.podSummaries.reduce((s, n) => s + n.running, 0);
      const totalPending = this.snapshot.podSummaries.reduce((s, n) => s + n.pending, 0);
      const totalFailed = this.snapshot.podSummaries.reduce((s, n) => s + n.failed, 0);
      const totalPods = this.snapshot.podSummaries.reduce((s, n) => s + n.total, 0);

      const podLabel = `Pods (${totalRunning}/${totalPods} running` +
        (totalPending > 0 ? `, ${totalPending} pending` : "") +
        (totalFailed > 0 ? `, ${totalFailed} failed` : "") +
        ")";

      const namespaceItems = this.snapshot.podSummaries.map((summary) => {
        const namespacePods = this.snapshot!.pods.filter(
          (p) => p.namespace === summary.namespace
        );
        return new NamespaceItem(summary, namespacePods);
      });

      items.push(new SectionItem(podLabel, "pods", namespaceItems));
    }

    // GitOps section
    if (this.snapshot.gitopsEngine && this.snapshot.gitopsStatuses.length > 0) {
      const readyCount = this.snapshot.gitopsStatuses.filter(
        (s) => s.ready === "True"
      ).length;
      const total = this.snapshot.gitopsStatuses.length;
      const gitopsLabel = `${this.snapshot.gitopsEngine} (${readyCount}/${total} ready)`;

      const gitopsItems = this.snapshot.gitopsStatuses.map(
        (s) => new GitOpsItem(s)
      );

      items.push(new SectionItem(gitopsLabel, "gitops", gitopsItems));
    } else if (this.snapshot.gitopsEngine) {
      items.push(
        new SectionItem(
          `${this.snapshot.gitopsEngine} (no resources)`,
          "gitops",
          [new InfoItem("No reconciliation resources found")]
        )
      );
    }

    return items;
  }
}

/**
 * Show pod logs in an output channel
 */
export async function showPodLogs(
  namespace: string,
  podName: string
): Promise<void> {
  const logChannel = vscode.window.createOutputChannel(
    `KSail: ${namespace}/${podName}`
  );
  logChannel.show();
  logChannel.appendLine(`Fetching logs for ${namespace}/${podName}...`);

  const logs = await getPodLogs(namespace, podName);
  logChannel.appendLine(logs);
}
