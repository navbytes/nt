import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { cleanup } from "@testing-library/svelte";

// jsdom ships no ResizeObserver, which Svelte's `bind:clientWidth` (and any
// component observing element size) needs. Real browsers always have it; this
// no-op stub lets those components mount in the test environment.
if (!("ResizeObserver" in globalThis)) {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

afterEach(cleanup);
