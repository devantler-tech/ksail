import { useEffect, useRef, useState } from "react";
import { logsEventSourceURL } from "../api.ts";

// MAX_LINES caps the rendered buffer so a long-running follow can't grow memory without bound; older
// lines scroll off (kubectl-logs-style tail).
const MAX_LINES = 5000;

type StreamStatus = "streaming" | "ended" | "error";

// LogViewer streams a pod container's logs over SSE and renders them in a scrollable, auto-following
// pane. It reconnects to a fresh stream whenever the target (cluster, pod, container) changes.
export function LogViewer({
  clusterNamespace,
  clusterName,
  podNamespace,
  pod,
  container,
}: {
  clusterNamespace: string;
  clusterName: string;
  podNamespace: string;
  pod: string;
  container: string;
}) {
  const [lines, setLines] = useState<string[]>([]);
  const [status, setStatus] = useState<StreamStatus>("streaming");
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setLines([]);
    setStatus("streaming");

    const source = new EventSource(logsEventSourceURL(clusterNamespace, clusterName, podNamespace, pod, container));

    source.addEventListener("log", (event: MessageEvent<string>) => {
      setLines((previous) => {
        const next = [...previous, event.data];

        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
      });
    });
    source.addEventListener("eof", () => {
      setStatus("ended");
      source.close();
    });
    // A connection-level error (drop, or the stream closed): stop here rather than let EventSource
    // reconnect and re-stream the tail (which would duplicate lines). The user can reopen to retry.
    source.onerror = () => {
      setStatus((current) => (current === "streaming" ? "error" : current));
      source.close();
    };

    return () => source.close();
  }, [clusterNamespace, clusterName, podNamespace, pod, container]);

  // Auto-scroll to the newest line as lines arrive.
  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [lines]);

  const statusLabel = status === "streaming" ? "Streaming…" : status === "ended" ? "Stream ended" : "Disconnected";

  return (
    <div className="space-y-2">
      <pre className="h-[60vh] w-full overflow-auto rounded-lg bg-slate-900 p-3 font-mono text-xs leading-relaxed text-slate-100">
        {lines.length === 0 ? <span className="text-slate-500">No log output yet…</span> : lines.join("\n")}
        <div ref={endRef} />
      </pre>
      <p className="text-xs text-slate-500 dark:text-slate-400">{statusLabel}</p>
    </div>
  );
}
