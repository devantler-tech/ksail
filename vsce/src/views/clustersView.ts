/**
 * Clusters Tree View
 *
 * Displays KSail clusters in the sidebar using direct binary execution.
 */

import * as vscode from "vscode";
import { listClusters, type ClusterInfo } from "../ksail/index.js";

/**
 * Cluster item in the tree view
 */
export class ClusterItem extends vscode.TreeItem {
  constructor(public readonly cluster: ClusterInfo) {
    super(cluster.name, vscode.TreeItemCollapsibleState.None);

    this.contextValue = "cluster";
    this.description = `${cluster.distribution} - ${cluster.status}`;

    // Set icon based on status
    if (cluster.status === "running") {
      this.iconPath = new vscode.ThemeIcon(
        "pass-filled",
        new vscode.ThemeColor("testing.iconPassed")
      );
    } else if (cluster.status === "stopped") {
      this.iconPath = new vscode.ThemeIcon(
        "circle-outline",
        new vscode.ThemeColor("testing.iconSkipped")
      );
    } else {
      this.iconPath = new vscode.ThemeIcon("question");
    }

    this.tooltip = new vscode.MarkdownString(
      `**${cluster.name}**\n\n` +
        `- Distribution: ${cluster.distribution}\n` +
        `- Provider: ${cluster.provider}\n` +
        `- Status: ${cluster.status}`
    );
  }
}

/**
 * Tree data provider for clusters
 */
export class ClustersTreeDataProvider
  implements vscode.TreeDataProvider<ClusterItem>
{
  private _onDidChangeTreeData = new vscode.EventEmitter<
    ClusterItem | undefined | null | void
  >();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private clusters: ClusterItem[] = [];
  private isLoading = false;

  constructor(private outputChannel: vscode.OutputChannel) {}

  /**
   * Refresh the tree view
   */
  refresh(): void {
    this.loadClusters();
  }

  /**
   * Load clusters via KSail binary
   */
  private async loadClusters(): Promise<void> {
    if (this.isLoading) {
      return;
    }

    this.isLoading = true;

    try {
      const clusterList = await listClusters(this.outputChannel);
      this.clusters = clusterList.map((c) => new ClusterItem(c));
    } catch (error) {
      this.outputChannel.appendLine(
        `Failed to load clusters: ${error instanceof Error ? error.message : String(error)}`
      );
      this.clusters = [];
    } finally {
      this.isLoading = false;
      this._onDidChangeTreeData.fire();
    }
  }

  getTreeItem(element: ClusterItem): vscode.TreeItem {
    return element;
  }

  getChildren(): ClusterItem[] {
    return this.clusters;
  }
}
