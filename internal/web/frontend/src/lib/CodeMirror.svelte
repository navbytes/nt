<script lang="ts">
  import { untrack } from "svelte";
  import { EditorView, keymap, highlightActiveLine, drawSelection } from "@codemirror/view";
  import { EditorState } from "@codemirror/state";
  import { history, historyKeymap, defaultKeymap, indentWithTab } from "@codemirror/commands";
  import { markdown } from "@codemirror/lang-markdown";
  import { syntaxHighlighting, HighlightStyle } from "@codemirror/language";
  import { autocompletion, completionKeymap } from "@codemirror/autocomplete";
  import { tags as t } from "@lezer/highlight";
  import { makeWikilinkSource } from "./cmWikilink";
  import { slashSource } from "./cmSlash";
  import type { NoteLink } from "./api";

  let {
    value,
    onChange,
    onSave,
    onEscape,
    getNotes,
  }: {
    value: string;
    onChange: (v: string) => void;
    onSave: () => void;
    onEscape: () => void;
    getNotes: () => NoteLink[];
  } = $props();

  let host: HTMLDivElement | undefined = $state();

  // Latest callbacks/notes, kept in a ref so the editor mounts ONCE — reading
  // them directly in the mount effect would recreate the view on every keystroke
  // (value changes) and when the note index loads (getNotes changes), wiping
  // edits and the cursor.
  let latest = { onChange, onSave, onEscape, getNotes };
  $effect(() => {
    latest = { onChange, onSave, onEscape, getNotes };
  });

  // The editor inherits the app's CSS variables, so it tracks light/dark with the
  // rest of the UI instead of shipping a second hardcoded palette.
  const theme = EditorView.theme({
    "&": { backgroundColor: "transparent", color: "var(--fg)", height: "100%" },
    "&.cm-focused": { outline: "none" },
    ".cm-scroller": { fontFamily: "var(--font-mono)", fontSize: "13px", lineHeight: "1.6" },
    ".cm-content": { caretColor: "var(--accent)", padding: "8px 0" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--accent)" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
      backgroundColor: "var(--bg-inset)",
    },
    ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--accent) 7%, transparent)" },
    ".cm-tooltip": {
      backgroundColor: "var(--bg-elev)",
      border: "1px solid var(--border)",
      borderRadius: "var(--radius-sm)",
      color: "var(--fg)",
    },
    ".cm-tooltip-autocomplete ul li[aria-selected]": {
      backgroundColor: "var(--accent)",
      color: "#fff",
    },
  });

  const highlight = HighlightStyle.define([
    { tag: t.heading, color: "var(--accent)", fontWeight: "bold" },
    { tag: t.strong, color: "var(--fg)", fontWeight: "bold" },
    { tag: t.emphasis, fontStyle: "italic" },
    { tag: [t.link, t.url], color: "var(--accent-2)" },
    { tag: t.monospace, color: "var(--green)" },
    { tag: t.quote, color: "var(--muted)" },
    { tag: [t.list, t.processingInstruction], color: "var(--fg-soft)" },
    { tag: t.strikethrough, textDecoration: "line-through", color: "var(--muted)" },
  ]);

  const wikilinkSource = makeWikilinkSource(() => latest.getNotes());

  let view: EditorView | undefined;

  // Mount once: this effect tracks only `host` (set once via bind:this); `value`
  // is read untracked as the initial doc, and callbacks go through `latest`.
  $effect(() => {
    if (!host) return;
    const el = host;
    view = new EditorView({
      parent: el,
      state: EditorState.create({
        doc: untrack(() => value),
        extensions: [
          history(),
          drawSelection(),
          highlightActiveLine(),
          EditorView.lineWrapping,
          markdown(),
          syntaxHighlighting(highlight),
          autocompletion({ override: [wikilinkSource, slashSource], icons: false }),
          theme,
          keymap.of([
            { key: "Mod-s", preventDefault: true, run: () => (latest.onSave(), true) },
            { key: "Escape", run: () => (latest.onEscape(), true) },
            ...completionKeymap,
            indentWithTab,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) latest.onChange(u.state.doc.toString());
          }),
        ],
      }),
    });
    view.focus();
    // Expose the view for e2e/integration checks (harmless in prod).
    (el as unknown as { cmView?: EditorView }).cmView = view;
    return () => {
      view?.destroy();
      view = undefined;
    };
  });

  // Reflect an EXTERNAL `value` change (e.g. a frontmatter tag edit reseeds the
  // buffer) into the view. Guarded by an equality check so the user's own typing
  // — which already flows out via onChange — never triggers a self-replace.
  $effect(() => {
    const v = value;
    if (view && v !== view.state.doc.toString()) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } });
    }
  });
</script>

<div bind:this={host} class="cm-host"></div>

<style>
  .cm-host {
    height: 100%;
    overflow: hidden;
  }
  .cm-host :global(.cm-editor) {
    height: 100%;
  }
  .cm-host :global(.cm-scroller) {
    overflow: auto;
  }
</style>
