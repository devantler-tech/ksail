import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import {
  ApiError,
  createCluster,
  deleteCluster,
  getConfig,
  listClusters,
  loginPath,
  logout,
  type Cluster,
  type User,
} from "./api.ts";

// Fallback create-form options used when the backend does not advertise its supported distributions
// via config.distributions. The operator omits it and only provisions VCluster in-cluster; the local
// `ksail cluster ui` backend advertises the locally creatable set.
const DEFAULT_DISTRIBUTIONS = ["VCluster"];

export function App() {
  const [readOnly, setReadOnly] = useState(true);
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<User | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [distributions, setDistributions] = useState<string[]>(DEFAULT_DISTRIBUTIONS);
  // Guards against state updates from in-flight requests that resolve after the component unmounts
  // (refresh runs from the interval, the button, and child callbacks, not only the init effect).
  const mounted = useRef(true);
  // Holds the polling interval so it can be stopped from refresh() on a 401 without unmounting.
  const pollRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined);

  const refresh = useCallback(async () => {
    try {
      const list = await listClusters();
      if (!mounted.current) {
        return;
      }
      setClusters(list.items ?? []);
      setError(null);
    } catch (err) {
      if (!mounted.current) {
        return;
      }
      if (err instanceof ApiError && err.status === 401) {
        // Stop polling once the session is gone; otherwise the interval keeps firing
        // unauthorized requests every 10s while the login screen is shown.
        if (pollRef.current) {
          clearInterval(pollRef.current);
          pollRef.current = undefined;
        }
        setNeedsLogin(true);
        return;
      }
      setError(String(err));
    }
  }, []);

  useEffect(() => {
    mounted.current = true;

    async function init() {
      let config;
      try {
        config = await getConfig();
      } catch (err) {
        if (mounted.current) {
          setError(String(err));
          setLoading(false);
        }
        return;
      }

      if (!mounted.current) {
        return;
      }

      setReadOnly(config.readOnly);
      setUser(config.user ?? null);
      setDistributions(config.distributions ?? DEFAULT_DISTRIBUTIONS);

      // When auth is enabled but no session exists yet, show the login screen and do not poll.
      if (config.authEnabled && !config.user) {
        setNeedsLogin(true);
        setLoading(false);
        return;
      }

      await refresh();
      if (mounted.current) {
        setLoading(false);
        pollRef.current = setInterval(() => void refresh(), 10000);
      }
    }

    void init();

    return () => {
      mounted.current = false;
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = undefined;
      }
    };
  }, [refresh]);

  if (needsLogin) {
    return <LoginScreen />;
  }

  return (
    <div className="mx-auto max-w-5xl p-6 font-sans text-slate-800">
      <header className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">KSail Clusters</h1>
        <div className="flex items-center gap-3">
          {user && (
            <span className="text-sm text-slate-500">{user.email ?? user.name ?? user.subject}</span>
          )}
          <button
            type="button"
            onClick={() => void refresh()}
            className="rounded bg-slate-200 px-3 py-1 text-sm hover:bg-slate-300"
          >
            Refresh
          </button>
          {user && (
            <button
              type="button"
              onClick={() => {
                void logout().finally(() => setNeedsLogin(true));
              }}
              className="rounded bg-slate-200 px-3 py-1 text-sm hover:bg-slate-300"
            >
              Logout
            </button>
          )}
        </div>
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

      {!readOnly && !loading && (
        <CreateForm distributions={distributions} onCreated={refresh} onError={setError} />
      )}
    </div>
  );
}

function LoginScreen() {
  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-4 p-6 font-sans text-slate-800">
      <h1 className="text-2xl font-bold">KSail</h1>
      <p className="text-sm text-slate-500">Sign in to manage your clusters.</p>
      <a
        href={loginPath}
        className="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700"
      >
        Login
      </a>
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

function CreateForm(props: {
  distributions: string[];
  onCreated: () => Promise<void>;
  onError: (message: string) => void;
}) {
  const { distributions, onCreated, onError } = props;
  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [distribution, setDistribution] = useState(distributions[0]);

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
          {distributions.map((value) => (
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
