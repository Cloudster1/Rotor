import { type FormEvent, type ReactNode, useEffect, useId, useRef } from "react";
import { danger, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

// The dialogs every page shares: native <dialog>, as the run dialog and the canvas's are.

/** What the server refused, in its own sentence. */
export function Refusal({ error }: { error: Error | null | undefined }) {
  if (!error) return null;
  return (
    <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger">
      {error.message}
    </p>
  );
}

/** A modal that holds the focus, closes on Escape and gives the focus back, as the run dialog does. */
export function Dialog({
  title,
  description,
  onClose,
  onSubmit,
  children,
  ...data
}: { title: string; description?: string; onClose: () => void; onSubmit: () => void; children: ReactNode } & Record<`data-${string}`, string>) {
  const id = useId();
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (element && !element.open) element.showModal();
    return () => element?.close();
  }, []);
  function submit(event: FormEvent) {
    event.preventDefault();
    onSubmit();
  }
  return (
    <dialog
      ref={dialog}
      onClose={onClose}
      aria-labelledby={`${id}-title`}
      aria-describedby={description ? `${id}-description` : undefined}
      className="m-auto w-[min(32rem,calc(100vw-2rem))] rounded-overlay border border-border bg-surface-overlay p-0 text-ink shadow-2 backdrop:bg-black/40"
      {...data}
    >
      <form onSubmit={submit} noValidate className="flex max-h-[85vh] flex-col gap-4 overflow-y-auto p-5">
        <h2 id={`${id}-title`} className="text-lg font-semibold text-ink">
          {title}
        </h2>
        {description && (
          <p id={`${id}-description`} className="text-sm text-ink-muted">
            {description}
          </p>
        )}
        {children}
      </form>
    </dialog>
  );
}

/** Asks before something that cannot be undone, or looks so, naming what goes with it; the button says what it does. */
export function ConfirmDialog({
  title,
  body,
  action,
  pending,
  error,
  onConfirm,
  onClose,
}: {
  title: string;
  body: string;
  action: string;
  pending?: boolean;
  error?: Error | null;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog title={title} description={body} onClose={onClose} onSubmit={onConfirm} data-confirm="">
      <Refusal error={error} />
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={secondary} onClick={onClose}>
          {s.shell.cancel}
        </button>
        <button type="submit" className={danger} disabled={pending} data-action="confirm">
          {action}
        </button>
      </div>
    </Dialog>
  );
}
