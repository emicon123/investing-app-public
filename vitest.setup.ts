import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// Radix popper primitives (Tooltip/Popover) measure their content with ResizeObserver once open;
// jsdom does not ship one.
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
globalThis.ResizeObserver ??= ResizeObserverStub;

afterEach(() => {
  cleanup();
});
