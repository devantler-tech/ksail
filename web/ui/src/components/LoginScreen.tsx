import { Boxes } from "lucide-react";
import { loginPath } from "../api.ts";

export function LoginScreen() {
  return (
    <div className="flex min-h-full items-center justify-center p-6">
      <div className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white p-8 text-center shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <span className="mx-auto flex size-12 items-center justify-center rounded-xl bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-500">
          <Boxes className="size-7" aria-hidden />
        </span>
        <h1 className="mt-4 text-xl font-semibold text-slate-900 dark:text-white">KSail</h1>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          Sign in to manage your clusters.
        </p>
        <a
          href={loginPath}
          className="mt-6 inline-flex w-full items-center justify-center rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-500"
        >
          Sign in
        </a>
      </div>
    </div>
  );
}
