// useAsyncList is the shared list hook behind the plugin K8s data layer. Both Headlamp surfaces KSail
// reproduces — K8s.ResourceClasses.<Kind>.useList() (k8s.ts) and the K8s.useResourceList() shim
// (pluginLib.ts) — were independently running the same one-shot fetch in a useEffect; this consolidates
// that into a single, tested hook so the two surfaces cannot drift apart.

import * as React from "react";

// useAsyncList fetches a list via the caller-supplied fetcher and exposes [items, error].
//
//   - It holds [items, error] state and runs `fetcher` in a useEffect keyed on `deps` (the caller
//     passes the cluster/kind/namespace primitives so a change re-fetches from scratch).
//   - An `active` guard discards results from a fetch superseded by a deps change or unmount.
//   - `error` is cleared on a successful fetch, so a transient failure clears once a later fetch
//     succeeds.
//
// The fetcher is read from a ref so a caller can pass a fresh closure each render without restarting the
// effect — only a change in `deps` re-fetches.
export function useAsyncList<T>(
  fetcher: () => Promise<T[]>,
  deps: ReadonlyArray<unknown>,
): [T[], Error | null] {
  const [items, setItems] = React.useState<T[]>([]);
  const [error, setError] = React.useState<Error | null>(null);

  const fetcherRef = React.useRef(fetcher);
  fetcherRef.current = fetcher;

  React.useEffect(() => {
    let active = true;

    fetcherRef
      .current()
      .then((list) => {
        if (active) {
          setItems(list);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setError(err instanceof Error ? err : new Error(String(err)));
        }
      });

    return () => {
      active = false;
    };
    // The caller owns the dependency list; the fetcher is read from a ref so a fresh closure each render
    // does not re-run the effect — only a change in `deps` re-fetches.
  }, deps);

  return [items, error];
}
