import {
  Combobox,
  ComboboxInput,
  ComboboxOption,
  ComboboxOptions,
  Dialog,
  DialogPanel,
  Transition,
  TransitionChild,
} from "@headlessui/react";
import { Search } from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";
import { cx } from "../lib/cx.ts";

// Command is one entry in the ⌘K palette: a label (optionally with an icon and a right-aligned
// context hint), extra search keywords, and the action to run when chosen.
export interface Command {
  id: string;
  label: string;
  hint?: string;
  icon?: ReactNode;
  keywords?: string;
  run: () => void;
}

// CommandPalette is a ⌘K-style fuzzy command launcher built on Headless UI's Combobox inside a
// Dialog (no extra dependency). The caller owns the open state and supplies the command list; on
// selection the command runs and the palette closes.
export function CommandPalette({
  open,
  onClose,
  commands,
}: {
  open: boolean;
  onClose: () => void;
  commands: Command[];
}) {
  const [query, setQuery] = useState("");

  const needle = query.trim().toLowerCase();
  const filtered =
    needle === ""
      ? commands
      : commands.filter((command) =>
          `${command.label} ${command.hint ?? ""} ${command.keywords ?? ""}`.toLowerCase().includes(needle),
        );

  function handleClose() {
    onClose();
  }

  return (
    <Transition show={open} as={Fragment} afterLeave={() => setQuery("")}>
      <Dialog onClose={handleClose} className="relative z-[70]">
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
        <div className="fixed inset-0 flex items-start justify-center p-4 pt-[12vh]">
          <TransitionChild
            as={Fragment}
            enter="ease-out duration-200"
            enterFrom="opacity-0 translate-y-2 scale-95"
            enterTo="opacity-100 translate-y-0 scale-100"
            leave="ease-in duration-150"
            leaveFrom="opacity-100 translate-y-0 scale-100"
            leaveTo="opacity-0 translate-y-2 scale-95"
          >
            <DialogPanel className="w-full max-w-lg overflow-hidden rounded-xl bg-white shadow-2xl ring-1 ring-slate-200 dark:bg-slate-900 dark:ring-slate-800">
              <Combobox
                onChange={(command: Command | null) => {
                  if (command) {
                    command.run();
                    handleClose();
                  }
                }}
              >
                <div className="flex items-center gap-2 border-b border-slate-200 px-4 dark:border-slate-800">
                  <Search className="size-4 shrink-0 text-slate-400" aria-hidden />
                  <ComboboxInput
                    autoFocus
                    className="w-full bg-transparent py-3 text-sm text-slate-900 placeholder:text-slate-400 focus:outline-none dark:text-white"
                    placeholder="Search commands and clusters…"
                    displayValue={() => ""}
                    onChange={(event) => setQuery(event.target.value)}
                  />
                </div>
                <ComboboxOptions static className="max-h-80 overflow-y-auto p-2">
                  {filtered.length === 0 ? (
                    <div className="px-3 py-6 text-center text-sm text-slate-500 dark:text-slate-400">No matches</div>
                  ) : (
                    filtered.map((command) => (
                      <ComboboxOption
                        key={command.id}
                        value={command}
                        className={({ focus }) =>
                          cx(
                            "flex cursor-pointer items-center gap-2.5 rounded-md px-3 py-2 text-sm",
                            focus
                              ? "bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-300"
                              : "text-slate-700 dark:text-slate-200",
                          )
                        }
                      >
                        {command.icon}
                        <span className="flex-1 truncate">{command.label}</span>
                        {command.hint ? <span className="text-xs text-slate-400">{command.hint}</span> : null}
                      </ComboboxOption>
                    ))
                  )}
                </ComboboxOptions>
              </Combobox>
            </DialogPanel>
          </TransitionChild>
        </div>
      </Dialog>
    </Transition>
  );
}
