import { useState, type FormEvent } from "react";
import { DISTRIBUTIONS, providersFor } from "../lib/distributions.ts";
import { Button, Modal, SelectField, TextField } from "./ui.tsx";

export interface CreateClusterInput {
  name: string;
  namespace: string;
  distribution: string;
  provider: string;
}

export function CreateClusterDialog({
  open,
  onClose,
  onCreate,
}: {
  open: boolean;
  onClose: () => void;
  onCreate: (input: CreateClusterInput) => Promise<void>;
}) {
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [distribution, setDistribution] = useState<string>(DISTRIBUTIONS[0]);
  const [provider, setProvider] = useState<string>(providersFor(DISTRIBUTIONS[0])[0]);
  const [submitting, setSubmitting] = useState(false);

  const providers = providersFor(distribution);

  function reset() {
    setName("");
    setNamespace("default");
    setDistribution(DISTRIBUTIONS[0]);
    setProvider(providersFor(DISTRIBUTIONS[0])[0]);
  }

  // Changing the distribution narrows the valid providers; snap to the recommended default.
  function handleDistributionChange(value: string) {
    setDistribution(value);
    setProvider(providersFor(value)[0]);
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() === "" || submitting) {
      return;
    }

    setSubmitting(true);
    try {
      await onCreate({
        name: name.trim(),
        namespace: namespace.trim() || "default",
        distribution,
        provider,
      });
      reset();
      onClose();
    } catch {
      // The parent surfaces the error via a toast; keep the dialog open so the user can adjust.
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={submitting ? () => undefined : onClose}
      title="Create cluster"
      description="Provision a new cluster managed by the operator."
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" form="create-cluster-form" loading={submitting}>
            Create
          </Button>
        </>
      }
    >
      <form id="create-cluster-form" onSubmit={(event) => void handleSubmit(event)} className="space-y-4">
        <TextField
          label="Name"
          value={name}
          autoFocus
          placeholder="my-cluster"
          onChange={(event) => setName(event.target.value)}
        />
        <TextField
          label="Namespace"
          value={namespace}
          onChange={(event) => setNamespace(event.target.value)}
        />
        <div className="grid grid-cols-2 gap-3">
          <SelectField
            label="Distribution"
            value={distribution}
            onChange={(event) => handleDistributionChange(event.target.value)}
          >
            {DISTRIBUTIONS.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </SelectField>
          <SelectField
            label="Provider"
            value={provider}
            onChange={(event) => setProvider(event.target.value)}
          >
            {providers.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </SelectField>
        </div>
      </form>
    </Modal>
  );
}
