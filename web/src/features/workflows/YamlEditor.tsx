import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { yaml, yamlLanguage } from "@codemirror/lang-yaml";
import { bracketMatching, HighlightStyle, indentOnInput, syntaxHighlighting } from "@codemirror/language";
import { type Diagnostic, lintGutter, setDiagnostics } from "@codemirror/lint";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, highlightActiveLine, hoverTooltip, keymap, lineNumbers } from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { stateExtensions, updateSchema } from "codemirror-json-schema";
import { yamlCompletion, yamlSchemaHover } from "codemirror-json-schema/yaml";
import type { JSONSchema7 } from "json-schema";
import { type Ref, useEffect, useImperativeHandle, useRef } from "react";
import type { Problem } from "@/api/workflows";

/** What a page may ask of the editor besides its text. */
export interface YamlEditorHandle {
  insert(text: string): void;
  focusAt(line: number, column: number): void;
}

// Every colour is a token, so the editor follows the light and dark schemes with the rest of the page.
const theme = EditorView.theme({
  "&": {
    backgroundColor: "var(--color-surface)",
    color: "var(--color-ink)",
    fontSize: "var(--text-sm)",
    border: "1px solid var(--color-border-strong)",
    borderRadius: "var(--radius-control)",
    minHeight: "24rem",
  },
  "&.cm-focused": { outline: "2px solid var(--color-focus)", outlineOffset: "1px" },
  ".cm-content": { fontFamily: "var(--font-mono)", caretColor: "var(--color-ink)", padding: "0.5rem 0" },
  ".cm-gutters": { backgroundColor: "var(--color-surface-sunken)", color: "var(--color-ink-muted)", border: "none" },
  ".cm-activeLine": { backgroundColor: "var(--color-surface-raised)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--color-surface-raised)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "var(--color-selection)" },
  ".cm-tooltip": {
    backgroundColor: "var(--color-surface-overlay)",
    color: "var(--color-ink)",
    border: "1px solid var(--color-border)",
    borderRadius: "var(--radius-control)",
    maxWidth: "32rem",
  },
  ".cm-tooltip-autocomplete ul li[aria-selected]": { backgroundColor: "var(--color-accent)", color: "var(--color-on-accent)" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--color-danger)" },
});

const highlight = HighlightStyle.define([
  { tag: [tags.propertyName, tags.definition(tags.propertyName)], color: "var(--color-accent)" },
  { tag: tags.comment, color: "var(--color-ink-muted)", fontStyle: "italic" },
  { tag: [tags.bool, tags.null, tags.number, tags.keyword], fontWeight: "600" },
]);

// One empty list, so an editor without warnings does not mark its text again on every render.
const NO_WARNINGS: Problem[] = [];

/** Where a problem sits in the text, clamped to it: the server counts lines and columns from 1. */
function diagnosticsFor(state: EditorState, problems: Problem[], severity: Diagnostic["severity"]): Diagnostic[] {
  return problems.map((p) => {
    const line = state.doc.line(Math.min(Math.max(p.line || 1, 1), state.doc.lines));
    const from = Math.min(line.from + Math.max((p.column || 1) - 1, 0), line.to);
    return { from, to: Math.max(line.to, from), severity, message: p.message, source: p.code };
  });
}

/** A YAML editor with the workflow schema's completion and descriptions, and the server's problems as marks. */
export function YamlEditor({
  value,
  onChange,
  problems,
  warnings = NO_WARNINGS,
  readOnly,
  label,
  describedBy,
  schema,
  handle,
}: {
  value: string;
  onChange: (value: string) => void;
  problems: Problem[];
  warnings?: Problem[];
  readOnly: boolean;
  label: string;
  describedBy?: string;
  schema?: Record<string, unknown>;
  handle?: Ref<YamlEditorHandle>;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const editable = useRef(new Compartment());
  const changed = useRef(onChange);
  changed.current = onChange;
  // The first props seed the view; the effects below carry later ones in as transactions.
  const first = useRef({ value, readOnly, label, describedBy, schema });

  // The view is made once, so the cursor and history survive every later change of props.
  useEffect(() => {
    if (!host.current) return;
    const { value, readOnly, label, describedBy, schema } = first.current;
    const created = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          history(),
          highlightActiveLine(),
          indentOnInput(),
          bracketMatching(),
          closeBrackets(),
          yaml(),
          syntaxHighlighting(highlight),
          autocompletion(),
          yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
          hoverTooltip(yamlSchemaHover()),
          stateExtensions(schema as JSONSchema7 | undefined),
          lintGutter(),
          keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap, ...completionKeymap]),
          theme,
          editable.current.of([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
          EditorView.contentAttributes.of({ "aria-label": label, ...(describedBy ? { "aria-describedby": describedBy } : {}) }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) changed.current(update.state.doc.toString());
          }),
        ],
      }),
    });
    view.current = created;
    return () => {
      created.destroy();
      view.current = null;
    };
  }, []);

  useEffect(() => {
    const v = view.current;
    if (v && v.state.doc.toString() !== value) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
    }
  }, [value]);

  useEffect(() => {
    const v = view.current;
    if (v) {
      const marks = [...diagnosticsFor(v.state, problems, "error"), ...diagnosticsFor(v.state, warnings, "warning")];
      v.dispatch(setDiagnostics(v.state, marks));
    }
  }, [problems, warnings]);

  useEffect(() => {
    view.current?.dispatch({
      effects: editable.current.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]),
    });
  }, [readOnly]);

  useEffect(() => {
    if (view.current && schema) updateSchema(view.current, schema as JSONSchema7);
  }, [schema]);

  useImperativeHandle(handle, () => ({
    insert(text: string) {
      const v = view.current;
      if (!v) return;
      const at = v.state.selection.main.head;
      v.dispatch({ changes: { from: at, insert: text }, selection: { anchor: at + text.length } });
      v.focus();
    },
    focusAt(line: number, column: number) {
      const v = view.current;
      if (!v) return;
      const target = v.state.doc.line(Math.min(Math.max(line, 1), v.state.doc.lines));
      const at = Math.min(target.from + Math.max(column - 1, 0), target.to);
      v.dispatch({ selection: { anchor: at }, scrollIntoView: true });
      v.focus();
    },
  }));

  return <div ref={host} data-yaml-editor />;
}
