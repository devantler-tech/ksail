// Kubernetes resource-quantity parsing and formatting (CPU in cores, memory in bytes), used by the
// Overview's resource-usage monitoring. Mirrors the subset of Kubernetes quantity syntax that node
// capacity/allocatable, pod requests/limits, and metrics.k8s.io values actually use.

// QUANTITY_SCALE maps a quantity suffix to its multiplier relative to the base unit (cores for CPU,
// bytes for memory). Decimal SI suffixes scale by powers of 1000; binary suffixes by powers of 1024.
const QUANTITY_SCALE: Record<string, number> = {
  "": 1,
  n: 1e-9,
  u: 1e-6,
  m: 1e-3,
  k: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
  Ki: 1024 ** 1,
  Mi: 1024 ** 2,
  Gi: 1024 ** 3,
  Ti: 1024 ** 4,
  Pi: 1024 ** 5,
  Ei: 1024 ** 6,
};

// Two-letter binary suffixes are listed before their one-letter SI prefixes so "Mi" never half-matches
// as "M" with a dangling "i".
const QUANTITY_PATTERN = /^([+-]?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)(Ki|Mi|Gi|Ti|Pi|Ei|n|u|m|k|M|G|T|P|E)?$/;

// parseQuantity parses a Kubernetes resource quantity ("100m", "1536Mi", "2", "16467492Ki") into a
// number in its base unit, or undefined when absent or unparseable. Values come from unstructured
// JSON, so any input type is tolerated.
export function parseQuantity(value: unknown): number | undefined {
  if (typeof value === "number") {
    return Number.isFinite(value) ? value : undefined;
  }
  if (typeof value !== "string") {
    return undefined;
  }

  const match = QUANTITY_PATTERN.exec(value.trim());
  if (!match) {
    return undefined;
  }

  return Number(match[1]) * QUANTITY_SCALE[match[2] ?? ""];
}

// formatCores renders a CPU quantity in cores: millicores below one core ("250m"), otherwise a
// compact decimal ("3.4", "12").
export function formatCores(cores: number): string {
  if (cores < 1) {
    return `${Math.round(cores * 1000)}m`;
  }

  return cores >= 10 ? `${Math.round(cores)}` : `${Number(cores.toFixed(1))}`;
}

// formatBytes renders a byte quantity with a binary unit ("512 MiB", "12.4 GiB").
export function formatBytes(bytes: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let value = bytes;
  let unit = 0;

  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }

  const digits = value > 0 && value < 100 && !Number.isInteger(value) ? 1 : 0;

  return `${value.toFixed(digits)} ${units[unit]}`;
}

// percentOf returns part as a 0–100 percentage of whole (clamped), or undefined when the whole is
// unknown or zero — callers render "—" instead of a meaningless 0%.
export function percentOf(part: number | undefined, whole: number): number | undefined {
  if (part === undefined || whole <= 0) {
    return undefined;
  }

  return Math.min(100, Math.max(0, (part / whole) * 100));
}
