import yaml from "js-yaml";
import type { Cluster } from "../api.ts";

// ksail.yaml carries these identifiers; including them makes the rendered YAML match the file
// `ksail init` writes (and the operator's Cluster CR), so a user can copy it straight into a repo.
const API_VERSION = "ksail.io/v1alpha1";
const KIND = "Cluster";

// clusterToYaml renders a Cluster as the ksail.yaml a user would author. Key order is preserved
// (apiVersion/kind/metadata/spec) and default-valued spec fields are kept (the create form already
// resolved them) so the YAML is a complete, runnable config.
export function clusterToYaml(cluster: Cluster): string {
  return yaml.dump(
    {
      apiVersion: API_VERSION,
      kind: KIND,
      metadata: {
        name: cluster.metadata.name,
        namespace: cluster.metadata.namespace ?? "default",
      },
      spec: cluster.spec,
    },
    { noRefs: true, sortKeys: false, lineWidth: 120 },
  );
}

// isMapping reports whether a parsed YAML value is a JSON object (not null, not an array) — i.e. a
// YAML mapping rather than a scalar or sequence.
function isMapping(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// yamlToCluster parses ksail.yaml text into the Cluster shape the create endpoint accepts (metadata +
// spec). apiVersion/kind are ignored (the endpoint infers them). Throws a friendly, inline-able error
// on malformed YAML, a non-mapping document, a non-string/empty metadata.name, or a spec that is not a
// mapping with a cluster section — so the dialog never surfaces a raw TypeError.
export function yamlToCluster(text: string): Cluster {
  const parsed = yaml.load(text);
  if (!isMapping(parsed)) {
    throw new Error("YAML must be a mapping with metadata and spec");
  }

  const metadata = isMapping(parsed.metadata) ? parsed.metadata : {};
  const rawName = metadata.name;
  if (typeof rawName !== "string" || rawName.trim() === "") {
    throw new Error("metadata.name is required");
  }

  const rawNamespace = metadata.namespace;
  const namespace = typeof rawNamespace === "string" && rawNamespace.trim() !== "" ? rawNamespace.trim() : "default";

  if (!isMapping(parsed.spec) || !isMapping(parsed.spec.cluster)) {
    throw new Error("spec.cluster must be a mapping (the cluster configuration)");
  }

  return {
    metadata: { name: rawName.trim(), namespace },
    spec: parsed.spec as Cluster["spec"],
  };
}
