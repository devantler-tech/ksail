/**
 * Cluster List JSON Parsing
 *
 * Pure (vscode-free) parsing of `ksail cluster list --output json`.
 * Kept dependency-free so it can be unit-tested with the node test runner
 * without standing up the VSCode extension host.
 *
 * The CLI emits a JSON array of objects shaped like:
 *   { "name": string, "provider": string, "distribution": string, "ttl": string | null }
 *
 * Source of truth for the contract: pkg/cli/cmd/cluster/list.go (the `--output json`
 * encoder added in the same Phase 4 PR; the human table is PROVIDER/DISTRIBUTION/CLUSTER[/TTL]).
 */

/**
 * Cluster status.
 *
 * `ksail cluster list` does not yet emit a status field (planned for Phase 5);
 * until then status is sniffed separately (see detectClusterStatus) and defaults
 * to "unknown".
 */
export type ClusterStatus = "running" | "stopped" | "unknown";

/**
 * Cluster information parsed from `ksail cluster list --output json`.
 *
 * `distribution` comes straight from the JSON, so the extension no longer
 * re-derives it from `docker ps`. `status` is populated later by status sniffing.
 */
export interface ClusterInfo {
  name: string;
  provider: string;
  distribution?: string;
  ttl?: string;
  status?: ClusterStatus;
}

/**
 * One raw element of the `cluster list --output json` array.
 */
interface RawClusterListEntry {
  name?: unknown;
  provider?: unknown;
  distribution?: unknown;
  ttl?: unknown;
}

/**
 * Coerce a JSON value to a non-empty trimmed string, or undefined.
 */
function asString(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

/**
 * Parse the JSON output of `ksail cluster list --output json`.
 *
 * Accepts the documented JSON-array shape. Returns [] for empty/whitespace
 * output or for the `[]`/`null` cases. Throws on malformed JSON or on JSON that
 * is not an array, so callers surface a clear error instead of silently showing
 * an empty cluster list (the exact failure this rewrite fixes).
 *
 * Provider/distribution casing is preserved as-emitted; consumers that compare
 * against the Go enum values should do so case-insensitively.
 */
export function parseClusterListJson(output: string): ClusterInfo[] {
  const trimmed = output.trim();
  if (trimmed.length === 0) {
    return [];
  }

  const parsed: unknown = JSON.parse(trimmed);
  if (parsed === null) {
    return [];
  }
  if (!Array.isArray(parsed)) {
    throw new Error("cluster list JSON output is not an array");
  }

  const clusters: ClusterInfo[] = [];
  for (const element of parsed) {
    if (element === null || typeof element !== "object") {
      continue;
    }
    const entry = element as RawClusterListEntry;
    const name = asString(entry.name);
    if (name === undefined) {
      continue; // a cluster without a name is unusable downstream
    }
    clusters.push({
      name,
      provider: asString(entry.provider) ?? "",
      distribution: asString(entry.distribution),
      ttl: asString(entry.ttl),
      status: "unknown",
    });
  }

  return clusters;
}
