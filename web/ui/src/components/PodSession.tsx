import { lazy, Suspense, type ReactNode } from "react";
import type { K8sObject } from "../api.ts";
import { podContainers } from "../lib/resources.ts";
import { LogViewer } from "./LogViewer.tsx";
import { SelectField, SlideOver } from "./ui.tsx";

// ExecTerminal pulls in xterm.js (~250 kB), so it is code-split: the chunk loads only when a terminal
// is actually opened, keeping it out of the initial bundle.
const ExecTerminal = lazy(() => import("./ExecTerminal.tsx").then((module) => ({ default: module.ExecTerminal })));

// ContainerPicker renders a container selector for a multi-container Pod (nothing for single-container
// Pods). Shared by the Logs and Exec slide-overs so the picker markup lives in one place.
export function ContainerPicker({
  pod,
  value,
  onChange,
}: {
  pod: K8sObject;
  value: string;
  onChange: (value: string) => void;
}) {
  const containers = podContainers(pod);
  if (containers.length <= 1) {
    return null;
  }

  return (
    <SelectField label="Container" value={value} onChange={(event) => onChange(event.target.value)}>
      {containers.map((name) => (
        <option key={name} value={name}>
          {name}
        </option>
      ))}
    </SelectField>
  );
}

// PodSlideOver is the shared shell for the Logs and Exec panels: a SlideOver titled "<verb> · <pod>"
// with the container picker above the verb-specific body. Keeps the two siblings free of duplicated
// SlideOver/picker markup (jscpd threshold is 0).
function PodSlideOver({
  verb,
  pod,
  container,
  onContainerChange,
  onClose,
  children,
}: {
  verb: string;
  pod: K8sObject | null;
  container: string;
  onContainerChange: (value: string) => void;
  onClose: () => void;
  children: (pod: K8sObject) => ReactNode;
}) {
  return (
    <SlideOver
      open={pod !== null}
      onClose={onClose}
      title={`${verb} · ${pod?.metadata?.name ?? ""}`}
      subtitle={pod?.metadata?.namespace ? `namespace: ${pod.metadata.namespace}` : ""}
    >
      {pod ? (
        <div className="space-y-3">
          <ContainerPicker pod={pod} value={container} onChange={onContainerChange} />
          {children(pod)}
        </div>
      ) : null}
    </SlideOver>
  );
}

// PodTarget is the cluster coordinates the log/exec streams address (the active cluster's
// namespace/name); the pod's own namespace/name come from the selected Pod object.
interface PodTarget {
  clusterNamespace: string;
  clusterName: string;
}

// PodPanelProps is the shared prop shape of the Logs and Exec slide-over panels (selected Pod,
// chosen container + change handler, close handler, and the cluster coordinates the stream
// addresses). Shared so the two siblings don't duplicate the prop type (jscpd threshold is 0).
interface PodPanelProps {
  pod: K8sObject | null;
  container: string;
  onContainerChange: (value: string) => void;
  onClose: () => void;
  target: PodTarget;
}

// PodLogs is the Logs slide-over: streams the chosen container's logs for the targeted Pod.
export function PodLogs({ pod, container, onContainerChange, onClose, target }: PodPanelProps) {
  return (
    <PodSlideOver verb="Logs" pod={pod} container={container} onContainerChange={onContainerChange} onClose={onClose}>
      {(activePod) => (
        <LogViewer
          clusterNamespace={target.clusterNamespace}
          clusterName={target.clusterName}
          podNamespace={activePod.metadata?.namespace ?? ""}
          pod={activePod.metadata?.name ?? ""}
          container={container}
        />
      )}
    </PodSlideOver>
  );
}

// PodExec is the Exec slide-over: opens an in-browser terminal into the chosen container of the
// targeted Pod. The xterm-backed terminal loads lazily (see ExecTerminal).
export function PodExec({ pod, container, onContainerChange, onClose, target }: PodPanelProps) {
  return (
    <PodSlideOver verb="Exec" pod={pod} container={container} onContainerChange={onContainerChange} onClose={onClose}>
      {(activePod) => (
        <Suspense fallback={<div className="text-sm text-slate-500 dark:text-slate-400">Loading terminal…</div>}>
          <ExecTerminal
            clusterNamespace={target.clusterNamespace}
            clusterName={target.clusterName}
            podNamespace={activePod.metadata?.namespace ?? ""}
            pod={activePod.metadata?.name ?? ""}
            container={container}
          />
        </Suspense>
      )}
    </PodSlideOver>
  );
}
