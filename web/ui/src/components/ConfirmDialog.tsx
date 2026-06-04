import { TriangleAlert } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Button, Modal } from "./ui.tsx";

// ConfirmDialog gates a destructive action behind an explicit confirmation step.
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "Confirm",
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel?: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const [working, setWorking] = useState(false);

  async function handleConfirm() {
    if (working) {
      return;
    }

    setWorking(true);
    try {
      await onConfirm();
      onClose();
    } catch {
      // Error surfaced via toast by the caller; keep the dialog open.
    } finally {
      setWorking(false);
    }
  }

  return (
    <Modal
      open={open}
      onClose={working ? () => undefined : onClose}
      title={title}
      description={description}
      icon={
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400">
          <TriangleAlert className="size-5" aria-hidden />
        </span>
      }
      footer={
        <>
          <Button variant="secondary" onClick={onClose} disabled={working}>
            Cancel
          </Button>
          <Button variant="danger" loading={working} onClick={() => void handleConfirm()}>
            {confirmLabel}
          </Button>
        </>
      }
    />
  );
}
