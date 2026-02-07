/**
 * Cluster Operations
 *
 * Provides functions for managing KSail clusters via direct binary execution.
 */

import * as vscode from "vscode";
import { runKsailCommand } from "./binary.js";

/**
 * Cluster status
 */
export type ClusterStatus = "running" | "stopped" | "unknown";

/**
 * Cluster information
 *
 * Note: The CLI `cluster list` command only returns name and provider.
 * Status is detected separately for each cluster.
 */
export interface ClusterInfo {
  name: string;
  provider: string;
  status?: ClusterStatus;
}

/**
 * Common cluster options shared between create and init
 *
 * Types are strings to support dynamic schema-driven values.
 * The MCP schema determines the actual valid enum values.
 */
export interface CommonClusterOptions {
  name?: string;
  distribution?: string;
  provider?: string;
  cni?: string;
  csi?: string;
  metricsServer?: string;
  certManager?: string;
  policyEngine?: string;
  gitopsEngine?: string;
  controlPlanes?: number;
  workers?: number;
}

/**
 * Options for creating a cluster
 */
export interface CreateClusterOptions extends CommonClusterOptions {
  distributionConfigPath?: string;
}

/**
 * Options for deleting a cluster
 */
export interface DeleteClusterOptions {
  name?: string;
  deleteStorage?: boolean;
  force?: boolean;
}

/**
 * Options for initializing a cluster
 */
export interface InitClusterOptions extends CommonClusterOptions {
  outputDir?: string;
  force?: boolean;
}

/**
 * Add common cluster options to CLI args array
 */
function addCommonClusterArgs(args: string[], options: CommonClusterOptions): void {
  const stringFlags: [keyof CommonClusterOptions, string][] = [
    ["name", "--name"],
    ["distribution", "--distribution"],
    ["provider", "--provider"],
    ["cni", "--cni"],
    ["csi", "--csi"],
    ["metricsServer", "--metrics-server"],
    ["certManager", "--cert-manager"],
    ["policyEngine", "--policy-engine"],
    ["gitopsEngine", "--gitops-engine"],
  ];

  for (const [key, flag] of stringFlags) {
    const value = options[key];
    if (typeof value === "string" && value) {
      args.push(flag, value);
    }
  }

  if (options.controlPlanes !== undefined) {
    args.push("--control-planes", options.controlPlanes.toString());
  }
  if (options.workers !== undefined) {
    args.push("--workers", options.workers.toString());
  }
}

/**
 * List all clusters
 *
 * The CLI outputs text format: "docker: local, test"
 * or "No clusters found." when empty.
 */
export async function listClusters(
  outputChannel?: vscode.OutputChannel
): Promise<ClusterInfo[]> {
  const result = await runKsailCommand(
    ["cluster", "list"],
    undefined,
    outputChannel
  );

  if (result.exitCode !== 0) {
    throw new Error(`Failed to list clusters: ${result.stderr}`);
  }

  return parseClusterListOutput(result.stdout);
}

/**
 * Parse cluster list text output
 *
 * Format: "docker: cluster1, cluster2"
 * Each line starts with provider name followed by colon, then comma-separated cluster names.
 */
function parseClusterListOutput(output: string): ClusterInfo[] {
  const clusters: ClusterInfo[] = [];
  const trimmed = output.trim();

  // Handle empty or "no clusters" output
  if (!trimmed || trimmed.toLowerCase().includes("no clusters found")) {
    return [];
  }

  // Split by lines in case multiple provider lines exist
  const lines = trimmed.split("\n").filter((line) => line.trim());

  for (const line of lines) {
    // Split on colon to separate provider from cluster names
    const colonIndex = line.indexOf(":");
    if (colonIndex === -1) {
      continue; // Skip lines without colon separator
    }

    const provider = line.substring(0, colonIndex).trim();
    const clustersPart = line.substring(colonIndex + 1).trim();

    if (!clustersPart) {
      continue; // No clusters for this provider
    }

    // Split cluster names by comma and trim each
    const clusterNames = clustersPart.split(",").map((n) => n.trim()).filter((n) => n);

    for (const name of clusterNames) {
      clusters.push({
        name: name,
        provider: provider,
        status: "unknown",
      });
    }
  }

  return clusters;
}

/**
 * Distribution type for Docker-based clusters
 */
export type Distribution = "kind" | "k3d" | "talos" | "unknown";

/**
 * Helper to spawn Docker and collect output
 */
async function spawnDocker(
  args: string[],
  defaultValue: string
): Promise<string> {
  try {
    const { spawn } = await import("child_process");

    return new Promise((resolve) => {
      const proc = spawn("docker", args);

      let output = "";
      proc.stdout.on("data", (data: Buffer) => {
        output += data.toString();
      });

      proc.on("close", (code) => {
        resolve(code === 0 ? output.trim() : defaultValue);
      });

      proc.on("error", () => {
        resolve(defaultValue);
      });
    });
  } catch {
    return defaultValue;
  }
}

/**
 * Detect cluster distribution by checking Docker container names
 * Kind uses pattern: {name}-control-plane, {name}-worker
 * K3d uses pattern: k3d-{name}-server-0, k3d-{name}-agent-0
 */
export async function detectDistribution(
  clusterName: string,
  provider: string
): Promise<Distribution> {
  // Hetzner provider always uses Talos
  if (provider.toLowerCase() === "hetzner") {
    return "talos";
  }

  // Only check Docker containers for Docker provider
  if (provider.toLowerCase() !== "docker") {
    return "unknown";
  }

  const output = await spawnDocker(
    ["ps", "-a", "--filter", `name=${clusterName}`, "--format", "{{.Names}}"],
    ""
  );

  const containers = output.split("\n").filter(Boolean);
  if (containers.length === 0) {
    return "unknown";
  }
  if (containers.some((name) => name.startsWith("k3d-"))) {
    return "k3d";
  }
  if (containers.some((name) => name.includes("-control-plane") || name.includes("-worker"))) {
    return "kind";
  }
  return "unknown";
}

/**
 * Get the Kubernetes context name for a cluster
 * Kind: kind-{name}
 * K3d: k3d-{name}
 * Talos: admin@{name} or {name}
 */
export function getContextName(
  clusterName: string,
  distribution: Distribution
): string {
  switch (distribution) {
    case "kind":
      return `kind-${clusterName}`;
    case "k3d":
      return `k3d-${clusterName}`;
    case "talos":
      return `admin@${clusterName}`;
    default:
      // Fallback: try the cluster name directly
      return clusterName;
  }
}

/**
 * Detect cluster status by checking Docker container state
 * Only works for Docker-based providers (Kind, K3d)
 */
export async function detectClusterStatus(
  clusterName: string,
  provider: string
): Promise<ClusterStatus> {
  // Only check status for Docker-based providers
  if (provider.toLowerCase() !== "docker") {
    return "unknown";
  }

  const output = await spawnDocker(
    ["ps", "-a", "--filter", `name=${clusterName}`, "--format", "{{.State}}"],
    ""
  );

  const states = output.split("\n").filter(Boolean);
  if (states.length === 0) {
    return "unknown";
  }
  if (states.some((state) => state === "running")) {
    return "running";
  }
  return "stopped";
}

/**
 * Create a cluster
 *
 * Note: The CLI reads ksail.yaml from the current working directory automatically.
 * There is no --config flag.
 */
export async function createCluster(
  options: CreateClusterOptions = {},
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "create"];

  if (options.distributionConfigPath) {
    args.push("--distribution-config", options.distributionConfigPath);
  }
  addCommonClusterArgs(args, options);

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to create cluster: ${result.stderr || result.stdout}`);
  }
}

/**
 * Delete a cluster
 */
export async function deleteCluster(
  options: DeleteClusterOptions = {},
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "delete"];

  if (options.name) {
    args.push("--name", options.name);
  }
  if (options.deleteStorage) {
    args.push("--delete-storage");
  }
  if (options.force) {
    args.push("--force");
  }

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to delete cluster: ${result.stderr || result.stdout}`);
  }
}

/**
 * Start a cluster
 */
export async function startCluster(
  name?: string,
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "start"];

  if (name) {
    args.push("--name", name);
  }

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to start cluster: ${result.stderr || result.stdout}`);
  }
}

/**
 * Stop a cluster
 */
export async function stopCluster(
  name?: string,
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "stop"];

  if (name) {
    args.push("--name", name);
  }

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to stop cluster: ${result.stderr || result.stdout}`);
  }
}

/**
 * Initialize a new cluster configuration
 */
export async function initCluster(
  options: InitClusterOptions = {},
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "init"];

  addCommonClusterArgs(args, options);

  if (options.outputDir) {
    args.push("--output", options.outputDir);
  }
  if (options.force) {
    args.push("--force");
  }

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to init cluster: ${result.stderr || result.stdout}`);
  }
}
