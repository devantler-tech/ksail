import { useEffect, useRef, useState } from "react";
import { applyManifests, errorMessage, type ApplyResult } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { useToast } from "./Toast.tsx";
import { Button, SlideOver } from "./ui.tsx";

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
  // busy tracks WHICH action is running (null = idle) so only the clicked button shows its spinner
  // while both stay disabled.
  const [busy, setBusy] = useState<"validate" | "apply" | null>(null);
  // A hidden file input drives the "Load from file…" button. In the desktop webview (WKWebView /
  // WebView2 / WebKitGTK) this opens the OS-native file picker; in a browser it opens the browser's —
  // so manifests can be loaded from disk on every surface with no Wails-specific binding.
  const fileInputRef = useRef<HTMLInputElement>(null);

  // The dialog is mounted permanently (only `open` toggles visibility), so clear prior results when
  // it (re)opens or the target cluster changes — otherwise a previous cluster's results could render
  // under a different cluster's header after close → switch → reopen.
  useEffect(() => {
    setResults(null);
    setLastDryRun(false);
  }, [open, clusterNamespace, clusterName]);

  // loadFromFiles reads the picked file(s) and appends them to the editor as a multi-document stream
  // (joined with the YAML "---" separator), so a whole directory of manifests can be staged at once and
  // combined with anything already pasted. Empty files are skipped; a read failure surfaces as a toast.
  async function loadFromFiles(files: FileList) {
    try {
      const contents = await Promise.all(Array.from(files).map((file) => file.text()));
      const loaded = contents.map((text) => text.trim()).filter(Boolean).join("\n---\n");
      if (loaded === "") {
        return;
      }

      setManifests((current) =>
        current.trim() === "" ? loaded : `${current.trimEnd()}\n---\n${loaded}`,
      );
      setResults(null);
      toast.success(`Loaded ${files.length} file(s)`);
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  function run(dryRun: boolean) {
    if (manifests.trim() === "") {
      toast.error("Add one or more Kubernetes manifests first (paste or load a file)");

      return;
    }

    setBusy(dryRun ? "validate" : "apply");
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
      .finally(() => setBusy(null));
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
          aria-label="Kubernetes manifests (multi-document YAML)"
          rows={16}
          className="w-full rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100"
        />
        <input
          ref={fileInputRef}
          type="file"
          accept=".yaml,.yml,.json,.txt"
          multiple
          className="hidden"
          onChange={(event) => {
            const { files } = event.target;
            if (files && files.length > 0) {
              void loadFromFiles(files);
            }
            // Reset so picking the same file again still fires onChange.
            event.target.value = "";
          }}
        />
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={() => fileInputRef.current?.click()} disabled={busy !== null}>
            Load from file…
          </Button>
          <Button
            variant="secondary"
            onClick={() => run(true)}
            loading={busy === "validate"}
            disabled={busy !== null}
          >
            Validate (dry run)
          </Button>
          <Button onClick={() => run(false)} loading={busy === "apply"} disabled={busy !== null}>
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
