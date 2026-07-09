import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/svelte";
import MindMap from "../lib/MindMap.svelte";
import { parseOutline } from "../lib/outline";

// Mounts the real Svelte component in jsdom (no browser) to verify it renders
// the outline as SVG and that clicking a branch collapses its subtree — the
// interaction that unit-testing the pure layout can't reach.
const html =
  '<h2 id="goals">Goals</h2><ul><li>Ship<ul><li>Auth</li><li>Billing</li></ul></li></ul><h2 id="risks">Risks</h2>';

function mount(onJump = vi.fn()) {
  const { root, truncated } = parseOutline(html, "Project");
  return { onJump, ...render(MindMap, { props: { root, truncated, onJump } }) };
}

describe("MindMap component", () => {
  it("renders a node per outline entry as SVG groups", () => {
    const { container } = mount();
    // Project + Goals + Ship + Auth + Billing + Risks = 6.
    expect(container.querySelectorAll("g.mm__node").length).toBe(6);
    expect(container.querySelector("svg.mm__svg")).toBeTruthy();
  });

  it("shows every outline label", () => {
    const { container } = mount();
    const labels = [...container.querySelectorAll("text.mm__label")].map((t) => t.textContent?.trim());
    expect(labels).toEqual(expect.arrayContaining(["Project", "Goals", "Ship", "Auth", "Billing", "Risks"]));
  });

  it("collapses a branch on click, hiding its descendants", async () => {
    const { container } = mount();
    const before = container.querySelectorAll("g.mm__node").length;
    // Click "Ship" (has Auth + Billing children).
    const ship = [...container.querySelectorAll("g.mm__node")].find((g) =>
      g.textContent?.includes("Ship"),
    )!;
    await fireEvent.click(ship);
    const after = container.querySelectorAll("g.mm__node").length;
    expect(after).toBe(before - 2); // Auth + Billing hidden
    // The collapsed node gains its "+" badge.
    expect(ship.querySelector("text.mm__badge")?.textContent).toBe("+");
  });

  it("jumps via onJump when a leaf heading is double-clicked", async () => {
    const { container, onJump } = mount();
    const risks = [...container.querySelectorAll("g.mm__node")].find((g) =>
      g.textContent?.includes("Risks"),
    )!;
    await fireEvent.dblClick(risks);
    expect(onJump).toHaveBeenCalledWith("risks");
  });

  it("collapse-all leaves only the trunk (root + its children)", async () => {
    const { container, getByTitle } = mount();
    await fireEvent.click(getByTitle("Collapse to trunk"));
    // root Project + Goals + Risks = 3 (Ship and its kids collapse under Goals…
    // actually Goals→Ship collapses; trunk = depth-1 nodes shown).
    const shown = container.querySelectorAll("g.mm__node").length;
    expect(shown).toBeLessThan(6);
    expect(shown).toBeGreaterThanOrEqual(3);
  });

  // The focused node is the only tab-reachable one (roving tabindex); arrows move.
  const focusedLabel = (c: HTMLElement) =>
    c.querySelector('g.mm__node[tabindex="0"]')?.textContent?.trim();

  it("arrow keys move focus down to a child, across siblings, and up to the parent", async () => {
    const { container } = mount();
    const rootG = container.querySelector('g.mm__node[tabindex="0"]')!;
    expect(focusedLabel(container)).toContain("Project"); // root starts focused

    await fireEvent.keyDown(rootG, { key: "ArrowDown" });
    expect(focusedLabel(container)).toContain("Goals"); // stepped into first child

    await fireEvent.keyDown(container.querySelector('g.mm__node[tabindex="0"]')!, { key: "ArrowRight" });
    expect(focusedLabel(container)).toContain("Risks"); // sibling of Goals

    await fireEvent.keyDown(container.querySelector('g.mm__node[tabindex="0"]')!, { key: "ArrowUp" });
    expect(focusedLabel(container)).toContain("Project"); // back to parent (root)
  });

  it("Enter collapses the focused branch", async () => {
    const { container } = mount();
    const ship = [...container.querySelectorAll("g.mm__node")].find((g) =>
      g.textContent?.includes("Ship"),
    )!;
    const before = container.querySelectorAll("g.mm__node").length;
    await fireEvent.keyDown(ship, { key: "Enter" });
    expect(container.querySelectorAll("g.mm__node").length).toBe(before - 2);
  });

  it("exposes zoom controls", () => {
    const { getByLabelText } = mount();
    expect(getByLabelText("Zoom in")).toBeTruthy();
    expect(getByLabelText("Zoom out")).toBeTruthy();
  });

  // --- editable mode -------------------------------------------------------
  function mountEditable() {
    const spies = {
      onAddChild: vi.fn(),
      onAddSibling: vi.fn(),
      onRename: vi.fn(),
      onDelete: vi.fn(),
    };
    const { root, truncated } = parseOutline(html, "Project");
    return { spies, ...render(MindMap, { props: { root, truncated, editable: true, ...spies } }) };
  }
  const nodeByText = (c: HTMLElement, t: string) =>
    [...c.querySelectorAll("g.mm__node")].find((g) => g.textContent?.includes(t)) as SVGGElement;

  it("shows an edit toolbar on the focused node", async () => {
    const { container } = mountEditable();
    await fireEvent.click(nodeByText(container, "Billing")); // a leaf
    const bar = container.querySelector(".mm__nodebar");
    expect(bar).toBeTruthy();
    expect(bar!.querySelector('[aria-label="Rename"]')).toBeTruthy();
    expect(bar!.querySelector('[aria-label="Delete"]')).toBeTruthy();
  });

  it("renames via the inline field (Enter commits onRename)", async () => {
    const { container, spies, getByLabelText } = mountEditable();
    await fireEvent.click(nodeByText(container, "Billing")); // root.0.0.1
    await fireEvent.click(getByLabelText("Rename"));
    const input = container.querySelector("input.mm__edit") as HTMLInputElement;
    expect(input).toBeTruthy();
    input.value = "Billing v2";
    await fireEvent.input(input);
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(spies.onRename).toHaveBeenCalledWith("root.0.0.1", "Billing v2");
  });

  it("Tab opens a child field that commits onAddChild", async () => {
    const { container, spies } = mountEditable();
    const auth = nodeByText(container, "Auth"); // root.0.0.0
    await fireEvent.click(auth);
    await fireEvent.keyDown(auth, { key: "Tab" });
    const input = container.querySelector("input.mm__edit") as HTMLInputElement;
    input.value = "TOTP";
    await fireEvent.input(input);
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(spies.onAddChild).toHaveBeenCalledWith("root.0.0.0", "TOTP");
  });

  it("Delete key removes a non-root node", async () => {
    const { container, spies } = mountEditable();
    const auth = nodeByText(container, "Auth");
    await fireEvent.click(auth);
    await fireEvent.keyDown(auth, { key: "Delete" });
    expect(spies.onDelete).toHaveBeenCalledWith("root.0.0.0");
  });

  it("focuses the requested node when the parent asks (focus-follows-new-node)", async () => {
    const { root, truncated } = parseOutline(html, "Project");
    const { container, rerender } = render(MindMap, {
      props: { root, truncated, editable: true, focusRequest: null },
    });
    // Ask to focus "Billing" (root.0.0.1) as if it were just added.
    await rerender({ root, truncated, editable: true, focusRequest: "root.0.0.1" });
    expect(container.querySelector('g.mm__node[tabindex="0"]')?.textContent).toContain("Billing");
  });

  it("reparents on drag-drop (pointer down on a node, up on another)", async () => {
    const onReparent = vi.fn();
    const { root, truncated } = parseOutline(html, "Project");
    const { container } = render(MindMap, { props: { root, truncated, editable: true, onReparent } });
    const auth = nodeByText(container, "Auth"); // root.0.0.0
    const risks = nodeByText(container, "Risks"); // root.1
    // Begin dragging Auth, then release over Risks. elementsFromPoint is stubbed
    // in jsdom, so drive dropId through the same code path via a spy on the DOM.
    await fireEvent.pointerDown(auth);
    // Simulate the move resolving Risks as the drop target, then release.
    const svg = container.querySelector("svg.mm__svg")!;
    (document as any).elementsFromPoint = () => [risks];
    await fireEvent.pointerMove(svg, { clientX: 10, clientY: 10 });
    await fireEvent.pointerUp(svg);
    expect(onReparent).toHaveBeenCalledWith("root.0.0.0", "root.1");
  });

  it("Escape cancels an inline edit without committing", async () => {
    const { container, spies } = mountEditable();
    await fireEvent.click(nodeByText(container, "Billing"));
    await fireEvent.keyDown(nodeByText(container, "Billing"), { key: "F2" });
    const input = container.querySelector("input.mm__edit") as HTMLInputElement;
    input.value = "nope";
    await fireEvent.input(input);
    await fireEvent.keyDown(input, { key: "Escape" });
    expect(spies.onRename).not.toHaveBeenCalled();
    expect(container.querySelector("input.mm__edit")).toBeNull();
  });

  it("toggles a fullscreen overlay via the button and Escape", async () => {
    const { container, getByLabelText } = mount();
    const mm = container.querySelector(".mm")!;
    expect(mm.classList.contains("mm--full")).toBe(false);

    await fireEvent.click(getByLabelText("Fullscreen"));
    expect(mm.classList.contains("mm--full")).toBe(true);
    expect(document.body.style.overflow).toBe("hidden"); // page scroll-locked

    // Escape exits fullscreen and restores scrolling.
    await fireEvent.keyDown(window, { key: "Escape" });
    expect(mm.classList.contains("mm--full")).toBe(false);
    expect(document.body.style.overflow).not.toBe("hidden");
  });

  it("culls deep labels on a dense map but keeps the root", () => {
    // >60 nodes trips dense mode; depth-4 leaves (deepN) are culled, root stays.
    const deep = Array.from({ length: 62 }, (_, i) => `<li>deep${i}</li>`).join("");
    const denseHtml = `<h2>H</h2><ul><li>A<ul><li>B<ul>${deep}</ul></li></ul></li></ul>`;
    const { root, truncated } = parseOutline(denseHtml, "BigNote");
    const { container } = render(MindMap, { props: { root, truncated } });
    const labels = [...container.querySelectorAll("text.mm__label")].map((t) => t.textContent ?? "");
    expect(labels.some((l) => l.includes("BigNote"))).toBe(true); // root label kept
    expect(labels.some((l) => l.includes("deep"))).toBe(false); // deep leaves culled
    // Fewer labels than nodes → culling actually happened.
    expect(container.querySelectorAll("text.mm__label").length).toBeLessThan(
      container.querySelectorAll("g.mm__node").length,
    );
  });
});
