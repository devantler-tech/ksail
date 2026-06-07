import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";
import { execWebSocketURL } from "../api.ts";

// ExecTerminal renders an xterm.js terminal wired to the backend exec WebSocket for one pod container.
// The session reconnects (the effect re-runs) whenever the target — cluster, pod, or container —
// changes. Frames use the backend's JSON protocol: client {op:"stdin"|"resize"}, server {op:"stdout"|
// "error"|"exit"}.
export function ExecTerminal({
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
  const hostRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) {
      return undefined;
    }

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      theme: { background: "#0f172a" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);

    const socket = new WebSocket(
      execWebSocketURL(clusterNamespace, clusterName, podNamespace, pod, container),
    );

    const sendResize = () => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ op: "resize", cols: term.cols, rows: term.rows }));
      }
    };

    socket.onopen = () => {
      term.focus();
      sendResize();
    };
    socket.onmessage = (event) => {
      const message = JSON.parse(event.data as string) as { op: string; data?: string };
      if (message.op === "stdout") {
        term.write(message.data ?? "");
      } else if (message.op === "error") {
        term.write(`\r\n\x1b[31m${message.data ?? "error"}\x1b[0m\r\n`);
      } else if (message.op === "exit") {
        term.write("\r\n\x1b[2m[session ended]\x1b[0m\r\n");
      }
    };
    socket.onclose = () => term.write("\r\n\x1b[2m[disconnected]\x1b[0m\r\n");

    const dataSub = term.onData((data) => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ op: "stdin", data }));
      }
    });
    const resizeSub = term.onResize(sendResize);

    // Fit to the container on initial layout (the slide-over animates open) and on any size change.
    const observer = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // The element is not laid out yet (e.g. mid-animation); the next observation will fit.
      }
    });
    observer.observe(host);

    return () => {
      observer.disconnect();
      dataSub.dispose();
      resizeSub.dispose();
      socket.close();
      term.dispose();
    };
  }, [clusterNamespace, clusterName, podNamespace, pod, container]);

  return <div ref={hostRef} className="h-[60vh] w-full overflow-hidden rounded-lg bg-slate-900 p-2" />;
}
