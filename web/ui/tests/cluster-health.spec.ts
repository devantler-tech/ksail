import { expect, test, type Page } from "@playwright/test";
import type { Cluster, K8sObject } from "../src/api.ts";
import { mockApi } from "./mock-api.ts";

const cluster: Cluster = {
  metadata: { name: "external-demo", namespace: "default", annotations: { "ksail.io/unmanaged": "true" } },
  status: {
    conditions: [{ type: "Ready", status: "False", reason: "Unmanaged", message: "Lifecycle is not managed by KSail." }],
  },
};

const node: K8sObject = {
  metadata: { name: "worker" },
  status: {
    allocatable: { cpu: "4", memory: "8Gi", pods: "110" },
    conditions: [{ type: "Ready", status: "True" }],
  },
};

const pod: K8sObject = {
  metadata: { name: "demo", namespace: "default" },
  spec: {
    nodeName: "worker",
    containers: [{ name: "app", resources: { requests: { cpu: "500m", memory: "256Mi" } } }],
  },
  status: { phase: "Running", containerStatuses: [{ name: "app", ready: true }] },
};

type ResourceResponse = { items: K8sObject[] } | { status: number; error: string };

async function openHealth(page: Page, responseFor: (kind: string) => ResourceResponse) {
  await mockApi(page, { mode: "local", capabilities: { workloadRead: true } }, async (route, url) => {
    if (url.pathname === "/api/v1/meta") {
      await route.fulfill({ json: { distributions: [], providers: {}, components: [] } });
      return true;
    }
    if (url.pathname === "/api/v1/clusters") {
      await route.fulfill({ json: { items: [cluster] } });
      return true;
    }
    if (url.pathname.endsWith("/resources")) {
      const response = responseFor(url.searchParams.get("kind") ?? "");
      await ("status" in response
        ? route.fulfill({ status: response.status, json: { error: response.error } })
        : route.fulfill({ json: response }));
      return true;
    }
    return false;
  });

  await page.goto("/");
  await page.getByRole("button", { name: "View external-demo", exact: true }).click();
  const main = page.locator("#main-content");
  await expect(main.getByRole("button", { name: "Refresh", exact: true })).toBeEnabled();
  return main;
}

function readable(kind: string): ResourceResponse {
  if (kind === "Node") return { items: [node] };
  if (kind === "Pod") return { items: [pod] };
  return { items: [] };
}

test("credential failures are visible rather than reported as an empty healthy cluster", async ({ page }) => {
  const main = await openHealth(page, () => ({ status: 502, error: "credential command failed: session expired" }));

  await expect(main.getByRole("alert")).toContainText("session expired");
  await expect(main.getByRole("alert")).toContainText("Refresh");
  await expect(main.getByText("Pod health", { exact: true }).locator("..")).toContainText("Pod health is unavailable.");
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).not.toContainText("0/0");
  await expect(main).not.toContainText("No pods.");
  await expect(main).not.toContainText("No recent warnings.");
  await expect(main).not.toContainText("No nodes reported.");
  await expect(main).toContainText("Recent warnings are unavailable.");
  await expect(main).toContainText("Resource usage is unavailable.");
});

test("a forbidden node read preserves readable pods without inventing node capacity", async ({ page }) => {
  const main = await openHealth(page, (kind) =>
    kind === "Node" ? { status: 403, error: "nodes are forbidden" } : readable(kind),
  );

  await expect(main.getByRole("alert")).toContainText("nodes are forbidden");
  await expect(main.getByText("Pod health", { exact: true }).locator("..")).toContainText("Running");
  await expect(main.getByText("Pod health", { exact: true }).locator("..")).toContainText("1");
  await expect(main.getByText("Workloads", { exact: true }).locator("..")).toContainText("Pods1");
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).not.toContainText("0/0");
  await expect(main).toContainText("Resource usage is unavailable.");
});

test("a failed pod read cannot fabricate resource requests or a zero pod count", async ({ page }) => {
  const main = await openHealth(page, (kind) =>
    kind === "Pod" ? { status: 403, error: "pods are forbidden" } : readable(kind),
  );

  await expect(main.getByRole("alert")).toContainText("pods are forbidden");
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).toContainText("1/1");
  await expect(main.getByText("Pod health", { exact: true }).locator("..")).toContainText("Pod health is unavailable.");
  await expect(main.getByText("Workloads", { exact: true }).locator("..")).toContainText("Pods—");
  await expect(main).toContainText("Resource usage is unavailable.");
});

test("a failed workload read is unknown while successful empty reads remain zero", async ({ page }) => {
  const main = await openHealth(page, (kind) =>
    kind === "Deployment" ? { status: 403, error: "deployments are forbidden" } : readable(kind),
  );
  const workloads = main.getByText("Workloads", { exact: true }).locator("..");

  await expect(workloads).toContainText("Deployments—");
  await expect(workloads).toContainText("StatefulSets0");
  await expect(workloads).toContainText("DaemonSets0");
  await expect(workloads).toContainText("Pods1");
});

test("missing metrics preserve genuine request estimates and surface the failed reads", async ({ page }) => {
  const main = await openHealth(page, (kind) =>
    kind.endsWith("Metrics") ? { status: 404, error: "metrics API is unavailable" } : readable(kind),
  );

  await expect(main.getByRole("alert")).toContainText("NodeMetrics");
  await expect(main.getByRole("alert")).toContainText("PodMetrics");
  await expect(main.getByText("Resource usage", { exact: true }).locator("..")).toContainText("500m requested (13%)");
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).toContainText("1/1");
});

test("refresh after credential recovery clears errors and restores real counts", async ({ page }) => {
  let expired = true;
  const main = await openHealth(page, (kind) =>
    expired ? { status: 502, error: "credential command failed: session expired" } : readable(kind),
  );
  await expect(main.getByRole("alert")).toContainText("session expired");

  expired = false;
  await main.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(main.getByRole("alert")).toHaveCount(0);
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).toContainText("1/1");
  await expect(main.getByText("Workloads", { exact: true }).locator("..")).toContainText("Pods1");
  await expect(main).toContainText("No recent warnings.");
});

test("successful empty responses remain legitimate empty health data", async ({ page }) => {
  const main = await openHealth(page, () => ({ items: [] }));

  await expect(main.getByRole("alert")).toHaveCount(0);
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).toContainText("0/0");
  await expect(main).toContainText("No pods.");
  await expect(main).toContainText("No recent warnings.");
});

test("long connection errors and their details remain readable on a phone", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const main = await openHealth(page, () => ({
    status: 502,
    error: `ConnectionFailedWithoutAnyNaturalBreakOpportunity${"x".repeat(800)}`,
  }));
  await expect(main.getByRole("alert")).toBeVisible();
  await main.getByText("Failed resource reads", { exact: true }).click();
  const widths = await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(widths.document).toBeLessThanOrEqual(widths.viewport);
  expect(widths.body).toBeLessThanOrEqual(widths.viewport);
});
