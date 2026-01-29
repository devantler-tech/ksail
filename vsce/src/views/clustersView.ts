/**
 * Clusters Tree View
 *
 * Displays KSail clusters in the sidebar using direct binary execution.
 */

import * as vscode from "vscode";
import { detectClusterStatus, listClusters, type ClusterInfo } from "../ksail/index.js";

/**
 * Base type for tree items (cluster or pending)
 */
export type ClusterTreeItem = ClusterItem | PendingClusterItem;

/**
 * Cluster item in the tree view
 */
export class ClusterItem extends vscode.TreeItem {
  constructor(public readonly cluster: ClusterInfo) {
    super(cluster.name, vscode.TreeItemCollapsibleState.None);

    this.contextValue = "cluster";
    this.description = cluster.provider;

    // Set icon based on status
    if (cluster.status === "running") {
      this.iconPath = new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
    } else if (cluster.status === "stopped") {
      this.iconPath = new vscode.ThemeIcon("circle-slash", new vscode.ThemeColor("testing.iconFailed"));
    } else {
      this.iconPath = new vscode.ThemeIcon("server");
    }

    const statusText = cluster.status === "unknown" ? "Status: Unknown" : `Status: ${cluster.status === "running" ? "Running" : "Stopped"}`;
    this.tooltip = new vscode.MarkdownString(
      `**${cluster.name}**\n\n` +
        `- Provider: ${cluster.provider}\n` +
        `- ${statusText}`
    );
  }
}

/**
 * Pending cluster item shown during creation
 */
export class PendingClusterItem extends vscode.TreeItem {
  constructor(
    public readonly clusterName: string,
    public statusMessage = "Creating..."
  ) {
    super(clusterName, vscode.TreeItemCollapsibleState.None);

    this.contextValue = "pendingCluster";
    this.description = statusMessage;

    // Animated spinner icon
    this.iconPath = new vscode.ThemeIcon("loading~spin");

    this.tooltip = new vscode.MarkdownString(
      `**${clusterName}**\n\n` +
      `Status: ${statusMessage}`
    );
  }

  /**
   * Update the status message
   */
  updateStatus(message: string): void {
    this.statusMessage = message;
    this.description = message;
    this.tooltip = new vscode.MarkdownString(
      `**${this.clusterName}**\n\n` +
      `Status: ${message}`
    );
  }
}

/**
 * Tree data provider for clusters
 */
export class ClustersTreeDataProvider
  implements vscode.TreeDataProvider<ClusterTreeItem> {
  private _onDidChangeTreeData = new vscode.EventEmitter<
    ClusterTreeItem | undefined | null | void
  >();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private clusters: ClusterItem[] = [];
  private pendingClusters = new Map<string, PendingClusterItem>();
  private isLoading = false;
  private treeView: vscode.TreeView<ClusterTreeItem> | undefined;

  constructor(private outputChannel: vscode.OutputChannel) {
    // Load clusters on construction
    this.loadClusters();
  }

  /**
   * Set the tree view instance for programmatic access
   */
  setTreeView(treeView: vscode.TreeView<ClusterTreeItem>): void {
    this.treeView = treeView;
  }

  /**
   * Refresh the tree view
   */
  refresh(): void {
    this.loadClusters();
  }

  /**
   * Add a pending cluster to show during creation
   */
  addPendingCluster(name: string): void {
    const pending = new PendingClusterItem(name);
    this.pendingClusters.set(name, pending);
    this._onDidChangeTreeData.fire();
  }

  /**
   * Update the status of a pending cluster
   */
  updatePendingCluster(name: string, status: string): void {
    const pending = this.pendingClusters.get(name);
    if (pending) {
      pending.updateStatus(status);
      this._onDidChangeTreeData.fire();
    }
  }

  /**
   * Remove a pending cluster (after creation completes or fails)
   */
  removePendingCluster(name: string): void {
    this.pendingClusters.delete(name);
    this._onDidChangeTreeData.fire();
  }

  /**
   * Load clusters via KSail binary
   */
  private async loadClusters(): Promise<void> {
    if (this.isLoading) {
      return;
    }

    this.isLoading = true;

    // Show loading message in tree view
    if (this.treeView) {
      this.treeView.message = "Loading clusters...";
    }

    try {
      const clusterList = await listClusters(this.outputChannel);

      // Load clusters first with unknown status
      this.clusters = clusterList.map((c) => new ClusterItem(c));
      this._onDidChangeTreeData.fire();

      // Detect status for each cluster asynchronously
      for (const cluster of clusterList) {
        detectClusterStatus(cluster.name, cluster.provider).then((status) => {
          cluster.status = status;
          // Update the corresponding cluster item
          const clusterItem = this.clusters.find((item) => item.cluster.name === cluster.name);
          if (clusterItem) {
            // Recreate the item with updated status
            const index = this.clusters.indexOf(clusterItem);
            this.clusters[index] = new ClusterItem(cluster);
            this._onDidChangeTreeData.fire();
          }
        }).catch(() => {
          // Ignore errors in status detection
        });
      }

      // Clear message on success
      if (this.treeView) {
        this.treeView.message = undefined;
      }
    } catch (error) {
      this.outputChannel.appendLine(
        `Failed to load clusters: ${error instanceof Error ? error.message : String(error)}`
      );
      this.clusters = [];

      // Show error in tree view
      if (this.treeView) {
        this.treeView.message = "Failed to load clusters. Check KSail binary.";
      }
    } finally {
      this.isLoading = false;
      this._onDidChangeTreeData.fire();
    }
  }

  getTreeItem(element: ClusterTreeItem): vscode.TreeItem {
    return element;
  }

  getChildren(): ClusterTreeItem[] {
    // Pending clusters appear at the top
    const pending = Array.from(this.pendingClusters.values());
    return [...pending, ...this.clusters];
  }
}
