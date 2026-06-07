import { useState } from "react";
import { ApiError, applyManifests, type ApplyResult } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { useToast } from "./Toast.tsx";
import { Button, SlideOver } from "./ui.tsx";

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }

  return err instanceof Error ? err.message : String(err);
}

const PLACEHOLDER =
  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n  namespace: default\ndata:\n  key: value";

// ApplyManifestsDialog is a slide-over for server-side-applying raw multi-document YAML to a cluster.
// Validate runs a server-side dry-run; Apply persists. Per-document results are listed so a partial
// failure is visible. Shown only when the backend advertises applyManifests and the UI is writable.
export function ApplyManifestsDialog({
  open,
  onClose,
  clusterNamespace,
  clusterName,
  onApplied,
}: {
  open: boolean;
  onClose: () => void;
  clusterNamespace: string;
  clusterName: string;
  // onApplied fires after a successful (non-dry-run) apply so the caller can refresh its view.
  onApplied: () => void;
}) {
  const toast = useToast();
  const [manifests, setManifests] = useState("");
  const [results, setResults] = useState<ApplyResult[] | null>(null);
  const [lastDryRun, setLastDryRun] = useState(false);
  const [busy, setBusy] = useState(false);

  function run(dryRun: boolean) {
    if (manifests.trim() === "") {
      toast.error("Paste one or more Kubernetes manifests first");

      return;
    }

    setBusy(true);
    setResults(null);
    applyManifests(clusterNamespace, clusterName, manifests, dryRun)
      .then((response) => {
        setResults(response.results);
        setLastDryRun(dryRun);

        const failed = response.results.filter((result) => result.status === "error").length;
        if (failed > 0) {
          toast.error(`${failed} of ${response.results.length} manifest(s) failed`);

          return;
        }

        toast.success(
          dryRun ? "Validation passed" : `Applied ${response.results.length} manifest(s)`,
        );
        if (!dryRun) {
          onApplied();
        }
      })
      .catch((err: unknown) => toast.error(errorMessage(err)))
      .finally(() => setBusy(false));
  }

  return (
    <SlideOver
      open={open}
      onClose={onClose}
      title="Apply YAML"
      subtitle={`${clusterName} · server-side apply`}
    >
      <div className="space-y-3">
        <textarea
          value={manifests}
          onChange={(event) => setManifests(event.target.value)}
          placeholder={PLACEHOLDER}
          spellCheck={false}
          rows={16}
          className="w-full rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
        />
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={() => run(true)} loading={busy}>
            Validate (dry run)
          </Button>
          <Button onClick={() => run(false)} loading={busy}>
            Apply
          </Button>
        </div>
        {results ? (
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-slate-500 dark:text-slate-400">
              {lastDryRun ? "Dry-run results" : "Apply results"}
            </p>
            <ul className="space-y-1">
              {results.map((result, index) => (
                <li
                  key={`${result.kind}/${result.namespace ?? ""}/${result.name || index}`}
                  className="rounded-md border border-slate-200 px-3 py-2 text-sm dark:border-slate-800"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium text-slate-800 dark:text-slate-100">
                      {result.kind} {result.name}
                      {result.namespace ? ` · ${result.namespace}` : ""}
                    </span>
                    <span
                      className={cx(
                        "text-xs font-medium",
                        result.status === "applied"
                          ? "text-emerald-600 dark:text-emerald-400"
                          : "text-red-600 dark:text-red-400",
                      )}
                    >
                      {result.status}
                    </span>
                  </div>
                  {result.error ? (
                    <p className="mt-1 text-xs break-words text-red-600 dark:text-red-400">
                      {result.error}
                    </p>
                  ) : null}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </SlideOver>
  );
}
