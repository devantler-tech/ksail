/**
 * Unit tests pinning the `ksail cluster list --output json` contract.
 *
 * Run with the Node test runner against the compiled output (see the `test`
 * script in package.json). These tests exercise the pure parser only — no
 * VSCode host is required.
 */

import assert from "node:assert/strict";
import test from "node:test";
import { parseClusterListJson } from "../ksail/clusterList.js";

test("parses the documented array shape and captures distribution", () => {
  const json = JSON.stringify([
    { name: "dev", provider: "docker", distribution: "Vanilla", ttl: null },
    { name: "edge", provider: "docker", distribution: "K3s", ttl: "2h 30m" },
    { name: "prod", provider: "hetzner", distribution: "Talos", ttl: null },
  ]);

  const clusters = parseClusterListJson(json);

  assert.equal(clusters.length, 3);
  assert.deepEqual(
    clusters.map((c) => c.name),
    ["dev", "edge", "prod"],
  );
  assert.deepEqual(
    clusters.map((c) => c.distribution),
    ["Vanilla", "K3s", "Talos"],
  );
  assert.equal(clusters[0].provider, "docker");
  assert.equal(clusters[2].provider, "hetzner");
});

test("captures ttl when present and leaves it undefined for null", () => {
  const json = JSON.stringify([
    { name: "a", provider: "docker", distribution: "Vanilla", ttl: "1h" },
    { name: "b", provider: "docker", distribution: "Vanilla", ttl: null },
  ]);

  const clusters = parseClusterListJson(json);

  assert.equal(clusters[0].ttl, "1h");
  assert.equal(clusters[1].ttl, undefined);
});

test("defaults status to unknown (no status field in the JSON yet)", () => {
  const clusters = parseClusterListJson(
    '[{"name":"a","provider":"docker","distribution":"Vanilla","ttl":null}]',
  );

  assert.equal(clusters[0].status, "unknown");
});

test("returns [] for an empty array", () => {
  assert.deepEqual(parseClusterListJson("[]"), []);
});

test("returns [] for empty / whitespace output", () => {
  assert.deepEqual(parseClusterListJson(""), []);
  assert.deepEqual(parseClusterListJson("   \n  "), []);
});

test("returns [] for a JSON null payload", () => {
  assert.deepEqual(parseClusterListJson("null"), []);
});

test("ignores entries without a usable name", () => {
  const json = JSON.stringify([
    { provider: "docker", distribution: "Vanilla" },
    { name: "", provider: "docker", distribution: "Vanilla" },
    { name: "keep", provider: "docker", distribution: "Vanilla" },
  ]);

  const clusters = parseClusterListJson(json);

  assert.equal(clusters.length, 1);
  assert.equal(clusters[0].name, "keep");
});

test("tolerates a missing distribution (leaves it undefined)", () => {
  const clusters = parseClusterListJson(
    '[{"name":"a","provider":"docker","ttl":null}]',
  );

  assert.equal(clusters[0].distribution, undefined);
});

test("preserves provider/distribution casing as emitted", () => {
  // The human table lowercases provider; JSON may differ — consumers must match
  // case-insensitively, so we assert the value is passed through verbatim here.
  const clusters = parseClusterListJson(
    '[{"name":"a","provider":"Docker","distribution":"VCluster","ttl":null}]',
  );

  assert.equal(clusters[0].provider, "Docker");
  assert.equal(clusters[0].distribution, "VCluster");
});

test("throws on malformed JSON", () => {
  assert.throws(() => parseClusterListJson("not json"));
});

test("throws when the top-level JSON is not an array", () => {
  assert.throws(
    () => parseClusterListJson('{"name":"a"}'),
    /not an array/,
  );
});
