import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import type { Cluster, ClusterMeta, ClusterSpec, ProviderInfo } from "../api.ts";
import { clusterToYaml, yamlToCluster } from "../lib/clusterYaml.ts";
import { cx } from "../lib/cx.ts";
import { availableProviders, COMPONENT_LABELS, preferredProvider, unavailableProviders, useMeta } from "../lib/meta.ts";
import { Button, Modal, SelectField, TextField } from "./ui.tsx";

// creatableDistributions narrows the offered distributions to those that still have at least one
// available provider once provider gating (providerStatus) is applied. With no gating it is the
// full list unchanged.
function creatableDistributions(
  meta: ClusterMeta,
  distributions: string[],
  providerStatus: ProviderInfo[] | null | undefined,
): string[] {
  return distributions.filter(
    (distribution) => availableProviders(meta.providers[distribution] ?? [], providerStatus).length > 0,
  );
}

export interface ClusterFormValues {
  name: string;
  namespace: string;
  distribution: string;
  provider: string;
  controlPlanes: string;
  workers: string;
  cni: string;
  csi: string;
  cdi: string;
  metricsServer: string;
  loadBalancer: string;
  certManager: string;
  policyEngine: string;
  gitOpsEngine: string;
}

export type FormMode = "create" | "edit";

// specFromValues maps the form fields to a ClusterSpec. Node counts are parsed to numbers; the
// component enums are sent verbatim (default values serialize away server-side via omitzero). Lives
// here (next to ClusterFormValues) so both App's submit and the YAML preview can build a spec.
export function specFromValues(values: ClusterFormValues): ClusterSpec {
  const controlPlanes = Number.parseInt(values.controlPlanes, 10);
  const workers = Number.parseInt(values.workers, 10);

  return {
    distribution: values.distribution as ClusterSpec["distribution"],
    provider: values.provider as ClusterSpec["provider"],
    controlPlanes: Number.isNaN(controlPlanes) ? undefined : controlPlanes,
    workers: Number.isNaN(workers) ? undefined : workers,
    cni: values.cni as ClusterSpec["cni"],
    csi: values.csi as ClusterSpec["csi"],
    cdi: values.cdi as ClusterSpec["cdi"],
    metricsServer: values.metricsServer as ClusterSpec["metricsServer"],
    loadBalancer: values.loadBalancer as ClusterSpec["loadBalancer"],
    certManager: values.certManager as ClusterSpec["certManager"],
    policyEngine: values.policyEngine as ClusterSpec["policyEngine"],
    gitOpsEngine: values.gitOpsEngine as ClusterSpec["gitOpsEngine"],
  };
}

// clusterFromValues builds the full Cluster (metadata + spec.cluster) the create endpoint accepts —
// the source for the YAML preview and the create request.
function clusterFromValues(values: ClusterFormValues): Cluster {
  return {
    metadata: {
      name: values.name.trim() || "my-cluster",
      namespace: values.namespace.trim() || "default",
    },
    spec: { cluster: specFromValues(values) },
  };
}

// CreateTemplate is a named starting point for a new cluster. Overrides are applied on top of the
// backend-resolved defaults; only distribution + node topology are set (always-valid fields), and the
// provider is recomputed for the chosen distribution. A template is offered only when its distribution
// is creatable on this backend.
export interface CreateTemplate {
  id: string;
  label: string;
  description: string;
  distribution: string;
  overrides: Partial<ClusterFormValues>;
}

export const CREATE_TEMPLATES: CreateTemplate[] = [
  {
    id: "kind-dev",
    label: "Kind — development",
    description: "Single-node upstream Kubernetes in Docker",
    distribution: "Vanilla",
    overrides: { controlPlanes: "1", workers: "0" },
  },
  {
    id: "k3s-light",
    label: "K3s — lightweight",
    description: "Lightweight K3s in Docker, one node",
    distribution: "K3s",
    overrides: { controlPlanes: "1", workers: "0" },
  },
  {
    id: "talos-ha",
    label: "Talos — highly available",
    description: "3 control planes + 2 workers on Talos Linux",
    distribution: "Talos",
    overrides: { controlPlanes: "3", workers: "2" },
  },
  {
    id: "kwok-sim",
    label: "KWOK — simulated",
    description: "Simulated cluster (no real workloads)",
    distribution: "KWOK",
    overrides: { controlPlanes: "1", workers: "0" },
  },
];

// componentDefaults returns the API-default selection for every component field, keyed by the
// component's spec key, sourced from the server's /meta payload.
function componentDefaults(meta: ClusterMeta): Record<string, string> {
  const defaults: Record<string, string> = {};
  for (const component of meta.components) {
    defaults[component.key] = component.default;
  }
  return defaults;
}

function createDefaults(
  meta: ClusterMeta,
  distributions: string[],
  providerStatus: ProviderInfo[] | null | undefined,
): ClusterFormValues {
  const offered = creatableDistributions(meta, distributions, providerStatus);
  const distribution = offered[0] ?? distributions[0] ?? meta.distributions[0] ?? "";
  const defaults = componentDefaults(meta);
  return {
    name: "",
    namespace: "default",
    distribution,
    provider: preferredProvider(availableProviders(meta.providers[distribution] ?? [], providerStatus)),
    controlPlanes: "1",
    workers: "0",
    cni: defaults.cni ?? "",
    csi: defaults.csi ?? "",
    cdi: defaults.cdi ?? "",
    metricsServer: defaults.metricsServer ?? "",
    loadBalancer: defaults.loadBalancer ?? "",
    certManager: defaults.certManager ?? "",
    policyEngine: defaults.policyEngine ?? "",
    gitOpsEngine: defaults.gitOpsEngine ?? "",
  };
}

function valuesFromCluster(cluster: Cluster, meta: ClusterMeta, distributions: string[]): ClusterFormValues {
  const spec = cluster.spec?.cluster ?? {};
  const defaults = componentDefaults(meta);
  const distribution = spec.distribution || distributions[0] || meta.distributions[0] || "";

  return {
    name: cluster.metadata.name,
    namespace: cluster.metadata.namespace ?? "default",
    distribution,
    provider: spec.provider || preferredProvider(meta.providers[distribution] ?? []),
    controlPlanes: spec.controlPlanes === undefined ? "1" : String(spec.controlPlanes),
    workers: spec.workers === undefined ? "0" : String(spec.workers),
    cni: spec.cni || defaults.cni,
    csi: spec.csi || defaults.csi,
    cdi: spec.cdi || defaults.cdi,
    metricsServer: spec.metricsServer || defaults.metricsServer,
    loadBalancer: spec.loadBalancer || defaults.loadBalancer,
    certManager: spec.certManager || defaults.certManager,
    policyEngine: spec.policyEngine || defaults.policyEngine,
    gitOpsEngine: spec.gitOpsEngine || defaults.gitOpsEngine,
  };
}

export function ClusterFormDialog({
  open,
  mode,
  initial,
  distributions,
  providerStatus,
  onSubmit,
  onSubmitRaw,
  onClose,
}: {
  open: boolean;
  mode: FormMode;
  initial: Cluster | null;
  // distributions the create form offers (backend-advertised via config.distributions). The
  // provider matrix and component options for the selected one still come from /api/v1/meta.
  distributions: string[];
  // providerStatus gates which providers are offered (backend-advertised via config.providers).
  // null/undefined means no gating (the operator), so every valid provider is offered.
  providerStatus?: ProviderInfo[] | null;
  onSubmit: (values: ClusterFormValues) => Promise<void>;
  // onSubmitRaw creates a cluster from a YAML-authored Cluster (lossless full-spec), used by the
  // create dialog's YAML mode. Omit it (e.g. edit mode) to disable YAML authoring.
  onSubmitRaw?: (cluster: Cluster) => Promise<void>;
  onClose: () => void;
}) {
  const meta = useMeta();
  const [values, setValues] = useState<ClusterFormValues>(() => createDefaults(meta, distributions, providerStatus));
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  // YAML authoring (create only): the editor text is the source of truth while view === "yaml".
  const [view, setView] = useState<"form" | "yaml">("form");
  const [yamlText, setYamlText] = useState("");
  const [yamlError, setYamlError] = useState<string | null>(null);

  // Reset the form whenever it opens (to create defaults, or the cluster being edited).
  useEffect(() => {
    if (!open) {
      return;
    }
    setValues(
      mode === "edit" && initial
        ? valuesFromCluster(initial, meta, distributions)
        : createDefaults(meta, distributions, providerStatus),
    );
    setAdvancedOpen(false);
    setView("form");
    setYamlError(null);
  }, [open, mode, initial, meta, distributions, providerStatus]);

  const isEdit = mode === "edit";
  const yamlEnabled = !isEdit && onSubmitRaw !== undefined;

  // Edit keeps the cluster's fixed distribution/provider (the selects are disabled); create gates
  // the offered options to what the backend reports as available.
  const offeredDistributions = isEdit ? distributions : creatableDistributions(meta, distributions, providerStatus);
  const offeredProviders = isEdit
    ? (meta.providers[values.distribution] ?? [])
    : availableProviders(meta.providers[values.distribution] ?? [], providerStatus);
  const blockedProviders = isEdit ? [] : unavailableProviders(providerStatus);
  const noProvidersAvailable = !isEdit && offeredDistributions.length === 0;

  function setField<K extends keyof ClusterFormValues>(key: K, value: ClusterFormValues[K]) {
    setValues((current) => ({ ...current, [key]: value }));
  }

  function handleDistributionChange(value: string) {
    setValues((current) => ({
      ...current,
      distribution: value,
      provider: preferredProvider(availableProviders(meta.providers[value] ?? [], providerStatus)),
    }));
  }

  // applyTemplate resets the form to backend-resolved defaults, then applies the template's
  // distribution (recomputing a valid provider) and its overrides.
  function applyTemplate(template: CreateTemplate) {
    const base = createDefaults(meta, distributions, providerStatus);
    setValues({
      ...base,
      distribution: template.distribution,
      provider: preferredProvider(availableProviders(meta.providers[template.distribution] ?? [], providerStatus)),
      ...template.overrides,
    });
  }

  // showYaml regenerates the editor from the current form values; showForm parses the editor back
  // into the form (so edits to known fields carry over), staying in YAML on a parse error.
  function showYaml() {
    setYamlText(clusterToYaml(clusterFromValues(values)));
    setYamlError(null);
    setView("yaml");
  }

  function showForm() {
    try {
      const cluster = yamlToCluster(yamlText);
      setValues(valuesFromCluster(cluster, meta, distributions));
      setYamlError(null);
      setView("form");
    } catch (error) {
      setYamlError(error instanceof Error ? error.message : String(error));
    }
  }

  async function submitForm() {
    if (values.name.trim() === "" || noProvidersAvailable) {
      return;
    }

    await onSubmit(values);
    onClose();
  }

  async function submitYaml() {
    if (onSubmitRaw === undefined) {
      return;
    }

    let cluster: Cluster;
    try {
      cluster = yamlToCluster(yamlText);
    } catch (error) {
      setYamlError(error instanceof Error ? error.message : String(error));

      return;
    }

    await onSubmitRaw(cluster);
    onClose();
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (submitting) {
      return;
    }

    setSubmitting(true);
    try {
      await (view === "yaml" ? submitYaml() : submitForm());
    } catch {
      // Parent surfaces the error via a toast; keep the dialog open so the user can adjust.
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={submitting ? () => undefined : onClose}
      title={isEdit ? "Edit cluster" : "Create cluster"}
      description={
        isEdit
          ? "Update the cluster's configuration. The operator reconciles changes."
          : "Provision a new cluster managed by the operator."
      }
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" form="cluster-form" loading={submitting} disabled={noProvidersAvailable}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <form id="cluster-form" onSubmit={(event) => void handleSubmit(event)} className="space-y-4">
        {noProvidersAvailable ? (
          <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30">
            No providers are available. Start Docker, or configure cloud credentials in Settings, to create a cluster.
          </p>
        ) : null}
        {yamlEnabled ? (
          <div className="flex justify-end">
            <div className="inline-flex overflow-hidden rounded-md ring-1 ring-inset ring-slate-300 dark:ring-slate-700">
              <button
                type="button"
                onClick={() => {
                  if (view !== "form") {
                    showForm();
                  }
                }}
                className={cx(
                  "px-3 py-1 text-xs font-medium",
                  view === "form"
                    ? "bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900"
                    : "bg-white text-slate-600 dark:bg-slate-800 dark:text-slate-300",
                )}
              >
                Form
              </button>
              <button
                type="button"
                onClick={() => {
                  if (view !== "yaml") {
                    showYaml();
                  }
                }}
                className={cx(
                  "px-3 py-1 text-xs font-medium",
                  view === "yaml"
                    ? "bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900"
                    : "bg-white text-slate-600 dark:bg-slate-800 dark:text-slate-300",
                )}
              >
                YAML
              </button>
            </div>
          </div>
        ) : null}
        {view === "yaml" ? (
          <div className="space-y-2">
            <textarea
              value={yamlText}
              onChange={(event) => setYamlText(event.target.value)}
              spellCheck={false}
              aria-label="Cluster YAML"
              className="h-80 w-full rounded-md border border-slate-300 bg-slate-50 p-3 font-mono text-xs leading-relaxed text-slate-800 focus:border-slate-400 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
            />
            {yamlError ? <p className="text-sm text-red-600 dark:text-red-400">{yamlError}</p> : null}
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Edit the full ksail.yaml. Create applies every field as-is; switching to Form keeps only the fields the
              form models.
            </p>
          </div>
        ) : (
          <>
            {!isEdit && CREATE_TEMPLATES.some((template) => offeredDistributions.includes(template.distribution)) ? (
              <SelectField
                label="Template"
                value=""
                onChange={(event) => {
                  const template = CREATE_TEMPLATES.find((item) => item.id === event.target.value);
                  if (template) {
                    applyTemplate(template);
                  }
                }}
              >
                <option value="">Start from a template…</option>
                {CREATE_TEMPLATES.filter((template) => offeredDistributions.includes(template.distribution)).map(
                  (template) => (
                    <option key={template.id} value={template.id} title={template.description}>
                      {template.label}
                    </option>
                  ),
                )}
              </SelectField>
            ) : null}
            <TextField
              label="Name"
              value={values.name}
              autoFocus={!isEdit}
              disabled={isEdit}
              placeholder="my-cluster"
              onChange={(event) => setField("name", event.target.value)}
            />
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <TextField
                label="Namespace"
                value={values.namespace}
                disabled={isEdit}
                onChange={(event) => setField("namespace", event.target.value)}
              />
              <SelectField
                label="Distribution"
                value={values.distribution}
                disabled={isEdit}
                onChange={(event) => handleDistributionChange(event.target.value)}
              >
                {offeredDistributions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </SelectField>
            </div>
            <SelectField
              label="Provider"
              value={values.provider}
              disabled={isEdit || offeredProviders.length === 0}
              onChange={(event) => setField("provider", event.target.value)}
            >
              {offeredProviders.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </SelectField>
            {blockedProviders.length > 0 ? (
              <p className="text-xs text-slate-500 dark:text-slate-400">
                Unavailable:{" "}
                {blockedProviders
                  .map((provider) => (provider.reason ? `${provider.name} (${provider.reason})` : provider.name))
                  .join(", ")}
                . Configure credentials in Settings to enable them.
              </p>
            ) : null}

            <div className="border-t border-slate-200 pt-2 dark:border-slate-800">
              <button
                type="button"
                onClick={() => setAdvancedOpen((current) => !current)}
                className="flex w-full items-center gap-1.5 py-1 text-sm font-medium text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-white"
                aria-expanded={advancedOpen}
              >
                {advancedOpen ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                Advanced options
              </button>

              {advancedOpen ? (
                <div className="mt-3 space-y-4">
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <TextField
                      label="Control planes"
                      type="number"
                      min={0}
                      value={values.controlPlanes}
                      onChange={(event) => setField("controlPlanes", event.target.value)}
                    />
                    <TextField
                      label="Workers"
                      type="number"
                      min={0}
                      value={values.workers}
                      onChange={(event) => setField("workers", event.target.value)}
                    />
                  </div>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    {meta.components.map((component) => (
                      <SelectField
                        key={component.key}
                        label={COMPONENT_LABELS[component.key] ?? component.key}
                        value={values[component.key]}
                        onChange={(event) => setField(component.key, event.target.value)}
                      >
                        {component.values.map((option) => (
                          <option key={option} value={option}>
                            {option}
                          </option>
                        ))}
                      </SelectField>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          </>
        )}
      </form>
    </Modal>
  );
}
