import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import type { Cluster } from "../api.ts";
import { DISTRIBUTIONS, providersFor } from "../lib/distributions.ts";
import { COMPONENT_FIELDS, componentDefaults } from "../lib/options.ts";
import { Button, Modal, SelectField, TextField } from "./ui.tsx";

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

function createDefaults(): ClusterFormValues {
  return {
    name: "",
    namespace: "default",
    distribution: DISTRIBUTIONS[0],
    provider: providersFor(DISTRIBUTIONS[0])[0],
    controlPlanes: "1",
    workers: "0",
    ...componentDefaults(),
  };
}

function valuesFromCluster(cluster: Cluster): ClusterFormValues {
  const spec = cluster.spec?.cluster ?? {};
  const defaults = componentDefaults();
  const distribution = spec.distribution || DISTRIBUTIONS[0];

  return {
    name: cluster.metadata.name,
    namespace: cluster.metadata.namespace ?? "default",
    distribution,
    provider: spec.provider || providersFor(distribution)[0],
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
  onSubmit,
  onClose,
}: {
  open: boolean;
  mode: FormMode;
  initial: Cluster | null;
  onSubmit: (values: ClusterFormValues) => Promise<void>;
  onClose: () => void;
}) {
  const [values, setValues] = useState<ClusterFormValues>(createDefaults);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  // Reset the form whenever it opens (to create defaults, or the cluster being edited).
  useEffect(() => {
    if (!open) {
      return;
    }
    setValues(mode === "edit" && initial ? valuesFromCluster(initial) : createDefaults());
    setAdvancedOpen(false);
  }, [open, mode, initial]);

  const isEdit = mode === "edit";

  function setField<K extends keyof ClusterFormValues>(key: K, value: ClusterFormValues[K]) {
    setValues((current) => ({ ...current, [key]: value }));
  }

  function handleDistributionChange(value: string) {
    setValues((current) => ({ ...current, distribution: value, provider: providersFor(value)[0] }));
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (values.name.trim() === "" || submitting) {
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
          <Button type="submit" form="cluster-form" loading={submitting}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <form id="cluster-form" onSubmit={(event) => void handleSubmit(event)} className="space-y-4">
        <TextField
          label="Name"
          value={values.name}
          autoFocus={!isEdit}
          disabled={isEdit}
          placeholder="my-cluster"
          onChange={(event) => setField("name", event.target.value)}
        />
        <div className="grid grid-cols-2 gap-3">
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
            {DISTRIBUTIONS.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </SelectField>
        </div>
        <SelectField
          label="Provider"
          value={values.provider}
          disabled={isEdit}
          onChange={(event) => setField("provider", event.target.value)}
        >
          {providersFor(values.distribution).map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </SelectField>

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
              <div className="grid grid-cols-2 gap-3">
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
              <div className="grid grid-cols-2 gap-3">
                {COMPONENT_FIELDS.map((field) => (
                  <SelectField
                    key={field.key}
                    label={field.label}
                    value={values[field.key]}
                    onChange={(event) => setField(field.key, event.target.value)}
                  >
                    {field.options.map((option) => (
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
