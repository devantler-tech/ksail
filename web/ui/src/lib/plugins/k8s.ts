// k8s.ts reproduces the slice of Headlamp's `lib/k8s` data layer that plugins consume: the `KubeObject`
// class hierarchy (statics-driven, so a plugin's `class Kustomization extends KubeObject { static
// apiVersion = 'kustomize.toolkit.fluxcd.io/v1'; … }` works unmodified), the `K8s.ResourceClasses` map
// (including `CustomResourceDefinition`, which real plugins list at load to detect installed CRDs), the
// imperative `apiList`/`apiGet` statics, and the `useList`/`useGet` hooks — all backed by KSail's
// read-only kube-apiserver proxy (see pkg/cli/clusterapi/kubeproxy.go) and scoped to the live active
// cluster. It is the faithful, minimal counterpart to `@kinvolk/headlamp-plugin/lib/K8s`.

import * as React from "react";
import { useClusterScopedList, type WatchBinding } from "./useClusterScopedList.ts";
import type { ClusterRef } from "./pluginLib.ts";
import { kubeObjectKey, watchStreamURL, type RawKubeObject } from "./watchStream.ts";

// activeClusterGetter resolves the active cluster for KubeObject's static (non-hook) data methods.
// Headlamp's KubeObject statics read an implicit active cluster; KSail mirrors that by having the loader
// set this getter once (setActiveCluster), so `Kustomization.apiList(...)` / `.useList()` follow the live
// cluster without each class closing over it.
let activeClusterGetter: () => ClusterRef | null = () => null;

// setActiveCluster records the live active-cluster getter the loader supplies (read on each fetch).
export function setActiveCluster(getter: () => ClusterRef | null): void {
  activeClusterGetter = getter;
}

// ApiError is an Error carrying the apiserver HTTP status. Plugins branch on `error.status === 404` (e.g.
// the Flux plugin's "is Flux installed?" check), so the status must survive from the proxy fetch to the
// caller. useClusterScopedList preserves the thrown Error instance, so the status reaches useList's
// [items, error] tuple unchanged.
export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

type KubeJSON = Record<string, unknown>;

// ListOptions mirror the Headlamp useList/apiList options object (a plugin calls
// `Pod.useList({ namespace })`). Only namespace filtering is applied today; the rest are accepted for
// API parity.
export interface ListOptions {
  namespace?: string | string[];
  cluster?: string;
  labelSelector?: string;
  fieldSelector?: string;
}

// ResourceEndpoint is the apiserver coordinates parsed from a class's statics.
interface ResourceEndpoint {
  group: string;
  version: string;
  plural: string;
  namespaced: boolean;
}

// KubeObject is the statics-driven base every resource class extends. A subclass sets the four statics
// (kind/apiName/apiVersion/isNamespaced); the static data methods derive the apiserver path from them.
// Instances expose the metadata/spec/status accessors Headlamp plugins read.
export class KubeObject {
  jsonData: KubeJSON;
  // cluster is the active cluster the object was read from (set by the fetch path); some plugin link
  // components scope a details link to it.
  cluster?: string;

  constructor(raw: KubeJSON) {
    this.jsonData = raw ?? {};
  }

  // Statics a subclass overrides. Defaults keep TypeScript happy for the base.
  static kind = "";
  static apiName = "";
  static apiVersion: string | string[] = "";
  static isNamespaced = true;

  // apiEndpoint parses the (first) apiVersion into group/version plus the plural/namespaced flags.
  static get apiEndpoint(): ResourceEndpoint {
    const apiVersion = Array.isArray(this.apiVersion) ? (this.apiVersion[0] ?? "") : this.apiVersion;
    const slash = apiVersion.indexOf("/");
    const group = slash === -1 ? "" : apiVersion.slice(0, slash);
    const version = slash === -1 ? apiVersion : apiVersion.slice(slash + 1);

    return { group, version, plural: this.apiName, namespaced: this.isNamespaced };
  }

  // create wraps a raw object as an instance of the concrete subclass (so instanceof + statics hold).
  static create(this: KubeObjectClass, raw: KubeJSON): KubeObject {
    return new this(raw);
  }

  // apiList starts a one-shot list of the collection and invokes onList with wrapped items (filtered to
  // opts.namespace when given). It returns a starter function; calling it begins the request and returns
  // a canceller — matching Headlamp's `apiList(onList, onError, opts)()` shape that plugins invoke at
  // module scope (e.g. the Flux plugin's CRD-installed probe).
  static apiList(
    this: KubeObjectClass,
    onList: (items: KubeObject[]) => void,
    onError?: (err: ApiError) => void,
    opts?: ListOptions,
  ): () => () => void {
    const cls = this;
    const listPath = collectionPath(cls.apiEndpoint);

    return () => {
      const cluster = activeClusterGetter();
      if (!cluster) {
        onError?.(new ApiError("pluginLib: no active cluster", 0));

        return () => undefined;
      }

      let cancelled = false;
      proxyList(cluster, listPath, cls)
        .then((items) => {
          if (!cancelled) {
            onList(filterByNamespace(items, opts?.namespace));
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            onError?.(toApiError(err));
          }
        });

      return () => {
        cancelled = true;
      };
    };
  }

  // useList is the hook list: live [items, error] scoped to the active cluster, filtered to
  // opts.namespace when given. Plugins call `K8s.ResourceClasses.<Kind>.useList({ namespace })`; the
  // no-arg form still works. Live updates flow through the shared watch machinery (WS multiplexer → SSE
  // → polling), same as the rest of the plugin K8s layer.
  static useList(this: KubeObjectClass, opts: ListOptions = {}): [KubeObject[], ApiError | null] {
    const cls = this;
    const listPath = collectionPath(cls.apiEndpoint);
    const namespaceKey = Array.isArray(opts.namespace) ? opts.namespace.join(",") : opts.namespace ?? "";

    const cluster = activeClusterGetter();
    const clusterName = cluster?.name ?? null;
    const clusterNamespace = cluster?.namespace ?? null;
    const hasCluster = clusterName !== null && clusterNamespace !== null;

    const watch: WatchBinding<KubeObject> = {
      url: hasCluster ? watchStreamURL(clusterNamespace, clusterName, listPath) : "",
      mux: hasCluster ? { clusterId: clusterName, path: listPath, query: "" } : undefined,
      toItem: (raw: RawKubeObject) => cls.create(raw as KubeJSON),
      keyOf: (item: KubeObject) => kubeObjectKey(item.jsonData as RawKubeObject),
    };

    const [items, error] = useClusterScopedList<KubeObject>(
      activeClusterGetter,
      async (active) => filterByNamespace(await proxyList(active, listPath, cls), opts.namespace),
      [listPath, namespaceKey],
      watch,
    );

    return [items, error as ApiError | null];
  }

  // apiGet starts a one-shot get of a single object (Headlamp's apiGet), returning a starter→canceller
  // like apiList.
  static apiGet(
    this: KubeObjectClass,
    onGet: (item: KubeObject) => void,
    name: string,
    namespace?: string,
    onError?: (err: ApiError) => void,
  ): () => () => void {
    const cls = this;
    const objPath = objectPath(cls.apiEndpoint, name, namespace);

    return () => {
      const cluster = activeClusterGetter();
      if (!cluster) {
        onError?.(new ApiError("pluginLib: no active cluster", 0));

        return () => undefined;
      }

      let cancelled = false;
      proxyGet(cluster, objPath, cls)
        .then((item) => {
          if (!cancelled) {
            onGet(item);
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            onError?.(toApiError(err));
          }
        });

      return () => {
        cancelled = true;
      };
    };
  }

  // useGet is the hook form of apiGet: live [item, error] for one object.
  static useGet(
    this: KubeObjectClass,
    name: string,
    namespace?: string,
  ): [KubeObject | null, ApiError | null] {
    const cls = this;
    const objPath = objectPath(cls.apiEndpoint, name, namespace);

    const [items, error] = useClusterScopedList<KubeObject>(
      activeClusterGetter,
      async (active) => [await proxyGet(active, objPath, cls)],
      [objPath],
    );

    return [items[0] ?? null, error as ApiError | null];
  }

  // useApiList subscribes onList to the live list (Headlamp parity), implemented over useList. onError
  // fires when the list errors.
  static useApiList(
    this: KubeObjectClass,
    onList: (items: KubeObject[]) => void,
    onError?: (err: ApiError) => void,
    opts: ListOptions = {},
  ): void {
    const [items, error] = this.useList(opts);

    React.useEffect(() => {
      onList(items);
    }, [items, onList]);

    React.useEffect(() => {
      if (error) {
        onError?.(error);
      }
    }, [error, onError]);
  }

  // ---- instance accessors mirroring Headlamp's KubeObject ----
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

  getValue(prop: string): unknown {
    return this.jsonData[prop];
  }
}

// KubeObjectClass is the static side of KubeObject (a plugin subclass's constructor type).
export type KubeObjectClass = typeof KubeObject;

// ResourceClasses is the Headlamp `K8s.ResourceClasses` map: a KubeObject subclass per kind.
export type ResourceClasses = Record<string, KubeObjectClass>;

// CustomResourceDefinition is exported by name (Headlamp parity); the Flux plugin lists it at load via
// `K8s.ResourceClasses.CustomResourceDefinition.apiList(...)`.
export class CustomResourceDefinition extends KubeObject {
  static kind = "CustomResourceDefinition";
  static apiName = "customresourcedefinitions";
  static apiVersion = "apiextensions.k8s.io/v1";
  static isNamespaced = false;
}

// Event is exported by name (plugins import it from lib/K8s/event); namespaced core v1 events.
export class Event extends KubeObject {
  static kind = "Event";
  static apiName = "events";
  static apiVersion = "v1";
  static isNamespaced = true;
}

// ResourceClassDef is the minimal descriptor makeKubeObjectClass needs.
export interface ResourceClassDef {
  kind: string;
  apiName: string;
  apiVersion: string | string[];
  isNamespaced: boolean;
}

// makeKubeObjectClass mints a KubeObject subclass with the given statics — Headlamp's makeKubeObject /
// makeCustomResourceClass building block, used for the built-in kinds and re-exported (via
// makeCustomResourceClass.ts) so plugins can define CRD classes.
export function makeKubeObjectClass(def: ResourceClassDef): KubeObjectClass {
  return class extends KubeObject {
    static kind = def.kind;
    static apiName = def.apiName;
    static apiVersion = def.apiVersion;
    static isNamespaced = def.isNamespaced;
  };
}

// BUILTIN_DEFS covers the core/apps/batch/networking kinds plugins most commonly list. CustomResource-
// Definition and Event are added to the map from their named classes below.
const BUILTIN_DEFS: ResourceClassDef[] = [
  { kind: "Pod", apiName: "pods", apiVersion: "v1", isNamespaced: true },
  { kind: "Service", apiName: "services", apiVersion: "v1", isNamespaced: true },
  { kind: "ConfigMap", apiName: "configmaps", apiVersion: "v1", isNamespaced: true },
  { kind: "Secret", apiName: "secrets", apiVersion: "v1", isNamespaced: true },
  { kind: "Namespace", apiName: "namespaces", apiVersion: "v1", isNamespaced: false },
  { kind: "Node", apiName: "nodes", apiVersion: "v1", isNamespaced: false },
  { kind: "PersistentVolumeClaim", apiName: "persistentvolumeclaims", apiVersion: "v1", isNamespaced: true },
  { kind: "ServiceAccount", apiName: "serviceaccounts", apiVersion: "v1", isNamespaced: true },
  { kind: "Deployment", apiName: "deployments", apiVersion: "apps/v1", isNamespaced: true },
  { kind: "ReplicaSet", apiName: "replicasets", apiVersion: "apps/v1", isNamespaced: true },
  { kind: "StatefulSet", apiName: "statefulsets", apiVersion: "apps/v1", isNamespaced: true },
  { kind: "DaemonSet", apiName: "daemonsets", apiVersion: "apps/v1", isNamespaced: true },
  { kind: "Job", apiName: "jobs", apiVersion: "batch/v1", isNamespaced: true },
  { kind: "CronJob", apiName: "cronjobs", apiVersion: "batch/v1", isNamespaced: true },
  { kind: "Ingress", apiName: "ingresses", apiVersion: "networking.k8s.io/v1", isNamespaced: true },
];

// makeResourceClasses builds the Headlamp `K8s.ResourceClasses` map — a KubeObject subclass per built-in
// kind plus the named CustomResourceDefinition/Event classes.
export function makeResourceClasses(): ResourceClasses {
  const classes: ResourceClasses = {};

  for (const def of BUILTIN_DEFS) {
    classes[def.kind] = makeKubeObjectClass(def);
  }
  classes.CustomResourceDefinition = CustomResourceDefinition;
  classes.Event = Event;

  return classes;
}

// collectionPath builds the apiserver collection URL for a class (cluster-wide, or a single namespace
// when given for a namespaced kind).
function collectionPath(ep: ResourceEndpoint, namespace?: string): string {
  const prefix = ep.group === "" ? `/api/${ep.version}` : `/apis/${ep.group}/${ep.version}`;

  return ep.namespaced && namespace ? `${prefix}/namespaces/${namespace}/${ep.plural}` : `${prefix}/${ep.plural}`;
}

// objectPath builds the apiserver URL for a single named object.
function objectPath(ep: ResourceEndpoint, name: string, namespace?: string): string {
  return `${collectionPath(ep, namespace)}/${name}`;
}

// filterByNamespace keeps items in the requested namespace(s); cluster-scoped items (no namespace) always
// pass. An empty/absent filter returns everything (cluster-wide list).
function filterByNamespace(items: KubeObject[], namespace?: string | string[]): KubeObject[] {
  if (namespace === undefined || (Array.isArray(namespace) && namespace.length === 0)) {
    return items;
  }

  const wanted = new Set(Array.isArray(namespace) ? namespace : [namespace]);

  return items.filter((item) => {
    const ns = item.getNamespace();

    return ns === undefined || wanted.has(ns);
  });
}

// toApiError normalizes an unknown thrown value to an ApiError (status 0 when not from the proxy).
function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) {
    return err;
  }

  return new ApiError(err instanceof Error ? err.message : String(err), 0);
}

// proxyList fetches a collection through the kube-proxy and wraps each item as the given class, throwing
// ApiError (with the HTTP status) on a non-OK response so callers can branch on 404 etc.
async function proxyList(cluster: ClusterRef, listPath: string, cls: KubeObjectClass): Promise<KubeObject[]> {
  const base = `/api/v1/clusters/${encodeURIComponent(cluster.namespace)}/${encodeURIComponent(cluster.name)}`;
  const response = await fetch(`${base}/proxy/${listPath.replace(/^\//, "")}`);
  if (!response.ok) {
    throw new ApiError(`apiserver GET ${listPath} failed: ${response.status}`, response.status);
  }

  const body = (await response.json()) as { items?: KubeJSON[] };

  return (body.items ?? []).map((item) => {
    const obj = cls.create(item);
    obj.cluster = cluster.name;

    return obj;
  });
}

// proxyGet fetches a single object through the kube-proxy and wraps it as the given class.
async function proxyGet(cluster: ClusterRef, objPath: string, cls: KubeObjectClass): Promise<KubeObject> {
  const base = `/api/v1/clusters/${encodeURIComponent(cluster.namespace)}/${encodeURIComponent(cluster.name)}`;
  const response = await fetch(`${base}/proxy/${objPath.replace(/^\//, "")}`);
  if (!response.ok) {
    throw new ApiError(`apiserver GET ${objPath} failed: ${response.status}`, response.status);
  }

  const obj = cls.create((await response.json()) as KubeJSON);
  obj.cluster = cluster.name;

  return obj;
}
