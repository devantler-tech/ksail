// Detected cluster identity, shared by every surface that shows a cluster's distribution and
// provider (the sidebar switcher, the clusters table, the Overview). A cluster whose spec already
// names both needs nothing; for any other cluster the store lists its Nodes once per session and
// keeps what detectClusterIdentity proves, so all surfaces show the same answer.
//
// Reads are bounded (MAX_CONCURRENT at a time) so a kubeconfig with many contexts does not open a
// request per cluster at once. A failed or empty read is stored as "nothing proven", which renders
// as "—" and is not retried until the page reloads: an unreachable cluster must not be polled.

import { useEffect, useSyncExternalStore } from "react";
import { listResources, type Cluster } from "../api.ts";
import { detectClusterIdentity, type ClusterIdentity } from "./clusterIdentity.ts";
import { clusterKey, splitClusterKey } from "./k8s.ts";

const MAX_CONCURRENT = 4;

const identities = new Map<string, ClusterIdentity>();
const requested = new Set<string>();
const queue: string[] = [];
const listeners = new Set<() => void>();
let running = 0;
// version changes whenever a detection lands, so useSyncExternalStore re-renders subscribers.
let version = 0;

function notify() {
  version += 1;
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

// needsDetection reports whether a cluster's spec leaves its distribution or provider unknown.
export function needsDetection(cluster: Cluster): boolean {
  const spec = cluster.spec?.cluster;

  return !spec?.distribution || !spec?.provider;
}

// primeIdentity records an identity another loader already derived (the Overview lists Nodes for
// its health cards), so the store does not list them a second time.
export function primeIdentity(key: string, identity: ClusterIdentity) {
  requested.add(key);
  identities.set(key, identity);
  notify();
}

function pump() {
  while (running < MAX_CONCURRENT && queue.length > 0) {
    const key = queue.shift()!;
    const [namespace, name] = splitClusterKey(key);
    running += 1;

    listResources(namespace, name, "Node")
      .then((list) => detectClusterIdentity(list.items ?? []))
      .catch((): ClusterIdentity => ({}))
      .then((identity) => {
        if (!identities.has(key)) {
          identities.set(key, identity);
        }
        notify();
      })
      .finally(() => {
        running -= 1;
        pump();
      });
  }
}

function request(key: string) {
  if (requested.has(key)) {
    return;
  }

  requested.add(key);
  queue.push(key);
  pump();
}

// useDetectedIdentities returns the detected identity of each given cluster that needs one, keyed
// by cluster key, starting a detection for any not yet requested. enabled gates it on the
// workload-read capability; without it nothing is fetched and every cluster reads as unknown.
export function useDetectedIdentities(clusters: Cluster[], enabled: boolean): Map<string, ClusterIdentity> {
  useSyncExternalStore(subscribe, () => version);

  const wanted = enabled ? clusters.filter(needsDetection).map(clusterKey) : [];
  const signature = wanted.join("\n");

  useEffect(() => {
    for (const key of signature === "" ? [] : signature.split("\n")) {
      request(key);
    }
  }, [signature]);

  const found = new Map<string, ClusterIdentity>();
  for (const key of wanted) {
    const identity = identities.get(key);
    if (identity) {
      found.set(key, identity);
    }
  }

  return found;
}

