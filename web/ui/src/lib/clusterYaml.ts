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

// yamlToCluster parses ksail.yaml text into the Cluster shape the create endpoint accepts (metadata +
// spec). apiVersion/kind are ignored (the endpoint infers them). Throws a friendly error on malformed
// YAML, a non-mapping document, or a missing metadata.name so the dialog can surface it inline.
export function yamlToCluster(text: string): Cluster {
  const parsed = yaml.load(text);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("YAML must be a mapping with metadata and spec");
  }

  const document = parsed as {
    metadata?: { name?: string; namespace?: string };
    spec?: Cluster["spec"];
  };
  const name = document.metadata?.name?.trim();
  if (!name) {
    throw new Error("metadata.name is required");
  }

  return {
    metadata: { name, namespace: document.metadata?.namespace?.trim() || "default" },
    spec: document.spec,
  };
}
