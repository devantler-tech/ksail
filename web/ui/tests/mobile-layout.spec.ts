import { expect, test, type Page } from "@playwright/test";
import { mockApi, mockClusterCatalog } from "./mock-api.ts";

const CLUSTER_NAME = "production-observability-control-plane-with-a-very-long-generated-cluster-name";
const NAMESPACE = "n".repeat(63);
const POD_NAME = "metrics-exporter-with-an-intentionally-long-unbroken-generated-pod-name-7f9c8d6b5f-qwert";
const PLUGIN_ACTION_LABELS = [
  "PluginActionWithoutAnyNaturalBreakOpportunity".repeat(3),
  "SecondPluginActionWithoutAnyNaturalBreakOpportunity".repeat(3),
];
const RESOURCE_READY_REASON = "ControllerReportedAnExceptionallyLongUnbrokenReadinessFailureReason".repeat(2);
const RESOURCE_STATUS_LABEL = `Not Ready: ${RESOURCE_READY_REASON}`;

const cluster = {
  metadata: {
    name: CLUSTER_NAME,
    namespace: NAMESPACE,
    creationTimestamp: "2026-08-01T12:00:00Z",
  },
  spec: {
    cluster: {
      distribution: "VCluster",
      provider: "Kubernetes",
      workers: 12,
      controlPlanes: 3,
    },
  },
  status: {
    phase: "Provisioning",
    endpoint: `https://${CLUSTER_NAME}.example.invalid:6443`,
    nodesReady: 15,
    nodesTotal: 15,
    lastReconcileTime: "2026-08-29T16:00:00Z",
    conditions: [
      {
        type: "InfrastructureAndWorkloadsRemainHealthyAcrossEveryAvailabilityZone",
        status: "True",
        reason: "AllInfrastructureComponentsSuccessfullyReconciled",
        message:
          "Every infrastructure component and workload is healthy across all availability zones with no pending remediation.",
        lastTransitionTime: "2026-08-29T16:00:00Z",
      },
    ],
  },
};

const node = {
  apiVersion: "v1",
  kind: "Node",
  metadata: {
    name: "worker-node-with-a-very-long-cloud-provider-generated-identifier-0123456789",
    creationTimestamp: "2026-08-01T12:00:00Z",
    labels: { "node-role.kubernetes.io/control-plane": "" },
  },
  status: {
    capacity: { cpu: "8", memory: "16Gi", pods: "110" },
    allocatable: { cpu: "7500m", memory: "15Gi", pods: "110" },
    nodeInfo: { kubeletVersion: "v1.37.0", osImage: "Talos Linux v1.12.0" },
    conditions: [
      {
        type: "Ready",
        status: "True",
        lastTransitionTime: "2026-08-29T16:00:00Z",
      },
    ],
  },
};

const pod = {
  apiVersion: "v1",
  kind: "Pod",
  metadata: {
    name: POD_NAME,
    namespace: "observability-system-with-a-long-namespace",
    creationTimestamp: "2026-08-29T12:00:00Z",
  },
  spec: {
    containers: [
      {
        name: "metrics-exporter",
        resources: { requests: { cpu: "250m", memory: "256Mi" } },
      },
    ],
  },
  status: {
    phase: "Running",
    conditions: [{ type: "Ready", status: "False", reason: RESOURCE_READY_REASON }],
    containerStatuses: [{ name: "metrics-exporter", ready: true }],
  },
};

const event = {
  apiVersion: "v1",
  kind: "Event",
  metadata: {
    name: "warning-with-a-long-generated-event-name",
    namespace: NAMESPACE,
    creationTimestamp: "2026-08-29T15:55:00Z",
  },
  type: "Warning",
  reason: "BackOffBecauseAContainerWithAnExceptionallyLongNameCouldNotStart",
  message:
    "The workload reported an intentionally long diagnostic message without natural break opportunities: abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789.",
  involvedObject: {
    kind: "Pod",
    name: POD_NAME,
    namespace: pod.metadata.namespace,
  },
  count: 12345,
  lastTimestamp: "2026-08-29T15:55:00Z",
};

function resources(kind: string) {
  if (kind === "Node") return [node];
  if (kind === "Pod") return [pod];
  if (kind === "Event") return [event];
  if (kind === "NodeMetrics") {
    return [
      {
        metadata: { name: node.metadata.name },
        usage: { cpu: "3200m", memory: "8Gi" },
      },
    ];
  }
  if (kind === "PodMetrics") {
    return [
      {
        metadata: { name: POD_NAME, namespace: pod.metadata.namespace },
        containers: [{ name: "metrics-exporter", usage: { cpu: "950m", memory: "900Mi" } }],
      },
    ];
  }
  return [];
}

async function mockOperatorApi(page: Page) {
  await mockApi(
    page,
    {
      mode: "operator",
      capabilities: {
        clusterUpdate: true,
        workloadRead: true,
        workloadWrite: true,
        kubeconfigDownload: true,
        applyManifests: true,
        secretsCipher: false,
        workloadLogs: true,
        workloadExec: true,
        clusterStartStop: false,
        componentsInstall: true,
        plugins: true,
        aiChat: false,
        kubeProxy: false,
        pluginInstall: false,
        aiChatWrite: false,
        pluginCatalog: false,
        kubeWatch: false,
        wsMultiplexer: false,
      },
    },
    async (route, url) => {
      if (url.pathname === "/api/v1/plugins") {
        await route.fulfill({
          json: { plugins: [{ name: "wide-action", main: "main.js" }] },
        });
        return true;
      }

      if (url.pathname === "/api/v1/plugins/wide-action/main.js") {
        await route.fulfill({
          contentType: "application/javascript",
          body: PLUGIN_ACTION_LABELS.map(
            (label) =>
              `window.pluginLib.registerAppBarAction(window.pluginLib.React.createElement("button", { type: "button", "aria-label": "${label}" }, "${label}"));`,
          ).join("\n"),
        });
        return true;
      }

      if (url.pathname === "/api/v1/meta") {
        await route.fulfill({
          json: {
            distributions: ["VCluster"],
            providers: { VCluster: ["Kubernetes"] },
            components: [],
            resourceKinds: ["Pod", "Deployment", "StatefulSet", "DaemonSet", "Event", "Node", "Namespace"].map(
              (kind) => ({
                kind,
                namespaced: !["Node", "Namespace"].includes(kind),
                scalable: ["Deployment", "StatefulSet"].includes(kind),
                restartable: ["Deployment", "StatefulSet", "DaemonSet"].includes(kind),
                reconcilable: false,
                deletable: !["Node", "Namespace"].includes(kind),
                browsable: true,
              }),
            ),
          },
        });
        return true;
      }

      if (await mockClusterCatalog(route, url, [cluster])) return true;

      if (url.pathname.endsWith("/resources")) {
        await route.fulfill({
          json: { items: resources(url.searchParams.get("kind") ?? "") },
        });
        return true;
      }
      return false;
    },
  );
}

async function expectPageAndTableToFit(page: Page) {
  await expect(page.locator("table")).toBeVisible();
  await expectPageAndElementToFit(page, "table", true);
}

// Measure document and content overflow through the same browser read for each view.
async function expectPageAndElementToFit(page: Page, selector: string, useParent = false) {
  const widths = await page.evaluate(
    ({ selector, useParent }) => {
      const selected = document.querySelector<HTMLElement>(selector);
      const element = useParent ? selected?.parentElement : selected;
      if (!element) throw new Error(`Missing layout measurement element: ${selector}`);
      return {
        documentClientWidth: document.documentElement.clientWidth,
        documentScrollWidth: document.documentElement.scrollWidth,
        elementClientWidth: element.clientWidth,
        elementScrollWidth: element.scrollWidth,
      };
    },
    { selector, useParent },
  );

  expect(widths.documentScrollWidth).toBe(widths.documentClientWidth);
  expect(widths.elementScrollWidth).toBe(widths.elementClientWidth);
}

async function navigateFromDrawer(page: Page, label: "Resources" | "Events") {
  await page.getByRole("button", { name: "Open navigation" }).click();
  await page.getByRole("dialog").getByRole("button", { name: label, exact: true }).click();
}

test.use({ viewport: { width: 320, height: 800 } });

test("operator views remain usable without horizontal overflow on a phone", async ({ page }) => {
  await mockOperatorApi(page);
  // Layout checks begin after the fixture bundle loads, including lazy externals.
  const pluginBundle = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/v1/plugins/wide-action/main.js",
  );
  await page.goto("/");
  await (await pluginBundle).finished();

  const pageTitle = page.getByRole("heading", { name: "Clusters", level: 1 });
  await expect(pageTitle).toBeVisible();
  await expect(pageTitle).toBeInViewport({ ratio: 1 });
  const pluginActions = page.getByRole("button", { name: "Plugin actions" });
  await expect(pluginActions).toBeInViewport({ ratio: 1 });
  await pluginActions.click();
  for (const label of PLUGIN_ACTION_LABELS) {
    const action = page.getByRole("button", { name: label });
    await expect(action).toBeVisible();
    await expect(action).toBeInViewport({ ratio: 1 });
  }
  await pluginActions.click();
  await expect(page.getByRole("button", { name: "Refresh", exact: true })).toBeInViewport({ ratio: 1 });
  await expect(page.getByRole("button", { name: "New cluster", exact: true })).toBeInViewport({ ratio: 1 });
  await expectPageAndTableToFit(page);

  await expect(page.getByRole("columnheader", { name: "Name" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Status" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Namespace" })).toBeHidden();
  const clusterStatusCell = page.getByRole("cell", { name: "Provisioning" });
  expect(await clusterStatusCell.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await page.getByText(CLUSTER_NAME, { exact: true }).click();

  const specCard = page.getByText("Spec", { exact: true }).locator("..");
  const statusCard = page.getByText("Status", { exact: true }).locator("..");
  const conditionsCard = page.getByText("Conditions", { exact: true }).locator("..");
  await specCard.scrollIntoViewIfNeeded();
  await expect(specCard).toBeInViewport({ ratio: 1 });
  await statusCard.scrollIntoViewIfNeeded();
  await expect(statusCard).toBeInViewport({ ratio: 1 });
  await conditionsCard.scrollIntoViewIfNeeded();
  await expect(conditionsCard).toBeInViewport({ ratio: 1 });

  await expectPageAndElementToFit(page, "#main-content");

  await navigateFromDrawer(page, "Resources");
  await expect(page.getByText(POD_NAME, { exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Name" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Status" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Age" })).toBeHidden();
  const resourceStatusCell = page.getByRole("cell", {
    name: RESOURCE_STATUS_LABEL,
  });
  expect(await resourceStatusCell.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await expectPageAndTableToFit(page);

  await navigateFromDrawer(page, "Events");
  const eventReason = page.getByText(event.reason, { exact: true });
  await expect(eventReason).toBeVisible();
  expect(
    await eventReason.evaluate(
      (element) => element.scrollWidth <= element.clientWidth && element.scrollHeight <= element.clientHeight,
    ),
  ).toBe(true);
  await expect(page.getByText(event.message, { exact: true }).filter({ visible: true })).toBeVisible();
  await expectPageAndTableToFit(page);
});
