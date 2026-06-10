import { Dialog, DialogPanel, DialogTitle, Transition, TransitionChild } from "@headlessui/react";
import { LoaderCircle, X } from "lucide-react";
import {
  Fragment,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { cx } from "../lib/cx.ts";

type Variant = "primary" | "secondary" | "danger" | "ghost";
type Size = "sm" | "md";

const VARIANT: Record<Variant, string> = {
  primary:
    "bg-blue-600 text-white hover:bg-blue-500 focus-visible:outline-blue-600 disabled:opacity-60",
  secondary:
    "bg-white text-slate-700 ring-1 ring-inset ring-slate-300 hover:bg-slate-50 focus-visible:outline-blue-600 disabled:opacity-60 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-700 dark:hover:bg-slate-700",
  danger:
    "bg-red-600 text-white hover:bg-red-500 focus-visible:outline-red-600 disabled:opacity-60",
  ghost:
    "text-slate-600 hover:bg-slate-100 hover:text-slate-900 focus-visible:outline-blue-600 disabled:opacity-60 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white",
};

const SIZE: Record<Size, string> = {
  sm: "gap-1.5 px-2.5 py-1.5 text-xs",
  md: "gap-2 px-3.5 py-2 text-sm",
};

type ButtonProps = {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>;

export function Button({
  variant = "primary",
  size = "md",
  loading = false,
  className,
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cx(
        "inline-flex items-center justify-center rounded-md font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 disabled:cursor-not-allowed",
        VARIANT[variant],
        SIZE[size],
        className,
      )}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? <LoaderCircle className="size-4 animate-spin" aria-hidden /> : null}
      {children}
    </button>
  );
}

type IconButtonProps = {
  label: string;
} & ButtonHTMLAttributes<HTMLButtonElement>;

export function IconButton({ label, className, children, ...props }: IconButtonProps) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      className={cx(
        "inline-flex size-9 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <LoaderCircle className={cx("size-4 animate-spin", className)} aria-hidden />;
}

const backdrop = (
  <TransitionChild
    as={Fragment}
    enter="ease-out duration-200"
    enterFrom="opacity-0"
    enterTo="opacity-100"
    leave="ease-in duration-150"
    leaveFrom="opacity-100"
    leaveTo="opacity-0"
  >
    <div className="fixed inset-0 bg-slate-900/40 backdrop-blur-sm dark:bg-black/60" />
  </TransitionChild>
);

// Modal is a centered dialog used for create and confirmation flows.
export function Modal({
  open,
  onClose,
  title,
  description,
  icon,
  children,
  footer,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: ReactNode;
  icon?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <Transition show={open} as={Fragment}>
      <Dialog onClose={onClose} className="relative z-50">
        {backdrop}
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <TransitionChild
            as={Fragment}
            enter="ease-out duration-200"
            enterFrom="opacity-0 translate-y-2 scale-95"
            enterTo="opacity-100 translate-y-0 scale-100"
            leave="ease-in duration-150"
            leaveFrom="opacity-100 translate-y-0 scale-100"
            leaveTo="opacity-0 translate-y-2 scale-95"
          >
            {/* Cap the panel at the viewport height and scroll the body so tall forms stay usable on
                small screens, while the footer (Cancel/Save) remains pinned and reachable. */}
            <DialogPanel className="flex max-h-[calc(100dvh-2rem)] w-full max-w-md flex-col rounded-xl bg-white shadow-2xl ring-1 ring-slate-200 dark:bg-slate-900 dark:ring-slate-800">
              <div className="overflow-y-auto overscroll-contain p-5">
                <div className="flex items-start gap-3">
                  {icon}
                  <div className="min-w-0 flex-1">
                    <DialogTitle className="text-base font-semibold text-slate-900 dark:text-white">
                      {title}
                    </DialogTitle>
                    {description ? (
                      <div className="mt-1 text-sm text-slate-500 dark:text-slate-400">
                        {description}
                      </div>
                    ) : null}
                  </div>
                </div>
                {children ? <div className="mt-4">{children}</div> : null}
              </div>
              {footer ? (
                <div className="flex shrink-0 justify-end gap-2 border-t border-slate-200 px-5 py-4 dark:border-slate-800">
                  {footer}
                </div>
              ) : null}
            </DialogPanel>
          </TransitionChild>
        </div>
      </Dialog>
    </Transition>
  );
}

// SlideOver is a right-anchored drawer used for resource detail views.
export function SlideOver({
  open,
  onClose,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Transition show={open} as={Fragment}>
      <Dialog onClose={onClose} className="relative z-50">
        {backdrop}
        <div className="fixed inset-0 overflow-hidden">
          <div className="absolute inset-y-0 right-0 flex max-w-full pl-10">
            <TransitionChild
              as={Fragment}
              enter="transform ease-out duration-250"
              enterFrom="translate-x-full"
              enterTo="translate-x-0"
              leave="transform ease-in duration-200"
              leaveFrom="translate-x-0"
              leaveTo="translate-x-full"
            >
              <DialogPanel className="flex h-full w-screen max-w-md flex-col border-l border-slate-200 bg-white shadow-xl dark:border-slate-800 dark:bg-slate-900">
                <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                  <div className="min-w-0">
                    <DialogTitle className="truncate text-base font-semibold text-slate-900 dark:text-white">
                      {title}
                    </DialogTitle>
                    {subtitle ? (
                      <div className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400">
                        {subtitle}
                      </div>
                    ) : null}
                  </div>
                  <IconButton label="Close" onClick={onClose} className="-mr-2 shrink-0">
                    <X className="size-5" />
                  </IconButton>
                </div>
                <div className="flex-1 overflow-y-auto overscroll-contain px-5 py-4">{children}</div>
              </DialogPanel>
            </TransitionChild>
          </div>
        </div>
      </Dialog>
    </Transition>
  );
}

const fieldLabel = "block text-xs font-medium text-slate-600 dark:text-slate-300";
const fieldControl =
  "mt-1 block w-full rounded-md border-0 bg-white px-3 py-2 text-sm text-slate-900 ring-1 ring-inset ring-slate-300 placeholder:text-slate-400 focus:ring-2 focus:ring-inset focus:ring-blue-600 dark:bg-slate-800 dark:text-white dark:ring-slate-700";

export function TextField({
  label,
  className,
  ...props
}: { label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="block">
      <span className={fieldLabel}>{label}</span>
      <input className={cx(fieldControl, className)} {...props} />
    </label>
  );
}

export function SelectField({
  label,
  className,
  children,
  ...props
}: { label: string } & SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <label className="block">
      <span className={fieldLabel}>{label}</span>
      <select className={cx(fieldControl, className)} {...props}>
        {children}
      </select>
    </label>
  );
}
