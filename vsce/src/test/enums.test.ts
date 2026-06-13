/**
 * Unit tests for the single-sourced enum catalog.
 *
 * DRIFT GUARD: the distribution/provider value sets below mirror the Go enums
 * (pkg/apis/cluster/v1alpha1/distribution.go ValidDistributions and
 * provider.go ValidProviders). If those change, both the catalog and these
 * tests must be updated together.
 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  describerFor,
  ENUM_CATALOG,
  getEnumDescription,
  getEnumValues,
} from "../ksail/enums.js";

test("distribution covers all six Go distributions", () => {
  assert.deepEqual(getEnumValues("distribution"), [
    "Vanilla",
    "K3s",
    "Talos",
    "VCluster",
    "KWOK",
    "EKS",
  ]);
});

test("provider covers all five Go providers including Kubernetes", () => {
  assert.deepEqual(getEnumValues("provider"), [
    "Docker",
    "Hetzner",
    "Omni",
    "AWS",
    "Kubernetes",
  ]);
});

test("every distribution and provider value has a description", () => {
  for (const field of ["distribution", "provider"] as const) {
    for (const value of getEnumValues(field)) {
      assert.notEqual(
        getEnumDescription(field, value),
        "",
        `${field} value "${value}" is missing a description`,
      );
    }
  }
});

test("getEnumValues returns [] for an unknown field", () => {
  assert.deepEqual(getEnumValues("nonexistent"), []);
});

test("getEnumDescription returns '' for unknown field or value", () => {
  assert.equal(getEnumDescription("nonexistent", "x"), "");
  assert.equal(getEnumDescription("distribution", "Nope"), "");
});

test("describerFor binds a field-scoped describer", () => {
  const describe = describerFor("provider");
  assert.equal(describe("Docker"), getEnumDescription("provider", "Docker"));
  assert.equal(describe("AWS"), getEnumDescription("provider", "AWS"));
});

test("catalog covers the wizard's enum fields", () => {
  for (const field of [
    "distribution",
    "provider",
    "cni",
    "csi",
    "metrics-server",
    "cert-manager",
    "policy-engine",
    "gitops-engine",
  ]) {
    assert.ok(ENUM_CATALOG[field], `catalog is missing field "${field}"`);
    assert.ok(
      ENUM_CATALOG[field].values.length > 0,
      `field "${field}" has no values`,
    );
  }
});
