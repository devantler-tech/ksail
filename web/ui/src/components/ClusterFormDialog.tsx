import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import type { Cluster, ClusterMeta, ProviderInfo } from "../api.ts";
import {
  availableProviders,
  COMPONENT_LABELS,
  preferredProvider,
  unavailableProviders,
  useMeta,
} from "../lib/meta.ts";
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
    (distribution) =>
      availableProviders(meta.providers[distribution] ?? [], providerStatus).length > 0,
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
    provider: preferredProvider(
      availableProviders(meta.providers[distribution] ?? [], providerStatus),
    ),
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

function valuesFromCluster(
  cluster: Cluster,
  meta: ClusterMeta,
  distributions: string[],
): ClusterFormValues {
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
  onClose: () => void;
}) {
  const meta = useMeta();
  const [values, setValues] = useState<ClusterFormValues>(() =>
    createDefaults(meta, distributions, providerStatus),
  );
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

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
  }, [open, mode, initial, meta, distributions, providerStatus]);

  const isEdit = mode === "edit";

  // Edit keeps the cluster's fixed distribution/provider (the selects are disabled); create gates
  // the offered options to what the backend reports as available.
  const offeredDistributions = isEdit
    ? distributions
    : creatableDistributions(meta, distributions, providerStatus);
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
      provider: preferredProvider(
        availableProviders(meta.providers[value] ?? [], providerStatus),
      ),
    }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (values.name.trim() === "" || submitting || noProvidersAvailable) {
      return;
    }

    setSubmitting(true);
    try {
      await onSubmit(values);
      onClose();
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
          <Button
            type="submit"
            form="cluster-form"
            loading={submitting}
            disabled={noProvidersAvailable}
          >
            {isEdit ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <form id="cluster-form" onSubmit={(event) => void handleSubmit(event)} className="space-y-4">
        {noProvidersAvailable ? (
          <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30">
            No providers are available. Start Docker, or configure cloud credentials in Settings, to
            create a cluster.
          </p>
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
              .map((provider) =>
                provider.reason ? `${provider.name} (${provider.reason})` : provider.name,
              )
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
      </form>
    </Modal>
  );
}
