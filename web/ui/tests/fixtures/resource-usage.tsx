import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ResourceUsagePanel } from "../../src/components/ResourceUsage.tsx";
import type { ClusterUsage, PodConsumption, ResourceTotals } from "../../src/lib/usage.ts";
import "../../src/index.css";

const cpu: ResourceTotals = {
  capacity: 4,
  allocatable: 3.5,
  requests: 1,
  limits: 2,
  usage: 1.25,
};

const memory: ResourceTotals = {
  capacity: 8 * 1024 ** 3,
  allocatable: 7 * 1024 ** 3,
  requests: 2 * 1024 ** 3,
  limits: 4 * 1024 ** 3,
  usage: 3 * 1024 ** 3,
};

const usage: ClusterUsage = {
  nodes: [
    {
      name: "worker-0",
      controlPlane: false,
      ready: true,
      cpu,
      memory,
      pods: { allocatable: 110, count: 2 },
    },
  ],
  cpu,
  memory,
  pods: { allocatable: 110, count: 2 },
  metricsAvailable: true,
};

const longIdentifier =
  "observability-system/metrics-exporter-with-an-intentionally-long-unbroken-generated-pod-name-7f9c8d6b5f-qwert";
const [namespace, name] = longIdentifier.split("/");
const topPods: PodConsumption[] = [{ namespace, name, cpu: 0.95, memory: 900 * 1024 ** 2 }];

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <main data-testid="resource-usage-fixture" className="p-4">
      <ResourceUsagePanel usage={usage} topPods={topPods} loading={false} />
    </main>
  </StrictMode>,
);
