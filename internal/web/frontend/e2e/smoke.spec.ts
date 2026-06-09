import { test, expect } from "@playwright/test";

// Exercises the real SPA against a seeded store. These are the cross-stack
// regression net: API → embed → SPA render → interaction, including the graph
// canvas (which jsdom can't render) and live task writes. Locators are scoped
// to specific regions because seed text intentionally appears in several places
// (e.g. a task shows in both the list and the activity feed; a note title also
// appears as a body heading).

test("Today lists due-now tasks", async ({ page }) => {
  await page.goto("/");
  // The Today cockpit shows overdue + due-today; the seeded task is due today.
  await expect(page.locator(".row__text", { hasText: "ship the SPA" })).toBeVisible();
});

test("sidebar opens a note; body renders with a resolved wikilink", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Welcome" }).first().click();
  await expect(page.locator(".note > h1")).toHaveText("Welcome");
  // [[Design]] became a real link inside the rendered body.
  await expect(page.locator(".prose").getByRole("link", { name: "Design" })).toBeVisible();
});

test("command palette (⌘K) finds a note and navigates to it", async ({ page }) => {
  await page.goto("/");
  await page.keyboard.press("Control+k");
  const input = page.locator(".palette__input");
  await expect(input).toBeVisible();
  await input.fill("Design");
  await input.press("Enter");
  await expect(page).toHaveURL(/\/n\//);
  await expect(page.locator(".note > h1")).toHaveText("Design");
  // "On this page" outline built from the note's headings.
  await expect(page.getByText("On this page")).toBeVisible();
  // exact: otherwise "Goals" also matches the "Non-goals" TOC link.
  await expect(page.getByRole("link", { name: "Goals", exact: true })).toBeVisible();
});

test("graph view renders a canvas with a node/link summary", async ({ page }) => {
  await page.goto("/graph");
  await expect(page.getByText(/notes ·/)).toBeVisible();
  await expect(page.locator(".graph-canvas canvas")).toBeVisible();
});

test("completing a task moves it out of the open list", async ({ page }) => {
  await page.goto("/tasks");
  const open = page.locator(".row", { hasText: "review graph view" });
  await expect(open).toBeVisible();
  await open.getByTitle("Mark done").click();
  await expect(page.locator(".row__text.done", { hasText: "review graph view" })).toBeVisible();
});
