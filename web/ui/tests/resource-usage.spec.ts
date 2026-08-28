import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 320, height: 800 } });

test("top pod consumers stay inside a mobile viewport", async ({ page }) => {
  await page.goto("/tests/fixtures/resource-usage.html");

  const cpuList = page.getByText("Top pods by cpu", { exact: true });
  const memoryList = page.getByText("Top pods by memory", { exact: true });

  await expect(cpuList).toBeVisible();
  await expect(memoryList).toBeVisible();

  // The headings alone do not prove the panel is usable: a layout regression can
  // clip or collapse the metric column while both headings stay visible. These
  // pin the fixture pod's rendered values -- formatCores(0.95) and
  // formatBytes(900 MiB) -- inside their own list, so losing a value fails here.
  await expect(cpuList.locator("..").getByText("950m", { exact: true })).toBeVisible();
  await expect(memoryList.locator("..").getByText("900 MiB", { exact: true })).toBeVisible();

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
