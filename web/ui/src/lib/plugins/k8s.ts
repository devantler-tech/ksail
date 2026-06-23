// k8s.ts reproduces the slice of Headlamp's `K8s` data layer that plugins consume — the
// `ResourceClasses.<Kind>.useList()` surface and the `KubeObject` instances it yields — backed by
// KSail's read-only kube-apiserver proxy (see pkg/cli/clusterapi/kubeproxy.go) and scoped to the live
// active cluster. It is the faithful, minimal counterpart to `@kinvolk/headlamp-plugin/lib/K8s`: a
// Headlamp plugin that does `const [pods] = K8s.ResourceClasses.Pod.useList()` gets live cluster data
// without modification. The full Headlamp class hierarchy (useGet, apiFactory, watch, CRD classes) is
// intentionally out of scope here — this covers the common list-and-render path.

import type { ClusterRef } from "./pluginLib.ts";
import { useAsyncList } from "./useAsyncList.ts";

// KubeObject wraps a raw Kubernetes resource as Headlamp plugins expect: `jsonData` holds the raw
// object and the metadata/spec/status accessors plus getName/getNamespace mirror Headlamp's KubeObject
// instance API closely enough for the common read paths.
export class KubeObject {
  jsonData: Record<string, unknown>;

  constructor(raw: Record<string, unknown>) {
    this.jsonData = raw ?? {};
  }

  get metadata(): Record<string, unknown> {
    return (this.jsonData.metadata as Record<string, unknown>) ?? {};
  }

  get spec(): unknown {
    return this.jsonData.spec;
  }

  get status(): unknown {
    return this.jsonData.status;
  }

  get kind(): unknown {
    return this.jsonData.kind;
  }

  getName(): string {
    return (this.metadata.name as string) ?? "";
  }

  getNamespace(): string | undefined {
    return this.metadata.namespace as string | undefined;
  }
}

// ResourceClass is the static surface a plugin uses: `K8s.ResourceClasses.Pod.useList()`. It is the
// list subset of Headlamp's KubeObject class statics — enough for the common "fetch and render a table"
// plugin pattern.
export interface ResourceClass {
  useList: () => [KubeObject[], Error | null];
}

export type ResourceClasses = Record<string, ResourceClass>;

interface ResourceDef {
  // listPath is the apiserver collection path the kube-proxy fetches (cluster-wide; the proxy honours
  // the same credentials as the resource browser).
  listPath: string;
}

// RESOURCE_DEFS maps each reproduced Headlamp ResourceClass name to its apiserver list path. The set
// covers the core/apps/batch/networking kinds plugins most commonly list; extend as needed.
const RESOURCE_DEFS: Record<string, ResourceDef> = {
  Pod: { listPath: "/api/v1/pods" },
  Service: { listPath: "/api/v1/services" },
  ConfigMap: { listPath: "/api/v1/configmaps" },
  Secret: { listPath: "/api/v1/secrets" },
  Namespace: { listPath: "/api/v1/namespaces" },
  Node: { listPath: "/api/v1/nodes" },
  Event: { listPath: "/api/v1/events" },
  PersistentVolumeClaim: { listPath: "/api/v1/persistentvolumeclaims" },
  ServiceAccount: { listPath: "/api/v1/serviceaccounts" },
  Deployment: { listPath: "/apis/apps/v1/deployments" },
  ReplicaSet: { listPath: "/apis/apps/v1/replicasets" },
  StatefulSet: { listPath: "/apis/apps/v1/statefulsets" },
  DaemonSet: { listPath: "/apis/apps/v1/daemonsets" },
  Job: { listPath: "/apis/batch/v1/jobs" },
  CronJob: { listPath: "/apis/batch/v1/cronjobs" },
  Ingress: { listPath: "/apis/networking.k8s.io/v1/ingresses" },
};

// proxyList fetches a collection through the kube-proxy and wraps each item as a KubeObject.
async function proxyList(cluster: ClusterRef, listPath: string): Promise<KubeObject[]> {
  const base = `/api/v1/clusters/${encodeURIComponent(cluster.namespace)}/${encodeURIComponent(cluster.name)}`;
  const response = await fetch(`${base}/proxy/${listPath.replace(/^\//, "")}`);
  if (!response.ok) {
    throw new Error(`apiserver GET ${listPath} failed: ${response.status}`);
  }

  const body = (await response.json()) as { items?: Record<string, unknown>[] };

  return (body.items ?? []).map((item) => new KubeObject(item));
}

// makeResourceClass builds one ResourceClass whose useList hook fetches via the proxy and re-fetches
// when the active cluster changes (keyed on the cluster's primitive name/namespace, mirroring the
// useResourceList shim so a cluster switch reloads the data). Live updates come from useAsyncList,
// which polls the fetcher on an interval; an absent cluster yields an empty list.
function makeResourceClass(def: ResourceDef, getCluster: () => ClusterRef | null): ResourceClass {
  return {
    useList(): [KubeObject[], Error | null] {
      const cluster = getCluster();
      const clusterName = cluster?.name ?? null;
      const clusterNamespace = cluster?.namespace ?? null;

      return useAsyncList<KubeObject>(() => {
        if (clusterName === null || clusterNamespace === null) {
          return Promise.resolve([]);
        }

        return proxyList({ namespace: clusterNamespace, name: clusterName }, def.listPath);
      }, [clusterName, clusterNamespace]);
    },
  };
}

// makeResourceClasses builds the Headlamp `K8s.ResourceClasses` map — a useList()-bearing entry per
// common kind, each backed by the kube-proxy and scoped to the live active cluster.
export function makeResourceClasses(getCluster: () => ClusterRef | null): ResourceClasses {
  const classes: ResourceClasses = {};

  for (const [kind, def] of Object.entries(RESOURCE_DEFS)) {
    classes[kind] = makeResourceClass(def, getCluster);
  }

  return classes;
}
