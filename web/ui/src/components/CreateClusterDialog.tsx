import { useState, type FormEvent } from "react";
import { Button, Modal, SelectField, TextField } from "./ui.tsx";

export interface CreateClusterInput {
  name: string;
  namespace: string;
  distribution: string;
}

export function CreateClusterDialog({
  open,
  onClose,
  onCreate,
  distributions,
}: {
  open: boolean;
  onClose: () => void;
  onCreate: (input: CreateClusterInput) => Promise<void>;
  distributions: string[];
}) {
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [distribution, setDistribution] = useState(distributions[0] ?? "");
  const [submitting, setSubmitting] = useState(false);

  function reset() {
    setName("");
    setNamespace("default");
    setDistribution(distributions[0] ?? "");
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() === "" || submitting) {
      return;
    }

    setSubmitting(true);
    try {
      await onCreate({ name: name.trim(), namespace: namespace.trim() || "default", distribution });
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
        <div className="grid grid-cols-2 gap-3">
          <TextField
            label="Namespace"
            value={namespace}
            onChange={(event) => setNamespace(event.target.value)}
          />
          <SelectField
            label="Distribution"
            value={distribution}
            onChange={(event) => setDistribution(event.target.value)}
          >
            {distributions.map((value) => (
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
