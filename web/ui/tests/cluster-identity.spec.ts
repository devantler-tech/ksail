import { expect, test, type Page } from "@playwright/test";
import type { K8sObject } from "../src/api.ts";
import { detectDistribution, detectProvider } from "../src/lib/clusterIdentity.ts";

type NodeShape = { osImage?: string; kubeletVersion?: string; providerID?: string; labels?: Record<string, string> };

function node({ osImage = "", kubeletVersion = "v1.36.4", providerID, labels = {} }: NodeShape): K8sObject {
  return {
    apiVersion: "v1",
    kind: "Node",
    metadata: { name: `node-${Math.random().toString(36).slice(2, 8)}`, labels },
    spec: providerID === undefined ? {} : { providerID },
    status: {
      nodeInfo: { osImage, kubeletVersion },
      conditions: [{ type: "Ready", status: "True" }],
    },
  };
}

const talosHetzner = (id: number) => node({ osImage: "Talos (v1.13.9)", providerID: `hcloud://${id}` });

test.describe("detectDistribution", () => {
  test("names each distribution from the fields its nodes carry", () => {
    expect(detectDistribution([talosHetzner(1), talosHetzner(2)])).toBe("Talos");
    expect(detectDistribution([node({ kubeletVersion: "v1.30.4+k3s1" })])).toBe("K3s");
    expect(detectDistribution([node({ kubeletVersion: "v1.29.3-eks-ae9a62a" })])).toBe("EKS");
    expect(detectDistribution([node({ kubeletVersion: "v1.29.4-gke.1043002" })])).toBe("GKE");
    expect(detectDistribution([node({ labels: { "kubernetes.azure.com/cluster": "rg" } })])).toBe("AKS");
    expect(detectDistribution([node({ osImage: "Debian GNU/Linux 12", providerID: "kind://docker/dev/dev-control-plane" })])).toBe(
      "Vanilla",
    );
  });

  test("says nothing when the nodes disagree or carry no signal", () => {
    expect(detectDistribution([])).toBeUndefined();
    expect(detectDistribution([node({ osImage: "Ubuntu 24.04 LTS" })])).toBeUndefined();
    expect(detectDistribution([talosHetzner(1), node({ osImage: "Ubuntu 24.04 LTS" })])).toBeUndefined();
  });
});

test.describe("detectProvider", () => {
  test("maps the providerID scheme to the provider", () => {
    expect(detectProvider([talosHetzner(1), talosHetzner(2)])).toBe("Hetzner");
    expect(detectProvider([node({ providerID: "aws:///eu-west-1a/i-0abc" })])).toBe("AWS");
    expect(detectProvider([node({ providerID: "gce://project/zone/vm" })])).toBe("GCP");
    expect(detectProvider([node({ providerID: "azure:///subscriptions/x" })])).toBe("Azure");
    expect(detectProvider([node({ providerID: "kind://docker/dev/dev-control-plane" })])).toBe("Docker");
  });

  test("ignores a node the cloud controller has not initialised yet", () => {
    expect(detectProvider([talosHetzner(1), node({ osImage: "Talos (v1.13.9)" })])).toBe("Hetzner");
  });

  test("says nothing when providers disagree, are unknown, or absent", () => {
    expect(detectProvider([])).toBeUndefined();
    expect(detectProvider([node({})])).toBeUndefined();
    expect(detectProvider([node({ providerID: "openstack:///abc" })])).toBeUndefined();
    expect(detectProvider([talosHetzner(1), node({ providerID: "aws:///eu-west-1a/i-0abc" })])).toBeUndefined();
  });
});

// An unmanaged cluster: a kubeconfig context ksail did not create, so the backend sends no spec.
const unmanagedCluster = {
  metadata: { name: "oidc@prod", namespace: "default", annotations: { "ksail.io/unmanaged": "true" } },
  status: {
    endpoint: "https://prod.example.invalid:6443",
    conditions: [
      {
        type: "Ready",
        status: "False",
        reason: "Unmanaged",
        message: "Cluster is present in the kubeconfig but not managed by ksail",
      },
    ],
  },
};

async function mockDesktopApi(page: Page, nodes: K8sObject[]) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());

    if (url.pathname === "/api/v1/config") {
      await route.fulfill({
        json: { readOnly: false, authEnabled: false, mode: "local", capabilities: { workloadRead: true } },
      });
      return;
    }

    if (url.pathname === "/api/v1/meta") {
      // Vanilla/Docker come first in the create-form catalogue, as on the real desktop surface. The
      // Overview must never show them for a cluster whose nodes say otherwise.
      await route.fulfill({
        json: {
          distributions: ["Vanilla", "Talos"],
          providers: { Vanilla: ["Docker"], Talos: ["Docker", "Hetzner"] },
          components: [],
          resourceKinds: ["Node", "Pod", "Event", "Namespace"].map((kind) => ({
            kind,
            namespaced: !["Node", "Namespace"].includes(kind),
            scalable: false,
            restartable: false,
            reconcilable: false,
            deletable: false,
            browsable: true,
          })),
        },
      });
      return;
    }

    if (url.pathname === "/api/v1/clusters") {
      await route.fulfill({ json: { items: [unmanagedCluster] } });
      return;
    }

    if (url.pathname.endsWith("/resources")) {
      const kind = url.searchParams.get("kind") ?? "";
      await route.fulfill({ json: { items: kind === "Node" ? nodes : [] } });
      return;
    }

    if (url.pathname === "/api/v1/events") {
      await route.fulfill({ status: 204 });
      return;
    }

    await route.fulfill({ status: 404, json: { error: `No mock route for ${url.pathname}` } });
  });
}

async function openOverview(page: Page) {
  await page.goto("/");
  await page.getByText("oidc@prod", { exact: true }).first().click();
}

test("an unmanaged Talos cluster on Hetzner is labelled from its nodes", async ({ page }) => {
  await mockDesktopApi(page, [talosHetzner(142432663), talosHetzner(141915898)]);
  await openOverview(page);

  await expect(page.getByText("Talos · Hetzner · namespace default", { exact: true })).toBeVisible();
  const spec = page.getByText("Spec", { exact: true }).locator("..");
  await expect(spec.getByText("Talos", { exact: true })).toBeVisible();
  await expect(spec.getByText("Hetzner", { exact: true })).toBeVisible();
  await expect(page.getByText("Vanilla", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Docker", { exact: true })).toHaveCount(0);
});

test("an unmanaged cluster with no conclusive node evidence is not guessed", async ({ page }) => {
  await mockDesktopApi(page, [node({ osImage: "Ubuntu 24.04 LTS" })]);
  await openOverview(page);

  await expect(page.getByText("— · — · namespace default", { exact: true })).toBeVisible();
  await expect(page.getByText("Vanilla", { exact: true })).toHaveCount(0);
});
