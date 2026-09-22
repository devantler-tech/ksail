import type { IdentityField } from "../lib/clusterIdentity.ts";

// DETECTED_TITLE explains a value ksail read from the cluster's nodes rather than from its spec.
export const DETECTED_TITLE = "Detected from the cluster's nodes";

// IdentityValue renders a distribution or provider. A value the nodes supplied is dotted-underlined
// with an explanatory title, so a detected fact never passes for one ksail was configured with.
export function IdentityValue({ field }: { field: IdentityField }) {
  if (!field.detected) {
    return <>{field.value}</>;
  }

  return (
    <span title={DETECTED_TITLE} className="underline decoration-slate-400 decoration-dotted underline-offset-2">
      {field.value}
    </span>
  );
}
