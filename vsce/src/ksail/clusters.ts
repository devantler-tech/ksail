/**
 * Cluster Operations
 *
 * Provides functions for managing KSail clusters via direct binary execution.
 */

import * as vscode from "vscode";
import { runKsailCommand } from "./binary.js";

/**
 * Cluster information
 */
export interface ClusterInfo {
  name: string;
  status: string;
  distribution: string;
  provider: string;
}

/**
 * Options for creating a cluster
 */
export interface CreateClusterOptions {
  name?: string;
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
 *
 * Types are strings to support dynamic schema-driven values.
 * The MCP schema determines the actual valid enum values.
 */
export interface InitClusterOptions {
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
  outputDir?: string;
  force?: boolean;
}

/**
 * List all clusters
 *
 * The CLI outputs text format: "PROVIDER=docker NAME=local, NAME=test"
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
 * Format: "PROVIDER=docker NAME=local, NAME=test"
 * Each provider section contains comma-separated NAME=value pairs.
 * Lines may contain multiple clusters for the same provider.
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
    // Extract PROVIDER=value
    const providerMatch = line.match(/PROVIDER=(\w+)/i);
    const provider = providerMatch ? providerMatch[1] : "unknown";

    // Extract all NAME=value pairs
    const nameMatches = line.matchAll(/NAME=([^,\s]+)/gi);
    for (const match of nameMatches) {
      clusters.push({
        name: match[1],
        status: "unknown", // Status not provided in list output
        distribution: "unknown", // Distribution not provided in list output
        provider: provider,
      });
    }
  }

  return clusters;
}

/**
 * Create a cluster
 */
export async function createCluster(
  options: CreateClusterOptions = {},
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "create"];

  if (options.name) {
    args.push("--name", options.name);
  }

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
 * Get cluster info
 *
 * Uses the ksail.yaml context in the current working directory.
 */
export async function getClusterInfo(
  outputChannel?: vscode.OutputChannel
): Promise<string> {
  const args = ["cluster", "info"];

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to get cluster info: ${result.stderr || result.stdout}`);
  }

  return result.stdout;
}

/**
 * Connect to a cluster with K9s
 *
 * Uses the ksail.yaml context in the current working directory.
 * Connect runs K9s which is interactive.
 */
export async function connectCluster(
  outputChannel?: vscode.OutputChannel
): Promise<void> {
  const args = ["cluster", "connect"];

  const result = await runKsailCommand(args, undefined, outputChannel);

  if (result.exitCode !== 0) {
    throw new Error(`Failed to connect to cluster: ${result.stderr || result.stdout}`);
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

  if (options.name) {
    args.push("--name", options.name);
  }
  if (options.distribution) {
    args.push("--distribution", options.distribution);
  }
  if (options.provider) {
    args.push("--provider", options.provider);
  }
  if (options.cni) {
    args.push("--cni", options.cni);
  }
  if (options.csi) {
    args.push("--csi", options.csi);
  }
  if (options.metricsServer) {
    args.push("--metrics-server", options.metricsServer);
  }
  if (options.certManager) {
    args.push("--cert-manager", options.certManager);
  }
  if (options.policyEngine) {
    args.push("--policy-engine", options.policyEngine);
  }
  if (options.gitopsEngine) {
    args.push("--gitops-engine", options.gitopsEngine);
  }
  if (options.controlPlanes !== undefined) {
    args.push("--control-planes", options.controlPlanes.toString());
  }
  if (options.workers !== undefined) {
    args.push("--workers", options.workers.toString());
  }
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
