import { expect, test, type Page } from "@playwright/test";
import type { Cluster, K8sObject } from "../src/api.ts";
import { detectDistribution, detectProvider, displayIdentity } from "../src/lib/clusterIdentity.ts";

type NodeShape = {
  osImage?: string;
  kubeletVersion?: string;
  providerID?: string;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
};

function node({ osImage = "", kubeletVersion = "v1.36.4", providerID, labels = {}, annotations = {} }: NodeShape): K8sObject {
  return {
    apiVersion: "v1",
    kind: "Node",
    metadata: { name: `node-${Math.random().toString(36).slice(2, 8)}`, labels, annotations },
    spec: providerID === undefined ? {} : { providerID },
    status: {
      nodeInfo: { osImage, kubeletVersion },
      conditions: [{ type: "Ready", status: "True" }],
    },
  };
}

const talosHetzner = (id: number) => node({ osImage: "Talos (v1.13.9)", providerID: `hcloud://${id}` });
const ubuntu = () => node({ osImage: "Ubuntu 24.04 LTS" });

test.describe("detectDistribution", () => {
  test("names each distribution from the fields its nodes carry", () => {
    expect(detectDistribution([talosHetzner(1), talosHetzner(2)])).toBe("Talos");
    expect(detectDistribution([node({ kubeletVersion: "v1.30.4+k3s1" })])).toBe("K3s");
    expect(detectDistribution([node({ kubeletVersion: "v1.29.3-eks-ae9a62a" })])).toBe("EKS");
    expect(detectDistribution([node({ kubeletVersion: "v1.29.4-gke.1043002" })])).toBe("GKE");
    expect(detectDistribution([node({ labels: { "kubernetes.azure.com/cluster": "rg" } })])).toBe("AKS");
    expect(detectDistribution([node({ kubeletVersion: "fake", annotations: { "kwok.x-k8s.io/node": "fake" } })])).toBe("KWOK");
    expect(detectDistribution([node({ osImage: "Debian GNU/Linux 12", providerID: "kind://docker/dev/dev-control-plane" })])).toBe(
      "Vanilla",
    );
  });

  test("says nothing when the nodes disagree or carry no signal", () => {
    expect(detectDistribution([])).toBeUndefined();
    expect(detectDistribution([ubuntu()])).toBeUndefined();
    expect(detectDistribution([talosHetzner(1), ubuntu()])).toBeUndefined();
    // A real node beside a simulated one is not a KWOK cluster.
    expect(detectDistribution([node({ annotations: { "kwok.x-k8s.io/node": "fake" } }), ubuntu()])).toBeUndefined();
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

test.describe("displayIdentity", () => {
  const withSpec = (spec?: { distribution?: string; provider?: string }): Cluster =>
    ({ metadata: { name: "c" }, spec: spec ? { cluster: spec } : undefined }) as Cluster;

  test("the spec wins over the nodes, and only node-supplied values are marked detected", () => {
    const shown = displayIdentity(withSpec({ distribution: "K3s", provider: "Docker" }), {
      distribution: "Talos",
      provider: "Hetzner",
    });
    expect(shown.distribution).toEqual({ value: "K3s", detected: false });
    expect(shown.provider).toEqual({ value: "Docker", detected: false });
  });

  test("the nodes fill fields the spec leaves empty", () => {
    const shown = displayIdentity(withSpec(), { distribution: "Talos", provider: "Hetzner" });
    expect(shown.distribution).toEqual({ value: "Talos", detected: true });
    expect(shown.provider).toEqual({ value: "Hetzner", detected: true });
  });

  test("nothing proven reads as an undetected dash", () => {
    const shown = displayIdentity(withSpec(), {});
    expect(shown.distribution).toEqual({ value: "—", detected: false });
    expect(shown.provider).toEqual({ value: "—", detected: false });
  });
});

// unmanaged builds a kubeconfig context ksail did not create: the backend sends it with no spec.
function unmanaged(name: string): Cluster {
  return {
    metadata: { name, namespace: "default", annotations: { "ksail.io/unmanaged": "true" } },
    status: {
      endpoint: `https://${name.replace(/[^a-z0-9]/g, "-")}.example.invalid:6443`,
      conditions: [
        {
          type: "Ready",
          status: "False",
          reason: "Unmanaged",
          message: "Cluster is present in the kubeconfig but not managed by ksail",
        },
      ],
    },
  } as Cluster;
}

const managedKind = {
  metadata: { name: "dev", namespace: "default" },
  spec: { cluster: { distribution: "Vanilla", provider: "Docker" } },
  status: { phase: "Ready" },
} as Cluster;

type MockOptions = {
  clusters: Cluster[];
  // nodesFor returns the Nodes a cluster serves, or "error" to fail its Node read.
  nodesFor: (cluster: string) => K8sObject[] | "error";
  delayMs?: number;
};

// mockDesktopApi serves the local (desktop) surface and records how many Node reads were in flight
// at once, so a test can prove the store bounds them.
async function mockDesktopApi(page: Page, { clusters, nodesFor, delayMs = 0 }: MockOptions) {
  const nodeReads = { total: 0, inFlight: 0, maxInFlight: 0 };

  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());

    if (url.pathname === "/api/v1/config") {
      await route.fulfill({
        json: { readOnly: false, authEnabled: false, mode: "local", capabilities: { workloadRead: true } },
      });
      return;
    }

    if (url.pathname === "/api/v1/meta") {
      // Vanilla/Docker come first in the create-form catalogue, as on the real desktop surface. No
      // surface may show them for a cluster whose nodes say otherwise.
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
      await route.fulfill({ json: { items: clusters } });
      return;
    }

    if (url.pathname.endsWith("/resources")) {
      const kind = url.searchParams.get("kind") ?? "";
      if (kind !== "Node") {
        await route.fulfill({ json: { items: [] } });
        return;
      }

      const name = decodeURIComponent(url.pathname.split("/").at(-2) ?? "");
      nodeReads.total += 1;
      nodeReads.inFlight += 1;
      nodeReads.maxInFlight = Math.max(nodeReads.maxInFlight, nodeReads.inFlight);
      if (delayMs > 0) {
        await new Promise((resolve) => setTimeout(resolve, delayMs));
      }
      nodeReads.inFlight -= 1;

      const nodes = nodesFor(name);
      await (nodes === "error"
        ? route.fulfill({ status: 502, json: { error: "cluster unreachable" } })
        : route.fulfill({ json: { items: nodes } }));
      return;
    }

    if (url.pathname === "/api/v1/events") {
      await route.fulfill({ status: 204 });
      return;
    }

    await route.fulfill({ status: 404, json: { error: `No mock route for ${url.pathname}` } });
  });

  return nodeReads;
}

async function openOverview(page: Page, name: string) {
  await page.goto("/");
  await page.getByText(name, { exact: true }).first().click();
}

const DETECTED = "Detected from the cluster's nodes";

test("an unmanaged Talos cluster on Hetzner is identified from its nodes on every surface", async ({ page }) => {
  await mockDesktopApi(page, {
    clusters: [unmanaged("oidc@prod")],
    nodesFor: () => [talosHetzner(142432663), talosHetzner(141915898)],
  });
  await openOverview(page, "oidc@prod");

  const main = page.locator("#main-content");
  await expect(main.getByText("Talos · Hetzner · namespace default", { exact: true })).toBeVisible();
  const spec = main.getByText("Spec", { exact: true }).locator("..");
  await expect(spec.getByTitle(DETECTED).filter({ hasText: "Talos" })).toBeVisible();
  await expect(spec.getByTitle(DETECTED).filter({ hasText: "Hetzner" })).toBeVisible();

  // The badge says why ksail tracks no phase, instead of "Unknown".
  await expect(main.getByText("Unmanaged", { exact: true }).first()).toBeVisible();
  await expect(main.getByText("Unknown", { exact: true })).toHaveCount(0);

  // The sidebar switcher shows the same detected identity, not a dash.
  const switcher = page.getByRole("button", { name: /oidc@prod/ }).first();
  await expect(switcher).toContainText("Talos · Hetzner");

  await expect(page.getByText("Vanilla", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Docker", { exact: true })).toHaveCount(0);
});

test("an unmanaged cluster with no conclusive node evidence is not guessed", async ({ page }) => {
  await mockDesktopApi(page, { clusters: [unmanaged("oidc@prod")], nodesFor: () => [ubuntu()] });
  await openOverview(page, "oidc@prod");

  await expect(page.getByText("— · — · namespace default", { exact: true })).toBeVisible();
  await expect(page.getByTitle(DETECTED)).toHaveCount(0);
  await expect(page.getByText("Vanilla", { exact: true })).toHaveCount(0);
});

test("the clusters table identifies unmanaged clusters, keeps configured ones, and bounds its reads", async ({ page }) => {
  const talos = Array.from({ length: 6 }, (_, index) => unmanaged(`talos-${index}`));
  const clusters = [managedKind, ...talos, unmanaged("unreachable")];
  const reads = await mockDesktopApi(page, {
    clusters,
    delayMs: 300,
    nodesFor: (name) => (name === "unreachable" ? "error" : name === "dev" ? [] : [talosHetzner(1)]),
  });
  await page.goto("/");

  // Each table row is a button labelled "View <name>".
  const row = (name: string) => page.getByRole("button", { name: `View ${name}`, exact: true });

  for (const cluster of talos) {
    await expect(row(cluster.metadata.name).getByTitle(DETECTED).filter({ hasText: "Talos" })).toBeVisible();
    await expect(row(cluster.metadata.name).getByTitle(DETECTED).filter({ hasText: "Hetzner" })).toBeVisible();
  }

  // A configured cluster shows its spec, unmarked, and is never probed.
  await expect(row("dev").getByText("Vanilla", { exact: true })).toBeVisible();
  await expect(row("dev").getByTitle(DETECTED)).toHaveCount(0);

  // An unreachable cluster reads as unknown, not as an error or a guess.
  const unreachable = row("unreachable").getByRole("cell");
  await expect(unreachable.nth(2)).toHaveText("—");
  await expect(unreachable.nth(3)).toHaveText("—");
  await expect(row("unreachable").getByTitle(DETECTED)).toHaveCount(0);

  // One read per unmanaged cluster, never more than four at once, none for the managed one.
  expect(reads.total).toBe(talos.length + 1);
  expect(reads.maxInFlight).toBeLessThanOrEqual(4);
  expect(reads.maxInFlight).toBeGreaterThan(1);
});
