import { type FormEvent, type ReactNode, useEffect, useId, useRef } from "react";
import { danger, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

// The pieces Access and Users share, standing in for the kit Armature builds them from.

/** A list's frame, as the audit log's table has it, scrolling sideways inside itself on a phone. */
export const tableFrame = "overflow-x-auto rounded-overlay border border-border bg-surface shadow-1";
export const th = "px-3 py-2 font-medium";
export const td = "px-3 py-2 align-top";
export const select = "h-9 w-full rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
export const label = "flex flex-col gap-1 text-sm font-medium text-ink";

/** A small word beside a name, such as "built in" or "group". */
export function Tag({ children, className = "", ...data }: { children: ReactNode; className?: string } & Record<`data-${string}`, string>) {
  return (
    <span className={`rounded-full bg-surface-raised px-2 py-0.5 text-2xs font-medium whitespace-nowrap text-ink-muted ${className}`} {...data}>
      {children}
    </span>
  );
}

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

/** Asks before something that cannot be undone, naming what goes with it; the button says what it does. */
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
          {s.access.cancel}
        </button>
        <button type="submit" className={danger} disabled={pending} data-action="confirm">
          {action}
        </button>
      </div>
    </Dialog>
  );
}

function range(offset: number, shown: number, total: number): string {
  return s.access.pager.range(total === 0 ? 0 : offset + 1, offset + shown, total);
}

/** The whole count while it fits on the page, and which of it is shown once it does not. */
export function shownOf(total: number, offset: number, shown: number, noun: { one: string; many: string }): string {
  if (offset === 0 && shown >= total) return `${total} ${total === 1 ? noun.one : noun.many}`;
  return `${range(offset, shown, total)} ${noun.many}`;
}

/** The start of the last page, for a page left past the end when the list shrank. */
export function clampOffset(offset: number, total: number, pageSize: number): number {
  if (offset < total) return offset;
  return Math.max(0, Math.floor((total - 1) / pageSize) * pageSize);
}

/** Steps through a list longer than one page. */
export function Pager({
  total,
  offset,
  pageSize,
  onChange,
  label,
}: {
  total: number;
  offset: number;
  pageSize: number;
  onChange: (offset: number) => void;
  label: string;
}) {
  if (total <= pageSize && offset === 0) return null;
  return (
    <nav aria-label={label} className="flex items-center justify-end gap-2 text-sm text-ink-muted">
      <span className="tabular-nums" data-page-range>
        {range(offset, Math.max(0, Math.min(pageSize, total - offset)), total)}
      </span>
      <button
        type="button"
        className={secondary}
        aria-label={s.access.pager.previousPage}
        disabled={offset === 0}
        onClick={() => onChange(Math.max(0, offset - pageSize))}
      >
        {s.access.pager.previous}
      </button>
      <button
        type="button"
        className={secondary}
        aria-label={s.access.pager.nextPage}
        disabled={offset + pageSize >= total}
        onClick={() => onChange(offset + pageSize)}
      >
        {s.access.pager.next}
      </button>
    </nav>
  );
}
