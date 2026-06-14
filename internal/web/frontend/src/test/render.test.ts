import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/svelte";
import Harness from "./Harness.svelte";

// The command palette navigates via the router; mock it so we can assert.
vi.mock("../lib/router.svelte", () => ({
  navigate: vi.fn(),
  loc: { path: "/", query: new URLSearchParams() },
  initRouter: () => () => {},
}));

// Mock the API client so components render against fixtures, no server needed.
vi.mock("../lib/api", () => {
  const groups = [
    { status: "open", tasks: [{ id: "T1", text: "write tests", status: "open", source: "cli" }] },
    { status: "done", tasks: [{ id: "T2", text: "old thing", status: "done" }] },
  ];
  return {
    setCsrf: vi.fn(),
    SaveConflict: class SaveConflict extends Error {},
    api: {
      raw: vi.fn().mockResolvedValue({ text: "# Hello\n\nthe body", etag: '"e1"' }),
      preview: vi.fn().mockResolvedValue("<p>preview</p>"),
      save: vi.fn().mockResolvedValue(undefined),
      state: vi.fn().mockResolvedValue({
        csrf: "x",
        version: "v",
        openCount: 1,
        noteCount: 2,
        sources: ["cli"],
      }),
      notes: vi.fn().mockResolvedValue({
        tree: [
          { name: "Welcome", path: "", url: "/n/abc", isNote: true },
          {
            name: "docs",
            path: "docs",
            url: "",
            isNote: false,
            children: [{ name: "Design", path: "", url: "/n/def", isNote: true }],
          },
        ],
        index: [
          { url: "/n/abc", title: "Welcome", path: "welcome.md" },
          { url: "/n/def", title: "Design", path: "docs/design.md" },
        ],
      }),
      note: vi.fn().mockResolvedValue({
        id: "def",
        title: "Design",
        folder: "docs",
        file: "design.md",
        crumbs: ["docs"],
        source: "cli",
        created: "2026-06-08",
        tags: ["spec"],
        bodyHTML:
          '<h2 id="overview">Overview</h2><p>the rendered body</p><h2 id="details">Details</h2>',
        backlinks: [{ title: "Welcome", url: "/n/abc", text: "", isNote: true }],
        taskRefs: [{ text: "do the thing", status: "open", source: "cli" }],
        etag: '"abc"',
      }),
      tasks: vi.fn().mockResolvedValue({ groups }),
      activity: vi.fn().mockResolvedValue({ days: [], sources: ["cli"] }),
      search: vi.fn().mockResolvedValue({ results: [] }),
      tags: vi.fn().mockResolvedValue({ tags: [{ name: "spec", count: 3 }] }),
      orphans: vi.fn().mockResolvedValue({ notes: [{ url: "/n/z", title: "Lonely", path: "lonely.md" }] }),
      taskDone: vi.fn().mockResolvedValue({ groups }),
      taskReopen: vi.fn().mockResolvedValue({ groups }),
      taskNew: vi.fn().mockResolvedValue({ groups }),
    },
  };
});

import { api } from "../lib/api";
import TaskRows from "../lib/TaskRows.svelte";
import Sidebar from "../lib/Sidebar.svelte";
import NoteView from "../routes/NoteView.svelte";
import Editor from "../lib/Editor.svelte";
import CommandPalette from "../lib/CommandPalette.svelte";
import Tags from "../routes/Tags.svelte";
import { navigate } from "../lib/router.svelte";
import { openPalette, closePalette } from "../lib/palette.svelte";

beforeEach(() => vi.clearAllMocks());

describe("TaskRows", () => {
  it("renders tasks from the query and completes one via the mutation", async () => {
    render(Harness, { props: { comp: TaskRows, props: {} } });

    expect(await screen.findByText("write tests")).toBeInTheDocument();
    await fireEvent.click(screen.getByTitle("Mark done"));
    // TanStack Query invokes mutationFn as (variables, context); only the id matters.
    expect(api.taskDone).toHaveBeenCalledOnce();
    expect(vi.mocked(api.taskDone).mock.calls[0]?.[0]).toBe("T1");
  });

  it("filters groups by status", async () => {
    render(Harness, { props: { comp: TaskRows, props: { statuses: ["open"] } } });
    expect(await screen.findByText("write tests")).toBeInTheDocument();
    expect(screen.queryByText("old thing")).not.toBeInTheDocument();
  });
});

describe("Sidebar", () => {
  it("renders the note tree (notes and nested folders)", async () => {
    render(Harness, { props: { comp: Sidebar, props: { path: "/" } } });
    expect(await screen.findByText("Welcome")).toBeInTheDocument();
    expect(screen.getByText("docs")).toBeInTheDocument();
    expect(screen.getByText("Design")).toBeInTheDocument();
  });
});

describe("NoteView", () => {
  it("renders title, server-rendered body, backlinks and task refs", async () => {
    render(Harness, { props: { comp: NoteView, props: { handle: "def" } } });
    expect(await screen.findByRole("heading", { name: "Design" })).toBeInTheDocument();
    expect(screen.getByText("the rendered body")).toBeInTheDocument();
    expect(screen.getByText("do the thing")).toBeInTheDocument();
    expect(screen.getByText("Linked from")).toBeInTheDocument();
  });

  it("builds an On-this-page TOC from the body headings", async () => {
    render(Harness, { props: { comp: NoteView, props: { handle: "def" } } });
    await screen.findByRole("heading", { name: "Design" });
    expect(screen.getByText("On this page")).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Overview" });
    expect(link.getAttribute("href")).toBe("#overview");
    expect(screen.getByRole("link", { name: "Details" })).toBeInTheDocument();
  });
});

describe("Editor", () => {
  it("loads raw text and saves with the captured etag, then closes", async () => {
    const onClose = vi.fn();
    render(Harness, { props: { comp: Editor, props: { handle: "def", onClose } } });

    // The CodeMirror editor is ready once the raw note loads (Save enabled).
    const saveBtn = (await screen.findByText("Save")) as HTMLButtonElement;
    await vi.waitFor(() => expect(saveBtn.disabled).toBe(false));

    await fireEvent.click(saveBtn);
    expect(api.save).toHaveBeenCalledOnce();
    const call = vi.mocked(api.save).mock.calls[0];
    expect(call?.[0]).toBe("def");
    expect(call?.[1]).toContain("the body"); // the loaded buffer was saved back
    expect(call?.[2]).toBe('"e1"');
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});

describe("CommandPalette", () => {
  afterEach(closePalette);

  it("opens, filters notes by query, and navigates on Enter", async () => {
    openPalette();
    render(Harness, { props: { comp: CommandPalette } });

    const input = (await screen.findByPlaceholderText(/Search notes/i)) as HTMLInputElement;
    await fireEvent.input(input, { target: { value: "Design" } });

    const hit = await screen.findByText("Design");
    expect(hit).toBeInTheDocument();
    // Nav items not matching the query are filtered out.
    expect(screen.queryByText("Today")).not.toBeInTheDocument();

    await fireEvent.keyDown(input, { key: "Enter" });
    expect(navigate).toHaveBeenCalledWith("/n/def");
  });
});

describe("Tags", () => {
  it("renders tags with counts linking to tag search", async () => {
    render(Harness, { props: { comp: Tags } });
    const chip = await screen.findByText("#spec", { exact: false });
    expect(chip.closest("a")?.getAttribute("href")).toBe("/search?tag=spec");
    expect(screen.getByText("3")).toBeInTheDocument();
  });
});
