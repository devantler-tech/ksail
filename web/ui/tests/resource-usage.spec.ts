import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 320, height: 800 } });

test("top pod consumers stay inside a mobile viewport", async ({ page }) => {
  await page.goto("/tests/fixtures/resource-usage.html");

  await expect(page.getByText("Top pods by cpu", { exact: true })).toBeVisible();
  await expect(page.getByText("Top pods by memory", { exact: true })).toBeVisible();

  const layout = await page.evaluate(() => {
    const fixture = document.querySelector<HTMLElement>("[data-testid='resource-usage-fixture']")!;
    const podName = document.querySelector<HTMLElement>("[title^='observability-system/']")!;

    return {
      documentClientWidth: document.documentElement.clientWidth,
      documentScrollWidth: document.documentElement.scrollWidth,
      fixtureClientWidth: fixture.clientWidth,
      fixtureScrollWidth: fixture.scrollWidth,
      podNameClientWidth: podName.clientWidth,
      podNameScrollWidth: podName.scrollWidth,
    };
  });

  expect(layout.documentScrollWidth).toBe(layout.documentClientWidth);
  expect(layout.fixtureScrollWidth).toBe(layout.fixtureClientWidth);
  expect(layout.podNameScrollWidth).toBeGreaterThan(layout.podNameClientWidth);
});
