import { expect, test, type APIRequestContext } from "@playwright/test";
import { mockApi, mockClusterCatalog } from "./mock-api.ts";

type ManifestIcon = { src: string; sizes: string; type: string; purpose?: string };

// A PNG stores its width and height as big-endian 32-bit integers at bytes 16-23 (the IHDR chunk).
function pngSize(body: Buffer): string {
  expect(body.subarray(0, 8).toString("hex"), "PNG signature").toBe("89504e470d0a1a0a");
  return `${body.readUInt32BE(16)}x${body.readUInt32BE(20)}`;
}

async function expectImage(request: APIRequestContext, url: URL, type: string, sizes?: string) {
  const response = await request.get(url.href);
  expect(response.status(), url.pathname).toBe(200);
  expect(response.headers()["content-type"], url.pathname).toContain(type);
  if (sizes !== undefined) {
    expect(pngSize(await response.body()), url.pathname).toBe(sizes);
  }
}

test("the UI links a manifest and icons that install it as an app", async ({ page, request }) => {
  await mockApi(page, { mode: "local", capabilities: {} }, (route, url) => mockClusterCatalog(route, url, []));
  // index.html is also served for nested client-side routes, so the links must resolve from one.
  await page.goto("/clusters/example");

  const linked = async (selector: string) => {
    const href = await page.locator(selector).getAttribute("href");
    expect(href, selector).not.toBeNull();
    return new URL(href!, page.url());
  };

  const manifestURL = await linked('link[rel="manifest"]');
  const response = await request.get(manifestURL.href);
  expect(response.status()).toBe(200);
  const manifest = await response.json();
  expect(manifest).toMatchObject({ name: "KSail", short_name: "KSail", display: "standalone" });
  // Relative start_url and scope keep the installed app on the origin that served the manifest.
  expect(new URL(manifest.start_url, manifestURL).pathname).toBe("/");
  expect(new URL(manifest.scope, manifestURL).pathname).toBe("/");

  const icons: ManifestIcon[] = manifest.icons;
  expect(icons.map((icon) => icon.purpose)).toContain("maskable");
  for (const icon of icons) {
    await expectImage(request, new URL(icon.src, manifestURL), icon.type, icon.sizes);
  }

  await expectImage(request, await linked('link[rel="apple-touch-icon"]'), "image/png", "180x180");
  await expectImage(request, await linked('link[rel="icon"]'), "image/svg+xml");
});
