import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  createCluster,
  deleteCluster,
  getConfig,
  listClusters,
  type Cluster,
} from "./api.ts";

// Only distributions the operator can currently provision in-cluster (see
// pkg/operator/provisioner.go buildDistributionConfig). Creating others would fail reconciliation.
const DISTRIBUTIONS = ["VCluster"];

export function App() {
  const [readOnly, setReadOnly] = useState(true);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const list = await listClusters();
      setClusters(list.items ?? []);
      setError(null);
    } catch (err) {
      setError(String(err));
    }
  }, []);

  useEffect(() => {
    let active = true;

    async function init() {
      try {
        const config = await getConfig();
        if (active) {
          setReadOnly(config.readOnly);
        }
      } catch (err) {
        if (active) {
          setError(String(err));
        }
      }

      await refresh();
      if (active) {
        setLoading(false);
      }
    }

    void init();
    const timer = setInterval(() => void refresh(), 10000);

    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [refresh]);

  return (
    <div className="mx-auto max-w-5xl p-6 font-sans text-slate-800">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">KSail Clusters</h1>
        <button
          type="button"
          onClick={() => void refresh()}
          className="rounded bg-slate-200 px-3 py-1 text-sm hover:bg-slate-300"
        >
          Refresh
        </button>
      </header>

      {readOnly && (
        <div className="mb-4 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-800">
          Read-only (GitOps-enforced). Clusters are managed declaratively from Git; mutating
          actions are disabled.
        </div>
      )}

      {error && (
        <div className="mb-4 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-800">
          {error}
        </div>
      )}

      {loading ? (
        <p>Loading…</p>
      ) : (
        <ClusterTable
          clusters={clusters}
          readOnly={readOnly}
          onChanged={refresh}
          onError={setError}
        />
      )}

      {!readOnly && !loading && <CreateForm onCreated={refresh} onError={setError} />}
    </div>
  );
}

function ClusterTable(props: {
  clusters: Cluster[];
  readOnly: boolean;
  onChanged: () => Promise<void>;
  onError: (message: string) => void;
}) {
  const { clusters, readOnly, onChanged, onError } = props;

  if (clusters.length === 0) {
    return <p className="text-slate-500">No clusters.</p>;
  }

  return (
    <table className="w-full border-collapse text-sm">
      <thead>
        <tr className="border-b text-left text-slate-500">
          <th className="py-2">Name</th>
          <th>Namespace</th>
          <th>Distribution</th>
          <th>Phase</th>
          <th>Endpoint</th>
          {!readOnly && <th></th>}
        </tr>
      </thead>
      <tbody>
        {clusters.map((cluster) => (
          <tr key={`${cluster.metadata.namespace}/${cluster.metadata.name}`} className="border-b">
            <td className="py-2 font-medium">{cluster.metadata.name}</td>
            <td>{cluster.metadata.namespace ?? "default"}</td>
            <td>{cluster.spec?.cluster?.distribution ?? "—"}</td>
            <td>{cluster.status?.phase ?? "—"}</td>
            <td className="font-mono text-xs">{cluster.status?.endpoint ?? "—"}</td>
            {!readOnly && (
              <td className="text-right">
                <DeleteButton cluster={cluster} onDeleted={onChanged} onError={onError} />
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function DeleteButton(props: {
  cluster: Cluster;
  onDeleted: () => Promise<void>;
  onError: (message: string) => void;
}) {
  const { cluster, onDeleted, onError } = props;

  async function handleDelete() {
    try {
      await deleteCluster(cluster.metadata.namespace ?? "default", cluster.metadata.name);
      await onDeleted();
    } catch (err) {
      onError(String(err));
    }
  }

  return (
    <button
      type="button"
      onClick={() => void handleDelete()}
      className="rounded bg-red-100 px-2 py-1 text-xs text-red-700 hover:bg-red-200"
    >
      Delete
    </button>
  );
}

function CreateForm(props: { onCreated: () => Promise<void>; onError: (message: string) => void }) {
  const { onCreated, onError } = props;
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [distribution, setDistribution] = useState(DISTRIBUTIONS[0]);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() === "") {
      return;
    }

    try {
      await createCluster({
        metadata: { name, namespace },
        spec: { cluster: { distribution } },
      });
      setName("");
      await onCreated();
    } catch (err) {
      onError(String(err));
    }
  }

  return (
    <form onSubmit={(event) => void handleSubmit(event)} className="mt-6 flex flex-wrap items-end gap-3">
      <label className="flex flex-col text-sm">
        Name
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          className="rounded border px-2 py-1"
          placeholder="my-cluster"
        />
      </label>
      <label className="flex flex-col text-sm">
        Namespace
        <input
          value={namespace}
          onChange={(event) => setNamespace(event.target.value)}
          className="rounded border px-2 py-1"
        />
      </label>
      <label className="flex flex-col text-sm">
        Distribution
        <select
          value={distribution}
          onChange={(event) => setDistribution(event.target.value)}
          className="rounded border px-2 py-1"
        >
          {DISTRIBUTIONS.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
      </label>
      <button type="submit" className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700">
        Create
      </button>
    </form>
  );
}
