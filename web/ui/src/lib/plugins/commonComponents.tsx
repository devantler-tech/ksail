// commonComponents.tsx reproduces the slice of Headlamp's `CommonComponents` module that plugins lay
// their content out with. A Headlamp plugin imports these from
// `@kinvolk/headlamp-plugin/lib/CommonComponents`, which the build maps to `pluginLib.CommonComponents`.
// KSail renders them with its own surface styling (Tailwind, not Material UI) so plugin content matches
// the host UI without pulling Material UI — e.g. the Flux Overview's donut status cards (TileChart),
// status pills (StatusLabel), and tables (Table) all render natively in KSail.

import * as React from "react";
import { pluginNavigate } from "./pluginNavigation.ts";
import { renderPluginIcon as renderIcon } from "./pluginIcon.ts";

// ---------------------------------------------------------------------------
// Section primitives
// ---------------------------------------------------------------------------

// SectionBoxHeaderProps mirrors the subset of Headlamp's SectionBox headerProps plugins pass — chiefly
// `actions`, the controls (sort/show selects, settings button) rendered on the right of the title row.
interface SectionBoxHeaderProps {
  actions?: React.ReactNode;
  noPadding?: boolean;
}

// SectionBox is Headlamp's titled content panel — the workhorse layout primitive plugins use to group
// detail content. The title is optional; headerProps.actions render on the right of the title row.
export function SectionBox({
  title,
  headerProps,
  children,
}: {
  title?: React.ReactNode;
  headerProps?: SectionBoxHeaderProps;
  children?: React.ReactNode;
}): React.ReactElement {
  const hasHeader = title !== undefined || headerProps?.actions !== undefined;

  return (
    <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      {hasHeader ? (
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</h2>
          {headerProps?.actions === undefined ? null : (
            <div className="flex items-center gap-2">{headerProps.actions}</div>
          )}
        </div>
      ) : null}
      {children}
    </section>
  );
}

// SectionHeader renders a standalone section title with optional actions, for plugins that title content
// without wrapping it in a SectionBox.
export function SectionHeader({
  title,
  actions,
}: {
  title: React.ReactNode;
  actions?: React.ReactNode[];
}): React.ReactElement {
  return (
    <div className="mb-3 flex items-center justify-between gap-3">
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</h2>
      {actions && actions.length > 0 ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

// SectionFilterHeader is Headlamp's list header with a title and (optional) action controls. KSail renders
// the title + actions; the namespace/search filter affordances are host-owned, so they are accepted but
// not rendered here.
export function SectionFilterHeader({
  title,
  actions,
}: {
  title?: React.ReactNode;
  actions?: React.ReactNode[];
  noNamespaceFilter?: boolean;
}): React.ReactElement {
  return <SectionHeader title={title} actions={actions} />;
}

// ---------------------------------------------------------------------------
// Status + labels
// ---------------------------------------------------------------------------

// StatusLabel is Headlamp's colored status pill. `status` selects the color; children are the text.
export function StatusLabel({
  status,
  children,
}: {
  status?: "success" | "warning" | "error" | "";
  children?: React.ReactNode;
}): React.ReactElement {
  const tone =
    status === "success"
      ? "bg-emerald-50 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400 dark:ring-emerald-500/30"
      : status === "warning"
        ? "bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30"
        : status === "error"
          ? "bg-red-50 text-red-700 ring-red-600/20 dark:bg-red-500/10 dark:text-red-400 dark:ring-red-500/30"
          : "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-500/30";

  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${tone}`}
    >
      {children}
    </span>
  );
}

// localeDateString / timeAgo are small date helpers shared with Utils (pluginLib). Kept here so DateLabel
// can format without importing the Utils object.
function toDate(value: unknown): Date | null {
  if (value instanceof Date) {
    return value;
  }
  if (typeof value === "string" || typeof value === "number") {
    const date = new Date(value);

    return Number.isNaN(date.getTime()) ? null : date;
  }

  return null;
}

export function localeDate(value: unknown): string {
  const date = toDate(value);

  return date ? date.toLocaleString() : "";
}

export function timeAgo(value: unknown): string {
  const date = toDate(value);
  if (!date) {
    return "";
  }

  const seconds = Math.round((Date.now() - date.getTime()) / 1000);
  const units: [number, string][] = [
    [60, "s"],
    [60, "m"],
    [24, "h"],
    [7, "d"],
    [4.345, "w"],
    [12, "mo"],
    [Number.POSITIVE_INFINITY, "y"],
  ];

  let amount = Math.max(seconds, 0);
  let unit = "s";
  for (const [size, label] of units) {
    if (amount < size) {
      unit = label;
      break;
    }
    amount = Math.floor(amount / size);
    unit = label;
  }

  return `${amount}${unit}`;
}

// DateLabel renders a relative time (with the absolute time as a tooltip), matching Headlamp's DateLabel.
export function DateLabel({ date }: { date: unknown; format?: string }): React.ReactElement {
  return (
    <span title={localeDate(date)} className="text-slate-600 dark:text-slate-300">
      {timeAgo(date)}
    </span>
  );
}

// ShowHideLabel renders text, truncated, with the full value as a tooltip (Headlamp's ShowHideLabel shows
// a show/hide toggle for long values; KSail truncates with a title attribute).
export function ShowHideLabel({ children }: { children?: React.ReactNode }): React.ReactElement {
  return <span className="block max-w-full truncate">{children}</span>;
}

// HoverInfoLabel renders a label with an info affordance whose tooltip is `hoverInfo` (Headlamp parity).
export function HoverInfoLabel({
  label,
  hoverInfo,
  icon,
}: {
  label: React.ReactNode;
  hoverInfo?: React.ReactNode;
  icon?: React.ReactNode;
}): React.ReactElement {
  return (
    <span className="inline-flex items-center gap-1" title={typeof hoverInfo === "string" ? hoverInfo : undefined}>
      {label}
      {renderIcon(icon) ?? <span aria-hidden>ⓘ</span>}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Links
// ---------------------------------------------------------------------------

// resolvePluginUrl builds the target URL for a CommonComponents.Link: an explicit `to`, or a named route
// resolved via window.pluginLib.Router.createRouteURL (the real registry-backed resolver from pluginLib).
function resolvePluginUrl(to?: string, routeName?: string, params?: Record<string, string>): string {
  if (to) {
    return to;
  }
  if (!routeName) {
    return "#";
  }

  const router = window.pluginLib?.Router as
    | { createRouteURL?: (name: string, params?: Record<string, string>) => string }
    | undefined;

  return router?.createRouteURL?.(routeName, params) ?? "#";
}

// Link is Headlamp's router link. It resolves `routeName`+`params` (or `to`) to a URL and navigates the
// plugin router on click (via pluginNavigate, so it stays inside KSail's persistent plugin MemoryRouter).
// An external http(s) URL renders as a normal new-tab anchor.
export function Link({
  to,
  routeName,
  params,
  children,
  className,
}: {
  to?: string;
  routeName?: string;
  params?: Record<string, string>;
  activeCluster?: string;
  children?: React.ReactNode;
  className?: string;
}): React.ReactElement {
  const url = resolvePluginUrl(to, routeName, params);
  const external = /^https?:\/\//.test(url) || url.startsWith("//");
  const linkClass = className ?? "text-blue-600 hover:underline dark:text-blue-400";

  if (external) {
    return (
      <a href={url} target="_blank" rel="noreferrer" className={linkClass}>
        {children}
      </a>
    );
  }

  return (
    <a
      href={url}
      className={linkClass}
      onClick={(event) => {
        event.preventDefault();
        if (url !== "#") {
          pluginNavigate(url);
        }
      }}
    >
      {children}
    </a>
  );
}

// ---------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------

// NameValueTable renders Headlamp's two-column name/value detail table. A row with `hide` is skipped.
export function NameValueTable({
  rows,
}: {
  rows: { name?: React.ReactNode; value?: React.ReactNode; hide?: boolean }[];
}): React.ReactElement {
  return (
    <table className="w-full text-sm">
      <tbody>
        {rows
          .filter((row) => !row.hide)
          .map((row, index) => (
            <tr key={index} className="border-b border-slate-100 last:border-0 dark:border-slate-800">
              <th className="w-1/3 py-1.5 pr-3 text-left align-top font-medium text-slate-500 dark:text-slate-400">
                {row.name}
              </th>
              <td className="py-1.5 align-top text-slate-700 dark:text-slate-200">{row.value}</td>
            </tr>
          ))}
      </tbody>
    </table>
  );
}

// TableColumn is the subset of a material-react-table column descriptor the Flux plugin (and similar)
// pass. `header` labels the column; the cell value comes from accessorFn(row) or accessorKey, rendered by
// `Cell` when present (called with the MRT-shaped { row.original, cell.getValue() }).
interface TableColumn {
  id?: string;
  header?: React.ReactNode;
  accessorKey?: string;
  accessorFn?: (row: unknown) => unknown;
  Cell?: (info: { row: { original: unknown }; cell: { getValue: () => unknown } }) => React.ReactNode;
}

// cellValue resolves a column's raw value for a row (accessorFn wins, else accessorKey lookup).
function cellValue(column: TableColumn, row: unknown): unknown {
  if (column.accessorFn) {
    return column.accessorFn(row);
  }
  if (column.accessorKey && row && typeof row === "object") {
    return (row as Record<string, unknown>)[column.accessorKey];
  }

  return undefined;
}

// Table is a bounded interpreter of Headlamp's material-react-table-backed Table: it renders the rows ×
// columns the plugin provides, honoring accessorFn/accessorKey + the Cell renderer. MRT-only props
// (gridTemplate, muiTableBodyCellProps, virtualization, built-in filtering) are accepted and ignored —
// enough to render the Flux resource lists natively; richer table behavior is a follow-up.
export function Table({
  columns,
  data,
  loading,
}: {
  columns: TableColumn[];
  data?: unknown[];
  loading?: boolean;
}): React.ReactElement {
  const rows = data ?? [];

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-slate-200 text-xs uppercase tracking-wide text-slate-500 dark:border-slate-700 dark:text-slate-400">
            {columns.map((column, index) => (
              <th key={column.id ?? index} className="px-3 py-2 font-medium">
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr>
              <td colSpan={columns.length} className="px-3 py-6 text-center text-slate-400">
                Loading…
              </td>
            </tr>
          ) : rows.length === 0 ? (
            <tr>
              <td colSpan={columns.length} className="px-3 py-6 text-center text-slate-400">
                No items
              </td>
            </tr>
          ) : (
            rows.map((row, rowIndex) => (
              <tr key={rowIndex} className="border-b border-slate-100 last:border-0 dark:border-slate-800">
                {columns.map((column, colIndex) => {
                  const value = cellValue(column, row);
                  const content = column.Cell
                    ? column.Cell({ row: { original: row }, cell: { getValue: () => value } })
                    : (value as React.ReactNode);

                  return (
                    <td key={column.id ?? colIndex} className="px-3 py-2 align-top text-slate-700 dark:text-slate-200">
                      {content}
                    </td>
                  );
                })}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

// SimpleTable is Headlamp's lighter table: `columns` carry a label + getter (datum/cellProps). KSail maps
// it onto the same rendering as Table.
export function SimpleTable({
  columns,
  data,
}: {
  columns: { label?: React.ReactNode; getter?: (row: unknown) => React.ReactNode; cellProps?: unknown }[];
  data?: unknown[];
}): React.ReactElement {
  const mapped: TableColumn[] = columns.map((column, index) => ({
    id: String(index),
    header: column.label,
    Cell: ({ row }) => (column.getter ? column.getter(row.original) : null),
  }));

  return <Table columns={mapped} data={data} />;
}

// ---------------------------------------------------------------------------
// Charts (SVG donut — no recharts dependency)
// ---------------------------------------------------------------------------

// ChartDatum is one donut segment: a value and its fill color (Headlamp's PercentageCircle/TileChart data).
interface ChartDatum {
  name?: string;
  value: number;
  fill?: string;
}

// PercentageCircle renders a donut from the segments, sized to `size` with ring `thickness`. The segments
// are drawn as stroked circle arcs (dash length = fraction of the circumference), rotated to start at 12
// o'clock. A muted track shows beneath.
function PercentageCircle({
  data,
  total,
  size = 140,
  thickness = 14,
}: {
  data: ChartDatum[];
  total?: number;
  size?: number;
  thickness?: number;
}): React.ReactElement {
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  const sum = total ?? data.reduce((acc, datum) => acc + Math.max(datum.value, 0), 0);

  let offset = 0;
  const arcs = sum > 0
    ? data
        .filter((datum) => datum.value > 0)
        .map((datum, index) => {
          const length = (datum.value / sum) * circumference;
          const arc = { key: index, length, offset, fill: datum.fill ?? "#3b82f6" };
          offset += length;

          return arc;
        })
    : [];

  const center = size / 2;

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="-rotate-90">
      <circle
        cx={center}
        cy={center}
        r={radius}
        fill="none"
        strokeWidth={thickness}
        className="stroke-slate-200 dark:stroke-slate-700"
      />
      {arcs.map((arc) => (
        <circle
          key={arc.key}
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={arc.fill}
          strokeWidth={thickness}
          strokeDasharray={`${arc.length} ${circumference - arc.length}`}
          strokeDashoffset={-arc.offset}
        />
      ))}
    </svg>
  );
}

// TileChart is Headlamp's donut status card: a legend (the stat lines) beside a donut whose center shows
// `label` (typically a percentage). Used across the Flux Overview (Kustomizations/HelmReleases/… cards).
export function TileChart({
  data,
  total,
  label,
  legend,
  title,
}: {
  data?: ChartDatum[];
  total?: number;
  label?: React.ReactNode;
  legend?: React.ReactNode;
  title?: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-2">
      {title === undefined ? null : (
        <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</h3>
      )}
      <div className="flex items-center justify-between gap-4">
        {legend === undefined ? null : <div className="min-w-0 text-xs text-slate-600 dark:text-slate-300">{legend}</div>}
        <div className="relative inline-flex shrink-0 items-center justify-center">
          <PercentageCircle data={data ?? []} total={total} />
          {label === undefined ? null : (
            <div className="absolute inset-0 flex items-center justify-center text-sm font-semibold text-slate-700 dark:text-slate-200">
              {label}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Buttons, loaders, empties
// ---------------------------------------------------------------------------

// ActionButton is Headlamp's icon button with a tooltip description.
export function ActionButton({
  description,
  icon,
  onClick,
}: {
  description?: string;
  icon?: React.ReactNode;
  onClick?: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      title={description}
      aria-label={description}
      onClick={onClick}
      className="inline-flex size-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
    >
      {renderIcon(icon)}
    </button>
  );
}

// Loader is Headlamp's spinner.
export function Loader({ title }: { title?: string; noContainer?: boolean }): React.ReactElement {
  return (
    <div className="flex items-center justify-center gap-2 p-6 text-sm text-slate-400" role="status" aria-label={title}>
      <span className="size-4 animate-spin rounded-full border-2 border-slate-300 border-t-transparent dark:border-slate-600 dark:border-t-transparent" />
      {title ? <span>{title}</span> : null}
    </div>
  );
}

// Empty / EmptyContent render Headlamp's empty-state placeholder.
export function Empty({ children }: { children?: React.ReactNode }): React.ReactElement {
  return <div className="p-6 text-center text-sm text-slate-400">{children}</div>;
}

// CommonComponents is the object assigned to pluginLib.CommonComponents (the module shape a plugin's
// CommonComponents import resolves to). Every export above is included so a plugin importing any of them
// from @kinvolk/headlamp-plugin/lib/CommonComponents binds to KSail's native implementation.
export const CommonComponents = {
  SectionBox,
  SectionHeader,
  SectionFilterHeader,
  StatusLabel,
  DateLabel,
  ShowHideLabel,
  HoverInfoLabel,
  Link,
  NameValueTable,
  Table,
  SimpleTable,
  TileChart,
  ActionButton,
  Loader,
  Empty,
  EmptyContent: Empty,
};

export type CommonComponentsShape = typeof CommonComponents;
