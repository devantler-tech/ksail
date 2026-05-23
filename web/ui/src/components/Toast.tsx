import { CircleAlert, CircleCheck, Info, X } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { cx } from "../lib/cx.ts";

type ToastKind = "success" | "error" | "info";

interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastApi {
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

const DISMISS_MS = 5000;

const KIND: Record<ToastKind, { icon: typeof Info; accent: string }> = {
  success: { icon: CircleCheck, accent: "text-emerald-500" },
  error: { icon: CircleAlert, accent: "text-red-500" },
  info: { icon: Info, accent: "text-blue-500" },
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const counter = useRef(0);

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id));
  }, []);

  const push = useCallback(
    (kind: ToastKind, message: string) => {
      counter.current += 1;
      const id = counter.current;
      setToasts((current) => [...current, { id, kind, message }]);
      window.setTimeout(() => dismiss(id), DISMISS_MS);
    },
    [dismiss],
  );

  const api = useMemo<ToastApi>(
    () => ({
      success: (message) => push("success", message),
      error: (message) => push("error", message),
      info: (message) => push("info", message),
    }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-full max-w-sm flex-col gap-2">
        {toasts.map((toast) => {
          const { icon: Icon, accent } = KIND[toast.kind];
          return (
            <div
              key={toast.id}
              role="status"
              className="pointer-events-auto flex items-start gap-3 rounded-lg border border-slate-200 bg-white p-3 shadow-lg dark:border-slate-700 dark:bg-slate-800"
            >
              <Icon className={cx("mt-0.5 size-5 shrink-0", accent)} aria-hidden />
              <p className="flex-1 break-words text-sm text-slate-700 dark:text-slate-200">
                {toast.message}
              </p>
              <button
                type="button"
                aria-label="Dismiss"
                onClick={() => dismiss(toast.id)}
                className="-m-1 rounded p-1 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200"
              >
                <X className="size-4" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error("useToast must be used within a ToastProvider");
  }

  return context;
}
