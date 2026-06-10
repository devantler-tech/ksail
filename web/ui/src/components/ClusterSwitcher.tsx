import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { Check, ChevronsUpDown } from "lucide-react";
import type { Cluster } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { clusterKey } from "../lib/k8s.ts";
import { phaseMeta } from "./StatusBadge.tsx";

// Dot is a small status-coloured dot derived from a cluster's phase (reuses the StatusBadge palette).
function Dot({ phase }: { phase?: string }) {
  return <span className={cx("size-2 shrink-0 rounded-full", phaseMeta(phase).dot)} aria-hidden />;
}

// ClusterSwitcher is the single control for choosing the active cluster: a card showing the current
// cluster (status dot · name · distribution·provider) with a dropdown of all clusters to switch to.
// Replaces the per-view cluster <select> dropdowns — cluster selection now lives in exactly one place.
export function ClusterSwitcher({
  clusters,
  activeKey,
  onSelect,
}: {
  clusters: Cluster[];
  activeKey: string;
  onSelect: (key: string) => void;
}) {
  const active = clusters.find((cluster) => clusterKey(cluster) === activeKey) ?? null;
  if (!active) {
    return null;
  }

  const spec = active.spec?.cluster;

  return (
    <Menu as="div" className="relative">
      <MenuButton className="flex w-full items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-left transition-colors hover:bg-slate-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 dark:border-slate-700 dark:bg-slate-800/60 dark:hover:bg-slate-800">
        <Dot phase={active.status?.phase} />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-semibold text-slate-900 dark:text-white">{active.metadata.name}</span>
          <span className="block truncate text-xs text-slate-500 dark:text-slate-400">
            {spec?.distribution ?? "—"}
            {spec?.provider ? ` · ${spec.provider}` : ""}
          </span>
        </span>
        <ChevronsUpDown className="size-4 shrink-0 text-slate-400" aria-hidden />
      </MenuButton>
      <MenuItems
        transition
        className="absolute z-30 mt-1 w-full origin-top overflow-hidden rounded-lg border border-slate-200 bg-white shadow-lg transition duration-100 ease-out focus:outline-none data-[closed]:scale-95 data-[closed]:opacity-0 dark:border-slate-700 dark:bg-slate-800"
      >
        <div className="max-h-72 overflow-y-auto overscroll-contain p-1">
          {clusters.map((cluster) => {
            const key = clusterKey(cluster);

            return (
              <MenuItem key={key}>
                {({ focus }) => (
                  <button
                    type="button"
                    onClick={() => onSelect(key)}
                    className={cx(
                      "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm",
                      focus ? "bg-slate-100 dark:bg-slate-700/60" : "",
                    )}
                  >
                    <Dot phase={cluster.status?.phase} />
                    <span className="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">{cluster.metadata.name}</span>
                    {key === activeKey ? <Check className="size-4 shrink-0 text-blue-600 dark:text-blue-400" aria-hidden /> : null}
                  </button>
                )}
              </MenuItem>
            );
          })}
        </div>
      </MenuItems>
    </Menu>
  );
}
