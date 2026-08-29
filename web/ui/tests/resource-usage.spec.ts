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
  const cpuValue = cpuList.locator("..").getByText("950m", { exact: true });
  const memoryValue = memoryList.locator("..").getByText("900 MiB", { exact: true });

  await expect(cpuValue).toBeVisible();
  await expect(memoryValue).toBeVisible();

  // toBeVisible() only proves a non-empty box: a value clipped by an
  // overflow:hidden ancestor, or pushed past the 320px edge, still passes it
  // -- and the document-level scrollWidth checks below stay green because a
  // clipping ancestor never extends the document. Requiring the values to sit
  // ENTIRELY inside the viewport is the property this test actually claims.
  await expect(cpuValue).toBeInViewport({ ratio: 1 });
  await expect(memoryValue).toBeInViewport({ ratio: 1 });

  const layout = await page.evaluate(() => {
    const fixture = document.querySelector<HTMLElement>("[data-testid='resource-usage-fixture']")!;
    const podName = document.querySelector<HTMLElement>("[title^='observability-system/']")!;
    const podNameStyle = getComputedStyle(podName);

    return {
      documentClientWidth: document.documentElement.clientWidth,
      documentScrollWidth: document.documentElement.scrollWidth,
      fixtureClientWidth: fixture.clientWidth,
      fixtureScrollWidth: fixture.scrollWidth,
      podNameClientWidth: podName.clientWidth,
      podNameScrollWidth: podName.scrollWidth,
      podNameOverflowX: podNameStyle.overflowX,
      podNameTextOverflow: podNameStyle.textOverflow,
      podNameWhiteSpace: podNameStyle.whiteSpace,
    };
  });

  expect(layout.documentScrollWidth).toBe(layout.documentClientWidth);
  expect(layout.fixtureScrollWidth).toBe(layout.fixtureClientWidth);
  expect(layout.podNameScrollWidth).toBeGreaterThan(layout.podNameClientWidth);

  // scrollWidth > clientWidth only proves the name is wider than its box -- it is
  // equally true of a box that clips the overflow with no affordance at all. The
  // three declarations below are what Tailwind's `truncate` actually applies, and
  // together they are the property this test claims: the name is kept on one line,
  // clipped to the box, and marked as continuing with an ellipsis. Dropping any one
  // of them is a user-visible regression that the width assertion alone cannot see.
  expect(layout.podNameWhiteSpace).toBe("nowrap");
  expect(layout.podNameOverflowX).toBe("hidden");
  expect(layout.podNameTextOverflow).toBe("ellipsis");
});
