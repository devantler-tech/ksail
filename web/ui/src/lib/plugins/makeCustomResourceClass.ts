// makeCustomResourceClass / apiFactory are Headlamp's factory helpers for minting custom-resource
// classes. A plugin that does not subclass KubeObject directly builds its CRD classes through these
// (Headlamp's `@kinvolk/headlamp-plugin/lib/lib/k8s/crd`). They are thin wrappers over makeKubeObjectClass
// so a minted class flows through the same kube-proxy + watch machinery as the built-in kinds.

import { makeKubeObjectClass, type KubeObjectClass } from "./k8s.ts";

// CustomResourceClassArgs mirrors Headlamp's makeCustomResourceClass argument: one or more
// group/version pairs (the served apiVersions), the plural resource name, and the namespaced flag.
export interface CustomResourceClassArgs {
  apiInfo: { group: string; version: string }[];
  pluralName: string;
  singularName?: string;
  kind?: string;
  isNamespaced: boolean;
}

// apiVersionsFromInfo joins each {group, version} into an apiVersion string ("group/version", or just
// "version" for the core group).
function apiVersionsFromInfo(apiInfo: { group: string; version: string }[]): string | string[] {
  const versions = apiInfo.map((info) => (info.group ? `${info.group}/${info.version}` : info.version));

  return versions.length === 1 ? versions[0] : versions;
}

// makeCustomResourceClass mints a KubeObject subclass for a CRD from its apiInfo/plural/namespaced.
export function makeCustomResourceClass(args: CustomResourceClassArgs): KubeObjectClass {
  return makeKubeObjectClass({
    kind: args.kind ?? args.singularName ?? args.pluralName,
    apiName: args.pluralName,
    apiVersion: apiVersionsFromInfo(args.apiInfo),
    isNamespaced: args.isNamespaced,
  });
}

// apiFactory mints a cluster-scoped resource class from a single group/version/plural (Headlamp's
// lower-level factory). A bare-version (core group) is supported by passing group "".
export function apiFactory(group: string, version: string, plural: string): KubeObjectClass {
  return makeKubeObjectClass({
    kind: plural,
    apiName: plural,
    apiVersion: group ? `${group}/${version}` : version,
    isNamespaced: false,
  });
}

// apiFactoryWithNamespace mints a namespace-scoped resource class.
export function apiFactoryWithNamespace(group: string, version: string, plural: string): KubeObjectClass {
  return makeKubeObjectClass({
    kind: plural,
    apiName: plural,
    apiVersion: group ? `${group}/${version}` : version,
    isNamespaced: true,
  });
}
