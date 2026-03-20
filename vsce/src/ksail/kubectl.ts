/**
 * Kubectl Utilities
 *
 * Provides functions for querying Kubernetes cluster state via the
 * Kubernetes extension's KubectlV1 API.
 */

import type { KubectlV1 } from "vscode-kubernetes-tools-api";

/**
 * Kubernetes DNS label regex for validating namespace and pod names.
 */
const K8S_DNS_LABEL = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

/**
 * Invoke a kubectl command via the Kubernetes extension's API.
 */
async function invokeKubectl(
  kubectl: KubectlV1,
  args: string
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  const result = await kubectl.invokeCommand(args);
  if (!result) {
    return { stdout: "", stderr: "kubectl not configured", exitCode: 1 };
  }
  return { stdout: result.stdout, stderr: result.stderr, exitCode: result.code };
}

/**
 * Get logs for a specific pod
 */
export async function getPodLogs(
  kubectl: KubectlV1,
  namespace: string,
  podName: string,
  tailLines = 100
): Promise<string> {
  if (!K8S_DNS_LABEL.test(namespace) || !K8S_DNS_LABEL.test(podName)) {
    return "Invalid namespace or pod name: must match Kubernetes DNS label format [a-z0-9]([a-z0-9-]*[a-z0-9])?";
  }

  const result = await invokeKubectl(
    kubectl,
    `logs ${podName} -n ${namespace} --tail ${tailLines}`
  );

  if (result.exitCode !== 0) {
    return `Failed to get logs: ${result.stderr}`;
  }

  return result.stdout;
}
