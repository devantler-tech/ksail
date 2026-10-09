import { expect, test, type Page } from "@playwright/test";
import { mockApi, mockClusterCatalog } from "./mock-api.ts";

async function openExpiredContext(page: Page, options: { enabled?: boolean; loginFails?: boolean } = {}) {
  let expired = true;
  const logins: { path: string; body: string | null }[] = [];
  await mockApi(page, { mode: "local", capabilities: { workloadRead: true } }, async (route, url) => {
    if (url.pathname === "/api/v1/config") return false;
    if (await mockClusterCatalog(route, url, [{
      metadata: { name: "selected", namespace: "default", annotations: { "ksail.io/unmanaged": "true" } },
    }])) return true;
    if (url.pathname.endsWith("/authentication/renew")) {
      logins.push({ path: url.pathname, body: route.request().postData() });
      if (options.loginFails) {
        await route.fulfill({ status: 502, json: { error: "AWS SSO sign-in failed" } });
      } else {
        expired = false;
        await route.fulfill({ status: 204 });
      }
      return true;
    }
    if (url.pathname.endsWith("/authentication")) {
      await route.fulfill({ json: { enabled: options.enabled ?? true, supported: true, required: expired } });
      return true;
    }
    if (url.pathname.endsWith("/resources")) {
      if (expired) {
        await route.fulfill({ status: 502, json: { error: "AWS SSO session expired" } });
      } else {
        await route.fulfill({ json: { items: url.searchParams.get("kind") === "Node" ? [{
          metadata: { name: "worker" }, status: { conditions: [{ type: "Ready", status: "True" }] },
        }] : [] } });
      }
      return true;
    }
    return false;
  });
  // Override config alone so the real app exposes the local authentication surface.
  await page.route("**/api/v1/config", (route) => route.fulfill({
    json: { mode: "local", readOnly: false, authEnabled: false, settingsEnabled: true,
      capabilities: { workloadRead: true } },
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "View selected", exact: true }).click();
  return { main: page.locator("#main-content"), logins };
}

test("desktop sign-in is explicit, context-scoped, and refreshes monitoring after recovery", async ({ page }) => {
  const { main, logins } = await openExpiredContext(page);
  const signIn = main.getByRole("button", { name: "Sign in to AWS SSO", exact: true });
  await expect(signIn).toBeVisible();
  expect(logins).toHaveLength(0);
  await signIn.click();
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).toContainText("1/1");
  await expect(signIn).toHaveCount(0);
  expect(logins).toEqual([{ path: "/api/v1/clusters/default/selected/authentication/renew", body: "{}" }]);
});

test("disabled renewal never offers sign-in or posts a login", async ({ page }) => {
  const { main, logins } = await openExpiredContext(page, { enabled: false });
  await expect(main.getByRole("alert")).toContainText("AWS SSO session expired");
  await expect(main.getByRole("button", { name: "Sign in to AWS SSO", exact: true })).toHaveCount(0);
  expect(logins).toHaveLength(0);
});

test("failed provider sign-in stays an explicit error and does not claim recovered monitoring", async ({ page }) => {
  const { main, logins } = await openExpiredContext(page, { loginFails: true });
  await main.getByRole("button", { name: "Sign in to AWS SSO", exact: true }).click();
  await expect(main.getByRole("alert").filter({ hasText: "AWS SSO sign-in failed" })).toBeVisible();
  await expect(main.getByText("Nodes", { exact: true }).first().locator("..")).not.toContainText("1/1");
  expect(logins).toHaveLength(1);
});
