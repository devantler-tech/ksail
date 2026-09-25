import { readFileSync } from "node:fs";
import { expect, test } from "@playwright/test";
import {
  applyManifests,
  clusterPath,
  deleteCluster,
  deleteResource,
  downloadKubeconfig,
  execWebSocketURL,
  listResources,
  logsEventSourceURL,
  scaleResource,
  updateCluster,
} from "../src/api.ts";

// An unmanaged cluster is listed under its raw kubeconfig context name; aws eks update-kubeconfig
// writes an ARN, whose "/" would otherwise become an extra path segment.
const arn = "arn:aws:eks:us-east-1:123456789012:cluster/prod";
const arnPath = `/api/v1/clusters/default/${encodeURIComponent(arn)}`;

// requestedPaths calls each request helper with fetch stubbed, and returns the paths it asked for.
async function requestedPaths(calls: Array<() => Promise<unknown>>): Promise<string[]> {
  const paths: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    paths.push(String(input));
    return new Response(null, { status: 204 });
  }) as typeof fetch;
  try {
    for (const call of calls) {
      await call();
    }
  } finally {
    globalThis.fetch = realFetch;
  }

  return paths;
}

test.describe("clusterPath", () => {
  test("escapes a cluster name that is not URL-safe", () => {
    expect(clusterPath("default", arn)).toBe(
      "/api/v1/clusters/default/arn%3Aaws%3Aeks%3Aus-east-1%3A123456789012%3Acluster%2Fprod",
    );
    expect(clusterPath("team a", "name?with#marks")).toBe("/api/v1/clusters/team%20a/name%3Fwith%23marks");
  });

  test("leaves a DNS-label name unchanged", () => {
    expect(clusterPath("default", "dev-cluster")).toBe("/api/v1/clusters/default/dev-cluster");
  });
});

test.describe("cluster-scoped requests", () => {
  test("escape the cluster name in every request", async () => {
    const target = { namespace: "default", name: arn, kind: "Deployment", resourceName: "web", resourceNamespace: "apps" };
    const paths = await requestedPaths([
      () => updateCluster("default", arn, { metadata: { name: arn } } as never),
      () => deleteCluster("default", arn),
      () => listResources("default", arn, "Pod"),
      () => applyManifests("default", arn, "kind: ConfigMap", true),
      () => scaleResource(target, 2),
      () => deleteResource(target),
    ]);

    expect(paths).toEqual([
      arnPath,
      arnPath,
      `${arnPath}/resources?kind=Pod`,
      `${arnPath}/apply?dryRun=true`,
      `${arnPath}/resources/Deployment/web/scale?namespace=apps`,
      `${arnPath}/resources/Deployment/web?namespace=apps`,
    ]);
  });

  test("escape the cluster name in the kubeconfig download", async () => {
    // An error response stops the download before it touches the DOM, which these tests do not have.
    const paths: string[] = [];
    const realFetch = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      paths.push(String(input));
      return new Response("not found", { status: 404 });
    }) as typeof fetch;
    try {
      await expect(downloadKubeconfig("default", arn)).rejects.toThrow("(404)");
    } finally {
      globalThis.fetch = realFetch;
    }

    expect(paths).toEqual([`${arnPath}/kubeconfig`]);
  });

  test("escape the cluster name in the logs stream and the exec socket", () => {
    expect(logsEventSourceURL("default", arn, "apps", "web-0", "")).toBe(
      `${arnPath}/logs?pod=web-0&follow=true&tail=1000&namespace=apps`,
    );

    const realWindow = (globalThis as { window?: unknown }).window;
    (globalThis as { window?: unknown }).window = { location: { protocol: "https:", host: "ksail.local" } };
    try {
      expect(execWebSocketURL("default", arn, "apps", "web-0", "")).toBe(
        `wss://ksail.local${arnPath}/exec?pod=web-0&namespace=apps`,
      );
    } finally {
      (globalThis as { window?: unknown }).window = realWindow;
    }
  });

  test("build every cluster path through clusterPath", () => {
    // A new call site that interpolates the name directly would reintroduce the 404; this fails it.
    const source = readFileSync(new URL("../src/api.ts", import.meta.url), "utf8");
    expect(source.match(/\/api\/v1\/clusters\/\$\{/g) ?? []).toHaveLength(1);
  });
});
