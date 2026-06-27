import { cx } from "../lib/cx.ts";
import { useTimeFormatters } from "../hooks/usePreferences.tsx";
import type { EventFields } from "../lib/k8s.ts";

// EventTypeBadge colours a Kubernetes event by its type: Warning stands out (amber), Normal is muted.
export function EventTypeBadge({ type }: { type: string }) {
  const warning = type === "Warning";

  return (
    <span
      className={cx(
        "inline-flex shrink-0 items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        warning
          ? "bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30"
          : "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
      )}
    >
      {type || "Normal"}
    </span>
  );
}

// EventRow is the compact representation of a single event (type badge · reason · target · age · note),
// shared by the Overview "recent warnings" panel and the resource detail's "related events" section.
function EventRow({ event }: { event: EventFields }) {
  const { format } = useTimeFormatters();

  return (
    <li className="flex items-start gap-2 text-sm">
      <EventTypeBadge type={event.type} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2">
          <span className="font-medium text-slate-700 dark:text-slate-200">{event.reason || "Event"}</span>
          {event.objectName ? (
            <span className="truncate text-xs text-slate-500 dark:text-slate-400">
              {event.objectKind ? `${event.objectKind}/` : ""}
              {event.objectName}
            </span>
          ) : null}
          <span className="ml-auto shrink-0 text-xs tabular-nums text-slate-400">{format(event.lastSeen)}</span>
        </div>
        {event.message ? (
          <p className="break-words text-xs text-slate-500 dark:text-slate-400" title={event.message}>
            {event.message}
          </p>
        ) : null}
      </div>
    </li>
  );
}

// EventList renders a vertical list of EventRow. Callers handle their own empty/loading states.
export function EventList({ events }: { events: EventFields[] }) {
  return (
    <ul className="space-y-2.5">
      {events.map((event, index) => (
        <EventRow key={`${event.objectKind}-${event.objectName}-${event.reason}-${index}`} event={event} />
      ))}
    </ul>
  );
}
