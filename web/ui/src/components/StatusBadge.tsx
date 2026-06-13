import {
  Circle,
  CircleAlert,
  CircleCheck,
  CirclePause,
  Clock,
  House,
  LoaderCircle,
  type LucideIcon,
} from "lucide-react";
import { cx } from "../lib/cx.ts";

// StatusTone is the shared palette for status dots: ok (green), warn (amber), error (red),
// info (blue), muted (grey).
export type StatusTone = "ok" | "warn" | "error" | "info" | "muted";

const DOT_TONES: Record<StatusTone, string> = {
  ok: "bg-emerald-500",
  warn: "bg-amber-500",
  error: "bg-red-500",
  info: "bg-blue-500",
  muted: "bg-slate-400",
};

// StatusDot is the small coloured status dot used in tables, legends, and the cluster switcher.
// It is decorative (aria-hidden) unless a title is given, in which case the tooltip carries the
// meaning.
export function StatusDot({
  tone,
  size = "sm",
  className,
  title,
}: {
  tone: StatusTone;
  size?: "sm" | "md";
  className?: string;
  title?: string;
}) {
  return (
    <span
      className={cx("rounded-full", size === "md" ? "size-2" : "size-1.5", DOT_TONES[tone], className)}
      title={title}
      aria-hidden={title ? undefined : true}
    />
  );
}

type PhaseMeta = {
  label: string;
  icon: LucideIcon;
  spin: boolean;
  badge: string;
  dot: StatusTone;
};

const FALLBACK: PhaseMeta = {
  label: "Unknown",
  icon: Circle,
  spin: false,
  badge: "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
  dot: "muted",
};

const PHASES: Record<string, PhaseMeta> = {
  Ready: {
    label: "Ready",
    icon: CircleCheck,
    spin: false,
    badge:
      "bg-emerald-50 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400 dark:ring-emerald-500/30",
    dot: "ok",
  },
  Provisioning: {
    label: "Provisioning",
    icon: LoaderCircle,
    spin: true,
    badge:
      "bg-blue-50 text-blue-700 ring-blue-600/20 dark:bg-blue-500/10 dark:text-blue-400 dark:ring-blue-500/30",
    dot: "info",
  },
  Updating: {
    label: "Updating",
    icon: LoaderCircle,
    spin: true,
    badge:
      "bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30",
    dot: "warn",
  },
  Deleting: {
    label: "Deleting",
    icon: LoaderCircle,
    spin: true,
    badge:
      "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
    dot: "muted",
  },
  Failed: {
    label: "Failed",
    icon: CircleAlert,
    spin: false,
    badge: "bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-500/10 dark:text-red-400 dark:ring-red-500/30",
    dot: "error",
  },
  Pending: {
    label: "Pending",
    icon: Clock,
    spin: false,
    badge:
      "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
    dot: "muted",
  },
  // Stopped is a display-only phase the SPA derives from a Ready=False/reason=Stopped condition (see
  // clusterPhase in lib/k8s.ts) for a cluster whose infrastructure exists but is not running. It is a
  // deliberate state, not an error, so it uses the muted (grey) tone — distinct from a green "Ready".
  Stopped: {
    label: "Stopped",
    icon: CirclePause,
    spin: false,
    badge:
      "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
    dot: "muted",
  },
};

export function phaseMeta(phase?: string): PhaseMeta {
  if (!phase) {
    return FALLBACK;
  }

  return PHASES[phase] ?? { ...FALLBACK, label: phase };
}

// HostBadge marks the operator's self-registered host cluster — the cluster the operator runs on
// (see HOST_CLUSTER_LABEL in lib/k8s.ts). Rendered next to the cluster name wherever lifecycle
// actions are hidden for it, so the missing edit/delete affordances are self-explanatory.
export function HostBadge() {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full bg-sky-50 px-2 py-0.5 text-xs font-medium text-sky-700 ring-1 ring-inset ring-sky-600/20 dark:bg-sky-500/10 dark:text-sky-400 dark:ring-sky-500/30"
      title="The cluster the KSail operator runs on"
    >
      <House className="size-3.5" aria-hidden />
      Host
    </span>
  );
}

export function StatusBadge({ phase }: { phase?: string }) {
  const meta = phaseMeta(phase);
  const Icon = meta.icon;

  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        meta.badge,
      )}
    >
      <Icon className={cx("size-3.5", meta.spin && "animate-spin")} aria-hidden />
      {meta.label}
    </span>
  );
}
