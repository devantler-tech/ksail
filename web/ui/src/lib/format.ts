// epochMs parses an ISO timestamp to epoch milliseconds, returning 0 when absent or invalid. Used for
// time-based sorting (newest/oldest) where an unparseable value should sort as the epoch.
export function epochMs(iso?: string): number {
  const ms = iso ? new Date(iso).getTime() : 0;

  return Number.isNaN(ms) ? 0 : ms;
}

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

// AbsoluteOptions selects how formatAbsolute renders a timestamp. `dateStyle` picks the
// locale-native string ("locale") vs. an ISO-like "YYYY-MM-DD HH:MM:SS" ("iso"); `timeZone`
// renders in the viewer's local zone ("local") or UTC ("utc"). Both are structurally the same
// string-literal unions the preferences store exposes, so callers can pass prefs directly.
export interface AbsoluteOptions {
  dateStyle?: "locale" | "iso";
  timeZone?: "local" | "utc";
}

// formatAbsolute renders an ISO timestamp as an absolute date-time string in the requested style
// and zone, or an em dash when absent/invalid. The "iso" style borrows the sv-SE locale, which
// formats as ISO-ordered "YYYY-MM-DD HH:MM:SS" across browsers without manual field assembly.
export function formatAbsolute(iso?: string, opts?: AbsoluteOptions): string {
  if (!iso) {
    return "—";
  }

  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return "—";
  }

  const utc = opts?.timeZone === "utc";
  const tzOpt: Intl.DateTimeFormatOptions | undefined = utc ? { timeZone: "UTC" } : undefined;
  const locale = opts?.dateStyle === "iso" ? "sv-SE" : undefined;
  const rendered = date.toLocaleString(locale, tzOpt);

  return utc ? `${rendered} UTC` : rendered;
}

// formatTimestamp renders an ISO timestamp in the viewer's locale and local zone, or an em dash
// when absent. Kept as the default-options shorthand over formatAbsolute.
export function formatTimestamp(iso?: string): string {
  return formatAbsolute(iso, { dateStyle: "locale", timeZone: "local" });
}
