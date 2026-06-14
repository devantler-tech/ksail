import type { View } from "../components/AppShell.tsx";

// DeepLinkTarget is the in-app navigation a ksail:// link resolves to.
export type DeepLinkTarget = { view?: View; clusterKey?: string };

const PREFIX = "ksail://";

// parseDeepLink turns a ksail:// URL into a navigation target, or null if it is not a recognized
// ksail:// link. Supported forms:
//   ksail://                              → the clusters view
//   ksail://clusters | overview | resources | events | secrets | settings
//   ksail://cluster/<name>                → clusters view, select default/<name>
//   ksail://cluster/<namespace>/<name>    → clusters view, select <namespace>/<name>
// Cluster selection keys are "namespace/name" to match App.tsx's clusterKey().
export function parseDeepLink(raw: string): DeepLinkTarget | null {
  if (!raw.startsWith(PREFIX)) {
    return null;
  }

  let segments: string[];
  try {
    segments = raw
      .slice(PREFIX.length)
      .split("/")
      .map((segment) => decodeURIComponent(segment.trim()))
      .filter(Boolean);
  } catch {
    // Malformed percent-encoding — ignore rather than navigate somewhere unexpected.
    return null;
  }

  if (segments.length === 0) {
    return { view: "clusters" };
  }

  const [head, ...rest] = segments;

  switch (head) {
    case "cluster":
      if (rest.length === 0) {
        return { view: "clusters" };
      }

      return {
        view: "clusters",
        clusterKey: rest.length === 1 ? `default/${rest[0]}` : `${rest[0]}/${rest[1]}`,
      };
    case "clusters":
    case "overview":
    case "resources":
    case "events":
    case "secrets":
    case "settings":
      return { view: head };
    default:
      return null;
  }
}
