/**
 * Kubernetes Cloud Explorer Provider
 *
 * Registers KSail as a Cloud Provider in the Kubernetes extension's
 * "Clouds" view. KSail clusters appear as top-level resources with
 * status icons, context menus, and kubeconfig support.
 */

import * as vscode from "vscode";
import type { CloudExplorerV1, KubectlV1 } from "vscode-kubernetes-tools-api";
import {
  detectClusterStatus,
  detectDistribution,
  getContextName,
  listClusters,
  type ClusterStatus,
} from "../ksail/index.js";

/**
 * Cloud resource representing a KSail cluster in the Cloud Explorer tree.
 */
export interface KSailCloudCluster {
  readonly name: string;
  readonly provider: string;
  readonly status: ClusterStatus;
}

/**
 * TreeDataProvider for KSail clusters in the Kubernetes Cloud Explorer.
 */
export class KSailCloudTreeDataProvider implements vscode.TreeDataProvider<KSailCloudCluster> {
  private _onDidChangeTreeData = new vscode.EventEmitter<KSailCloudCluster | undefined | null | void>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private clusters: KSailCloudCluster[] = [];
  private isLoading = false;

  constructor(private outputChannel: vscode.OutputChannel) {
    this.loadClusters();
  }

  refresh(): void {
    this.loadClusters();
  }

  private async loadClusters(): Promise<void> {
    if (this.isLoading) {
      return;
    }
    this.isLoading = true;

    try {
      const clusterList = await listClusters(this.outputChannel);

      // Initial load with unknown status
      this.clusters = clusterList.map((c) => ({
        name: c.name,
        provider: c.provider,
        status: c.status ?? "unknown",
      }));
      this._onDidChangeTreeData.fire();

      // Detect status in parallel, then update
      const statusPromises = clusterList.map(async (cluster) => {
        try {
          return await detectClusterStatus(cluster.name, cluster.provider);
        } catch {
          return "unknown" as ClusterStatus;
        }
      });

      const statuses = await Promise.all(statusPromises);
      this.clusters = clusterList.map((c, i) => ({
        name: c.name,
        provider: c.provider,
        status: statuses[i],
      }));
      this._onDidChangeTreeData.fire();
    } catch (error) {
      this.outputChannel.appendLine(
        `Failed to load clusters: ${error instanceof Error ? error.message : String(error)}`
      );
      this.clusters = [];
      this._onDidChangeTreeData.fire();
    } finally {
      this.isLoading = false;
    }
  }

  getTreeItem(element: KSailCloudCluster): vscode.TreeItem {
    const item = new vscode.TreeItem(element.name, vscode.TreeItemCollapsibleState.None);

    // Status-based icon
    if (element.status === "running") {
      item.iconPath = new vscode.ThemeIcon("pass", new vscode.ThemeColor("testing.iconPassed"));
    } else if (element.status === "stopped") {
      item.iconPath = new vscode.ThemeIcon("circle-slash", new vscode.ThemeColor("testing.iconFailed"));
    } else {
      item.iconPath = new vscode.ThemeIcon("server");
    }

    // Provider as description
    item.description = element.provider.charAt(0).toUpperCase() + element.provider.slice(1);

    // Context value for menus + kubeconfig support
    const kubeconfigCtx = "kubernetes.providesKubeconfig";
    item.contextValue = `ksail-cluster-${element.status} ${kubeconfigCtx}`;

    const statusText = element.status === "unknown"
      ? "Status: Unknown"
      : `Status: ${element.status === "running" ? "Running" : "Stopped"}`;

    item.tooltip = new vscode.MarkdownString(
      `**${element.name}**\n\n` +
      `- Provider: ${element.provider}\n` +
      `- ${statusText}`
    );

    return item;
  }

  getChildren(): KSailCloudCluster[] {
    return this.clusters;
  }
}

/**
 * Create the KSail CloudProvider for the Kubernetes extension's Cloud Explorer.
 */
export function createKSailCloudProvider(
  outputChannel: vscode.OutputChannel,
  kubectlAPI?: KubectlV1
): {
  cloudProvider: CloudExplorerV1.CloudProvider;
  treeDataProvider: KSailCloudTreeDataProvider;
} {
  const treeDataProvider = new KSailCloudTreeDataProvider(outputChannel);

  const cloudProvider: CloudExplorerV1.CloudProvider = {
    cloudName: "KSail",
    treeDataProvider,
    async getKubeconfigYaml(cluster: KSailCloudCluster): Promise<string | undefined> {
      try {
        const distribution = await detectDistribution(cluster.name, cluster.provider);
        const contextName = getContextName(cluster.name, distribution);

        // Validate context name to prevent command injection
        if (!/^[\w@._-]+$/.test(contextName)) {
          vscode.window.showErrorMessage(
            `Invalid context name for "${cluster.name}": contains unexpected characters`
          );
          return undefined;
        }

        // Use the Kubernetes extension's KubectlV1 API when available
        if (kubectlAPI) {
          const result = await kubectlAPI.invokeCommand(
            `config view --minify --flatten --context ${contextName}`
          );
          if (result && result.code === 0 && result.stdout.trim()) {
            return result.stdout;
          }
          vscode.window.showErrorMessage(
            `Failed to get kubeconfig for "${cluster.name}": ${result?.stderr || "unknown error"}`
          );
          return undefined;
        }

        // Fallback: spawn kubectl binary directly
        const { spawn } = await import("child_process");
        return new Promise((resolve) => {
          const proc = spawn("kubectl", [
            "config", "view", "--minify", "--flatten",
            "--context", contextName,
          ]);

          let output = "";
          let stderr = "";
          proc.stdout.on("data", (data: Buffer) => { output += data.toString(); });
          proc.stderr.on("data", (data: Buffer) => { stderr += data.toString(); });

          proc.on("close", (code) => {
            if (code === 0 && output.trim()) {
              resolve(output);
            } else {
              vscode.window.showErrorMessage(
                `Failed to get kubeconfig for "${cluster.name}": ${stderr || "unknown error"}`
              );
              resolve(undefined);
            }
          });

          proc.on("error", (err) => {
            vscode.window.showErrorMessage(
              `Failed to get kubeconfig for "${cluster.name}": ${err.message}`
            );
            resolve(undefined);
          });
        });
      } catch (error) {
        vscode.window.showErrorMessage(
          `Failed to get kubeconfig: ${error instanceof Error ? error.message : String(error)}`
        );
        return undefined;
      }
    },
  };

  return { cloudProvider, treeDataProvider };
}
