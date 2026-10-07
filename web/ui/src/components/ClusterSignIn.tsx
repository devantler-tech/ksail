import { useEffect, useRef, useState } from "react";
import { errorMessage, getClusterAuthentication, renewClusterAuthentication, type ClusterAuthentication } from "../api.ts";
import { ErrorBanner } from "./states.tsx";
import { Button } from "./ui.tsx";

export function ClusterSignIn({ namespace, name, onSignedIn }: {
  namespace: string;
  name: string;
  onSignedIn: () => void;
}) {
  const [info, setInfo] = useState<ClusterAuthentication | null>(null);
  const [error, setError] = useState("");
  const [signingIn, setSigningIn] = useState(false);
  const request = useRef<AbortController | null>(null);

  useEffect(() => {
    let cancelled = false;
    getClusterAuthentication(namespace, name)
      .then((value) => { if (!cancelled) setInfo(value); })
      .catch((err: unknown) => { if (!cancelled) setError(errorMessage(err)); });
    return () => {
      cancelled = true;
      request.current?.abort();
    };
  }, [namespace, name]);

  async function signIn() {
    const controller = new AbortController();
    request.current = controller;
    setSigningIn(true);
    setError("");
    try {
      await renewClusterAuthentication(namespace, name, controller.signal);
      if (!controller.signal.aborted) {
        setInfo((current) => current ? { ...current, required: false } : current);
        onSignedIn();
      }
    } catch (err: unknown) {
      setError(controller.signal.aborted ? "AWS sign-in cancelled." : errorMessage(err));
    } finally {
      if (request.current === controller) request.current = null;
      setSigningIn(false);
    }
  }

  return (
    <div className="space-y-2">
      {error ? <ErrorBanner message={error} /> : null}
      {info && !info.enabled ? <p className="text-sm text-slate-500">{info.message}</p> : null}
      {info?.enabled && info.supported && info.required ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button onClick={() => void signIn()} loading={signingIn}>Sign in to AWS SSO</Button>
          {signingIn ? (
            <>
              <span className="text-sm text-slate-500">Complete AWS sign-in in your browser.</span>
              <Button variant="secondary" onClick={() => request.current?.abort()}>Cancel sign-in</Button>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
