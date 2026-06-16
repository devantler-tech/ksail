/**
 * Unit tests for the single-sourced context-name table.
 *
 * Mirrors the Go convention in pkg/apis/cluster/v1alpha1/distribution.go
 * (ContextName) and pkg/svc/detector/cluster/cluster.go.
 */

import assert from "node:assert/strict";
import test from "node:test";
import { isKSailContext, parseClusterName, resolveContext } from "../ksail/contexts.js";

test("resolveContext builds the per-distribution context prefix", () => {
  assert.equal(resolveContext("dev", "Vanilla"), "kind-dev");
  assert.equal(resolveContext("dev", "K3s"), "k3d-dev");
  assert.equal(resolveContext("dev", "Talos"), "admin@dev");
  assert.equal(resolveContext("dev", "VCluster"), "vcluster-docker_dev");
  assert.equal(resolveContext("dev", "KWOK"), "kwok-dev");
});

test("resolveContext matches the distribution case-insensitively", () => {
  assert.equal(resolveContext("dev", "vanilla"), "kind-dev");
  assert.equal(resolveContext("dev", "k3s"), "k3d-dev");
  assert.equal(resolveContext("dev", "vcluster"), "vcluster-docker_dev");
});

test("resolveContext falls back to the bare name for EKS / unknown / missing", () => {
  assert.equal(resolveContext("dev", "EKS"), "dev");
  assert.equal(resolveContext("dev", "Mystery"), "dev");
  assert.equal(resolveContext("dev", undefined), "dev");
});

test("isKSailContext recognizes all five managed prefixes", () => {
  assert.equal(isKSailContext("kind-dev"), true);
  assert.equal(isKSailContext("k3d-dev"), true);
  assert.equal(isKSailContext("admin@dev"), true);
  assert.equal(isKSailContext("vcluster-docker_dev"), true);
  assert.equal(isKSailContext("kwok-dev"), true);
});

test("isKSailContext rejects non-KSail contexts", () => {
  assert.equal(isKSailContext("minikube"), false);
  assert.equal(isKSailContext("user@dev.eksctl.io"), false);
  assert.equal(isKSailContext("dev"), false);
});

test("parseClusterName extracts the cluster name from a managed context", () => {
  assert.equal(parseClusterName("kind-dev"), "dev");
  assert.equal(parseClusterName("k3d-edge"), "edge");
  assert.equal(parseClusterName("admin@prod"), "prod");
  assert.equal(parseClusterName("vcluster-docker_virt"), "virt");
  assert.equal(parseClusterName("kwok-sim"), "sim");
});

test("parseClusterName returns undefined for unmanaged contexts", () => {
  assert.equal(parseClusterName("minikube"), undefined);
});

test("resolveContext and parseClusterName round-trip for prefixed distributions", () => {
  for (const [distribution, name] of [
    ["Vanilla", "dev"],
    ["K3s", "edge"],
    ["Talos", "prod"],
    ["VCluster", "virt"],
    ["KWOK", "sim"],
  ] as const) {
    const context = resolveContext(name, distribution);
    assert.equal(parseClusterName(context), name, `${distribution} round-trip`);
  }
});
