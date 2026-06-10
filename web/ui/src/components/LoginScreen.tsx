import { loginPath } from "../api.ts";
import { KSailMark } from "./Logo.tsx";

export function LoginScreen() {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden p-6">
      {/* Soft brand-tinted glow behind the card — atmosphere only, so it is hidden from AT. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(36rem_24rem_at_50%_30%,rgba(46,196,230,0.12),transparent_70%)]"
      />
      <div className="relative w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <KSailMark className="mx-auto size-12" />
        <h1 className="mt-4 text-xl font-semibold text-slate-900 dark:text-white">KSail</h1>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          Sign in to manage your clusters.
        </p>
        <a
          href={loginPath}
          className="mt-6 inline-flex w-full items-center justify-center rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600"
        >
          Sign in
        </a>
      </div>
    </div>
  );
}
