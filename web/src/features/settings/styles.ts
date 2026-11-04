// The class strings every Settings and Admin section shares, typed once; a card takes the width its grid cell gives it.

export const card = "rounded-overlay border border-border bg-surface p-5 shadow-1";
export const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
export const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
export const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
export const danger = "h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60";
/** A button that ends something at once, quieter than danger because it asks nothing first. */
export const dangerQuiet =
  "h-8 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-danger hover:bg-surface-sunken disabled:opacity-60";
export const link = "text-sm font-medium text-accent underline-offset-2 hover:underline";
/** An item of a list laid out as cards inside a card, such as one tool or one provider. */
export const item = "rounded-control border border-border p-3";
/** The grid such items sit in: one column, two from md, three from 2xl. */
export const itemGrid = "grid items-start gap-3 md:grid-cols-2 2xl:grid-cols-3";
