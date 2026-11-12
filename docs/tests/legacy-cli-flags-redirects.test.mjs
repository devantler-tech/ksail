import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";

import {
  currentPage,
  legacyPages,
  legacyRedirects,
  pageExists,
  removedPages,
  renamedPages,
} from "../src/redirects/legacy-cli-flags.mjs";

// The old addresses must keep reaching the command reference as it is generated today
// (ksail#7818).
test("every legacy command page redirects to a page the reference has", () => {
  const redirects = legacyRedirects();

  assert.equal(
    Object.keys(redirects).length,
    legacyPages.length - removedPages.length + 1,
    "one redirect per legacy page whose command still exists, plus the section address",
  );

  for (const [from, to] of Object.entries(redirects)) {
    assert.match(from, /^\/configuration\/cli-flags\/(.+\/)?$/);
    assert.match(to, /^\/cli-flags\/(.+\/)?$/);
  }
});

test("the addresses other sites linked to land on their current pages", () => {
  const redirects = legacyRedirects();

  assert.equal(
    redirects["/configuration/cli-flags/cluster/cluster-root/"],
    "/cli-flags/cluster/cluster-root/",
  );
  assert.equal(
    redirects["/configuration/cli-flags/workload/workload-root/"],
    "/cli-flags/workload/workload-root/",
  );
  assert.equal(redirects["/configuration/cli-flags/"], "/cli-flags/");
  assert.equal(redirects["/configuration/cli-flags/root/"], "/cli-flags/");
});

test("nested legacy pages move to their command group", () => {
  assert.equal(
    currentPage("workload/create/secret/workload-create-secret-generic"),
    "workload/workload-create-secret-generic",
  );
  assert.equal(
    currentPage("workload/apply/workload-apply-root"),
    "workload/workload-apply-root",
  );
});

test("renamed commands redirect to their replacement", () => {
  const redirects = legacyRedirects();

  assert.equal(
    redirects["/configuration/cli-flags/cluster/cluster-init/"],
    "/cli-flags/project/project-init/",
  );
  assert.equal(
    redirects["/configuration/cli-flags/cipher/cipher-decrypt/"],
    "/cli-flags/workload/workload-cipher-decrypt/",
  );
});

test("the renamed and removed lists only name legacy pages", () => {
  for (const page of [...Object.keys(renamedPages), ...removedPages]) {
    assert.ok(legacyPages.includes(page), `${page} is not a legacy page`);
  }

  assert.equal(new Set(legacyPages).size, legacyPages.length, "legacy pages repeat");
});

test("removed commands get no redirect and have no page today", () => {
  const redirects = legacyRedirects();

  for (const page of removedPages) {
    assert.equal(currentPage(page), undefined);
    assert.equal(redirects[`/configuration/cli-flags/${page}/`], undefined);

    // A command that comes back under its old name should be redirected again.
    const segments = page.split("/");
    assert.equal(pageExists(`${segments[0]}/${segments.at(-1)}`), false);
  }
});

test("a legacy page without a current page fails instead of redirecting nowhere", () => {
  const directory = mkdtempSync(join(tmpdir(), "ksail-cli-flags-"));

  try {
    assert.throws(
      () => legacyRedirects(pathToFileURL(`${directory}/`)),
      /has no page "workload\/workload-cipher-decrypt" for the old address/,
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
