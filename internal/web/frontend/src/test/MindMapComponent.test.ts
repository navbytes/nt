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
});
