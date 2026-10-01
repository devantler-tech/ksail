// Detected cluster identity, shared by every surface that shows a cluster's distribution and
// provider (the sidebar switcher, the clusters table, the Overview). A cluster whose spec already
// names both needs nothing; for any other instance the store lists its Nodes once per session and
// keeps what detectClusterIdentity proves, so all surfaces show the same answer.
//
// Reads are bounded (MAX_CONCURRENT at a time) so a kubeconfig with many contexts does not open a
// request per cluster at once. A failed or empty read is stored as "nothing proven", which renders
// as "—" and is not retried until the page reloads: an unreachable cluster must not be polled.

import { useEffect, useSyncExternalStore } from "react";
import { listResources, type Cluster } from "../api.ts";
import { detectClusterIdentity, type ClusterIdentity } from "./clusterIdentity.ts";
import { clusterInstanceKey, clusterKey, splitClusterKey } from "./k8s.ts";

const MAX_CONCURRENT = 4;

const identities = new Map<string, ClusterIdentity>();
const requested = new Set<string>();
const queue: { key: string; instanceKey: string }[] = [];
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
export function primeIdentity(instanceKey: string, identity: ClusterIdentity) {
  requested.add(instanceKey);
  identities.set(instanceKey, identity);
  notify();
}

function pump() {
  while (running < MAX_CONCURRENT && queue.length > 0) {
    const { key, instanceKey } = queue.shift()!;
    const [namespace, name] = splitClusterKey(key);
    running += 1;

    listResources(namespace, name, "Node")
      .then((list) => detectClusterIdentity(list.items ?? []))
      .catch((): ClusterIdentity => ({}))
      .then((identity) => {
        if (!identities.has(instanceKey)) {
          identities.set(instanceKey, identity);
        }
        notify();
      })
      .finally(() => {
        running -= 1;
        pump();
      });
  }
}

function request(key: string, instanceKey: string) {
  if (requested.has(instanceKey)) {
    return;
  }

  requested.add(instanceKey);
  queue.push({ key, instanceKey });
  pump();
}

// useDetectedIdentities returns the detected identity of each given cluster that needs one, keyed
// by address, starting a detection for any instance not yet requested. Cached results and pending
// reads stay with the instance they belong to, so a late response cannot identify its replacement.
// enabled gates it on the
// workload-read capability; without it nothing is fetched and every cluster reads as unknown.
export function useDetectedIdentities(clusters: Cluster[], enabled: boolean): Map<string, ClusterIdentity> {
  useSyncExternalStore(subscribe, () => version);

  const wanted = enabled ? clusters.filter(needsDetection) : [];
  const signature = JSON.stringify(wanted.map((cluster) => [clusterKey(cluster), clusterInstanceKey(cluster)]));

  useEffect(() => {
    for (const [key, instanceKey] of JSON.parse(signature) as [string, string][]) {
      request(key, instanceKey);
    }
  }, [signature]);

  const found = new Map<string, ClusterIdentity>();
  for (const cluster of wanted) {
    const identity = identities.get(clusterInstanceKey(cluster));
    if (identity) {
      found.set(clusterKey(cluster), identity);
    }
  }

  return found;
}
