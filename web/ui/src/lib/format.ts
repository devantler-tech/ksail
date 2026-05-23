// relativeAge renders a compact "time since" label (e.g. "5m", "3h", "2d") from an ISO timestamp.
export function relativeAge(iso?: string): string {
  if (!iso) {
    return "—";
  }

  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return "—";
  }

  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m`;
  }

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours}h`;
  }

  const days = Math.floor(hours / 24);
  return `${days}d`;
}

// formatTimestamp renders an ISO timestamp in the user's locale, or an em dash when absent.
export function formatTimestamp(iso?: string): string {
  if (!iso) {
    return "—";
  }

  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
}
