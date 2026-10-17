import { existsSync } from "node:fs";

// The documentation site before this one published the command reference below
// /configuration/cli-flags/, and older pages, posts and bookmarks still link there
// (ksail#7818). These are the pages it published, as paths below that address. The list is
// closed: nothing has been published there since the reference moved to /cli-flags/.
export const legacyPages = [
  "cipher/cipher-decrypt",
  "cipher/cipher-edit",
  "cipher/cipher-encrypt",
  "cipher/cipher-import",
  "cipher/cipher-root",
  "cluster/cluster-connect",
  "cluster/cluster-create",
  "cluster/cluster-delete",
  "cluster/cluster-init",
  "cluster/cluster-list",
  "cluster/cluster-root",
  "cluster/cluster-start",
  "cluster/cluster-stop",
  "cluster/info/cluster-info-dump",
  "cluster/info/cluster-info-root",
  "root",
  "workload/apply/workload-apply-edit-last-applied",
  "workload/apply/workload-apply-root",
  "workload/apply/workload-apply-set-last-applied",
  "workload/apply/workload-apply-view-last-applied",
  "workload/create/secret/workload-create-secret-docker-registry",
  "workload/create/secret/workload-create-secret-generic",
  "workload/create/secret/workload-create-secret-root",
  "workload/create/secret/workload-create-secret-tls",
  "workload/create/service/workload-create-service-clusterip",
  "workload/create/service/workload-create-service-externalname",
  "workload/create/service/workload-create-service-loadbalancer",
  "workload/create/service/workload-create-service-nodeport",
  "workload/create/service/workload-create-service-root",
  "workload/create/source/workload-create-source-git",
  "workload/create/source/workload-create-source-helm",
  "workload/create/source/workload-create-source-oci",
  "workload/create/source/workload-create-source-root",
  "workload/create/workload-create-clusterrole",
  "workload/create/workload-create-clusterrolebinding",
  "workload/create/workload-create-configmap",
  "workload/create/workload-create-cronjob",
  "workload/create/workload-create-deployment",
  "workload/create/workload-create-helmrelease",
  "workload/create/workload-create-ingress",
  "workload/create/workload-create-job",
  "workload/create/workload-create-kustomization",
  "workload/create/workload-create-namespace",
  "workload/create/workload-create-poddisruptionbudget",
  "workload/create/workload-create-priorityclass",
  "workload/create/workload-create-quota",
  "workload/create/workload-create-role",
  "workload/create/workload-create-rolebinding",
  "workload/create/workload-create-root",
  "workload/create/workload-create-serviceaccount",
  "workload/create/workload-create-token",
  "workload/gen/secret/workload-gen-secret-docker-registry",
  "workload/gen/secret/workload-gen-secret-generic",
  "workload/gen/secret/workload-gen-secret-root",
  "workload/gen/secret/workload-gen-secret-tls",
  "workload/gen/service/workload-gen-service-clusterip",
  "workload/gen/service/workload-gen-service-externalname",
  "workload/gen/service/workload-gen-service-loadbalancer",
  "workload/gen/service/workload-gen-service-nodeport",
  "workload/gen/service/workload-gen-service-root",
  "workload/gen/workload-gen-clusterrole",
  "workload/gen/workload-gen-clusterrolebinding",
  "workload/gen/workload-gen-configmap",
  "workload/gen/workload-gen-cronjob",
  "workload/gen/workload-gen-deployment",
  "workload/gen/workload-gen-helmrelease",
  "workload/gen/workload-gen-ingress",
  "workload/gen/workload-gen-job",
  "workload/gen/workload-gen-namespace",
  "workload/gen/workload-gen-poddisruptionbudget",
  "workload/gen/workload-gen-priorityclass",
  "workload/gen/workload-gen-quota",
  "workload/gen/workload-gen-role",
  "workload/gen/workload-gen-rolebinding",
  "workload/gen/workload-gen-root",
  "workload/gen/workload-gen-serviceaccount",
  "workload/rollout/workload-rollout-history",
  "workload/rollout/workload-rollout-pause",
  "workload/rollout/workload-rollout-restart",
  "workload/rollout/workload-rollout-resume",
  "workload/rollout/workload-rollout-root",
  "workload/rollout/workload-rollout-status",
  "workload/rollout/workload-rollout-undo",
  "workload/workload-delete",
  "workload/workload-describe",
  "workload/workload-edit",
  "workload/workload-exec",
  "workload/workload-explain",
  "workload/workload-export",
  "workload/workload-expose",
  "workload/workload-get",
  "workload/workload-import",
  "workload/workload-install",
  "workload/workload-logs",
  "workload/workload-push",
  "workload/workload-reconcile",
  "workload/workload-root",
  "workload/workload-scale",
  "workload/workload-validate",
  "workload/workload-wait",
];

// Legacy pages whose command now has a page under another name, mapped to that page below
// /cli-flags/. The empty path is the reference's index page.
export const renamedPages = {
  root: "",
  "cipher/cipher-root": "workload/workload-cipher-root",
  "cipher/cipher-decrypt": "workload/workload-cipher-decrypt",
  "cipher/cipher-edit": "workload/workload-cipher-edit",
  "cipher/cipher-encrypt": "workload/workload-cipher-encrypt",
  "cipher/cipher-import": "workload/workload-cipher-import",
  "cluster/cluster-init": "project/project-init",
  "cluster/info/cluster-info-root": "cluster/cluster-info",
};

// Legacy pages whose command was removed without a replacement. Their addresses stay
// "page not found" on purpose.
export const removedPages = ["cluster/info/cluster-info-dump"];

const referenceDirectory = new URL("../content/docs/cli-flags/", import.meta.url);

// currentPage returns the page below /cli-flags/ that took over a legacy page, or undefined
// when its command was removed. Apart from the renamed pages, every page kept its name and
// lost the subdirectories below its command group, so
// workload/create/secret/workload-create-secret-generic is now
// workload/workload-create-secret-generic.
export function currentPage(legacyPage) {
  if (removedPages.includes(legacyPage)) {
    return undefined;
  }

  if (Object.hasOwn(renamedPages, legacyPage)) {
    return renamedPages[legacyPage];
  }

  const segments = legacyPage.split("/");

  return `${segments[0]}/${segments.at(-1)}`;
}

// pageExists reports whether the generated command reference in directory has page.
export function pageExists(page, directory = referenceDirectory) {
  return existsSync(new URL(page === "" ? "index.mdx" : `${page}.mdx`, directory));
}

// legacyRedirects maps every legacy address whose command still exists to its current page.
// It throws when that page is missing from the generated reference, so a later rename fails
// the docs build here instead of quietly sending old links to "page not found".
export function legacyRedirects(directory = referenceDirectory) {
  const redirects = { "/configuration/cli-flags/": "/cli-flags/" };

  for (const legacyPage of legacyPages) {
    const page = currentPage(legacyPage);

    if (page === undefined) {
      continue;
    }

    if (!pageExists(page, directory)) {
      throw new Error(
        `The command reference has no page "${page}" for the old address ` +
          `/configuration/cli-flags/${legacyPage}/. Map it to the page that replaced it, ` +
          "or list it as removed, in docs/src/redirects/legacy-cli-flags.mjs.",
      );
    }

    redirects[`/configuration/cli-flags/${legacyPage}/`] =
      page === "" ? "/cli-flags/" : `/cli-flags/${page}/`;
  }

  return redirects;
}
