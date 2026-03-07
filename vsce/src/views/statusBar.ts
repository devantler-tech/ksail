/**
 * Cluster Status Bar
 *
 * Displays a compact cluster health indicator in the VSCode status bar.
 */

import * as vscode from "vscode";
import type { ClusterStatusTreeDataProvider } from "./clusterStatusView.js";

/**
 * Create and manage the cluster status bar item
 */
export class ClusterStatusBar {
  private statusBarItem: vscode.StatusBarItem;
  private disposables: vscode.Disposable[] = [];

  constructor(private statusProvider: ClusterStatusTreeDataProvider) {
    this.statusBarItem = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Left,
      50
    );
    this.statusBarItem.command = "ksail.status.refresh";
    this.statusBarItem.name = "KSail Cluster Status";

    // Update when tree data changes
    const changeListener = statusProvider.onDidChangeTreeData(() => {
      this.update();
    });
    this.disposables.push(changeListener);

    // Initial update
    this.update();
    this.statusBarItem.show();
  }

  /**
   * Update the status bar display
   */
  private update(): void {
    const health = this.statusProvider.getHealth();
    const snapshot = this.statusProvider.getSnapshot();

    if (!snapshot || snapshot.error) {
      // No connected cluster or an error retrieving the snapshot
      this.statusBarItem.text = "$(question) KSail: No Cluster";
      this.statusBarItem.backgroundColor = undefined;
    } else {
      switch (health) {
        case "Healthy":
          this.statusBarItem.text = "$(pass) KSail: Healthy";
          this.statusBarItem.backgroundColor = undefined;
          break;
        case "Degraded":
          this.statusBarItem.text = "$(warning) KSail: Degraded";
          this.statusBarItem.backgroundColor = new vscode.ThemeColor(
            "statusBarItem.warningBackground"
          );
          break;
        case "Error":
          this.statusBarItem.text = "$(error) KSail: Error";
          this.statusBarItem.backgroundColor = new vscode.ThemeColor(
            "statusBarItem.errorBackground"
          );
          break;
        case "Unknown":
        default:
          this.statusBarItem.text = "$(question) KSail: Unknown";
          this.statusBarItem.backgroundColor = undefined;
          break;
      }
    }

    // Build tooltip
    if (snapshot && !snapshot.error) {
      const totalPods = snapshot.pods.length;
      const running = snapshot.pods.filter((p) => p.phase === "Running").length;
      const parts = [`Pods: ${running}/${totalPods} running`];

      if (snapshot.gitopsEngine) {
        const readyCount = snapshot.gitopsStatuses.filter(
          (s) => s.ready === "True"
        ).length;
        parts.push(
          `${snapshot.gitopsEngine}: ${readyCount}/${snapshot.gitopsStatuses.length} ready`
        );
      }

      parts.push("Click to refresh");
      this.statusBarItem.tooltip = parts.join("\n");
    } else {
      this.statusBarItem.tooltip = "KSail: Click to refresh cluster status";
    }
  }

  /**
   * Dispose the status bar item
   */
  dispose(): void {
    this.statusBarItem.dispose();
    for (const d of this.disposables) {
      d.dispose();
    }
  }
}
