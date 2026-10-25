// Renders the web app icons in public/ from their SVG sources, so the committed PNGs stay
// reproducible. Run `npm run gen:icons` after changing public/favicon.svg or icons/maskable.svg.
// It drives Playwright's Chromium (`npx playwright install chromium`); a normal build does not run
// it, because the PNGs are committed.
import { chromium } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

const icons = [
  { source: "public/favicon.svg", target: "public/icon-192.png", size: 192 },
  { source: "public/favicon.svg", target: "public/icon-512.png", size: 512 },
  { source: "icons/maskable.svg", target: "public/icon-maskable-512.png", size: 512 },
  { source: "icons/maskable.svg", target: "public/apple-touch-icon.png", size: 180 },
];

const browser = await chromium.launch();
try {
  for (const { source, target, size } of icons) {
    const svg = await readFile(root + source, "utf8");
    const page = await browser.newPage({ viewport: { width: size, height: size } });
    await page.setContent(
      `<style>html,body{margin:0}svg{display:block;width:${size}px;height:${size}px}</style>${svg}`,
    );
    await page.screenshot({ path: root + target, omitBackground: true });
    await page.close();
    console.log(`${target} (${size}x${size}) from ${source}`);
  }
} finally {
  await browser.close();
}
