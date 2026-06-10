import { ArrowDown } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { logsEventSourceURL } from "../api.ts";

// MAX_LINES caps the rendered buffer so a long-running follow can't grow memory without bound; older
// lines scroll off (kubectl-logs-style tail).
const MAX_LINES = 5000;

// FOLLOW_THRESHOLD_PX is how close to the bottom (in px) the pane must be to count as "following".
// Slightly generous so sub-line rounding or a trailing partial scroll doesn't silently stop the tail.
const FOLLOW_THRESHOLD_PX = 40;

type StreamStatus = "streaming" | "ended" | "error";

// LogViewer streams a pod container's logs over SSE and renders them in a scrollable pane that
// follows the tail. Scrolling up pauses the follow so scrollback can be read without the next line
// yanking the view back down; scrolling to the bottom (or the Follow button) resumes it. It
// reconnects to a fresh stream whenever the target (cluster, pod, container) changes.
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
  // following mirrors "is the pane scrolled to the bottom"; it drives both the auto-scroll and the
  // Follow affordance shown while paused.
  const [following, setFollowing] = useState(true);
  const paneRef = useRef<HTMLPreElement>(null);

  useEffect(() => {
    setLines([]);
    setStatus("streaming");
    setFollowing(true);

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

  // Keep the tail in view as lines arrive — but only while the user is at the bottom.
  useEffect(() => {
    const pane = paneRef.current;
    if (following && pane) {
      pane.scrollTop = pane.scrollHeight;
    }
  }, [lines, following]);

  // handleScroll keeps `following` in sync with where the user scrolled the pane.
  function handleScroll() {
    const pane = paneRef.current;
    if (!pane) {
      return;
    }

    setFollowing(pane.scrollHeight - pane.scrollTop - pane.clientHeight < FOLLOW_THRESHOLD_PX);
  }

  function resumeFollow() {
    const pane = paneRef.current;
    if (pane) {
      pane.scrollTop = pane.scrollHeight;
    }
    setFollowing(true);
  }

  const statusLabel = status === "streaming" ? "Streaming…" : status === "ended" ? "Stream ended" : "Disconnected";

  return (
    <div className="space-y-2">
      <div className="relative">
        <pre
          ref={paneRef}
          onScroll={handleScroll}
          className="h-[60vh] w-full overflow-auto overscroll-contain rounded-lg bg-slate-900 p-3 font-mono text-xs leading-relaxed text-slate-100"
        >
          {lines.length === 0 ? <span className="text-slate-500">No log output yet…</span> : lines.join("\n")}
        </pre>
        {!following && status === "streaming" ? (
          <button
            type="button"
            onClick={resumeFollow}
            className="absolute bottom-3 right-3 inline-flex items-center gap-1.5 rounded-full bg-slate-700/90 px-3 py-1.5 text-xs font-medium text-white shadow-lg transition-colors hover:bg-slate-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500"
          >
            <ArrowDown className="size-3.5" aria-hidden />
            Follow
          </button>
        ) : null}
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400" role="status">
        {statusLabel}
      </p>
    </div>
  );
}
