import type { Page, Route } from "@playwright/test";
import type { Cluster } from "../src/api.ts";

// Keep catalog routing shared while each test retains its cluster scenario.
export async function mockClusterCatalog(route: Route, url: URL, clusters: Cluster[]) {
  if (url.pathname === "/api/v1/meta") {
    await route.fulfill({ json: { distributions: [], providers: {}, components: [] } });
    return true;
  }
  if (url.pathname === "/api/v1/clusters") {
    await route.fulfill({ json: { items: clusters } });
    return true;
  }
  return false;
}

type MockApiConfig = {
  mode: "local" | "operator";
  capabilities: Record<string, boolean>;
};

// Keep shared API plumbing separate from each scenario's fixtures and mutable state.
export async function mockApi(page: Page, config: MockApiConfig, handle: (route: Route, url: URL) => Promise<boolean>) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/v1/config") {
      await route.fulfill({
        json: { readOnly: false, authEnabled: false, ...config },
      });
      return;
    }
    if (await handle(route, url)) return;
    if (url.pathname === "/api/v1/events") {
      await route.fulfill({ status: 204 });
      return;
    }
    await route.fulfill({
      status: 404,
      json: { error: `No mock route for ${url.pathname}` },
    });
  });
}
