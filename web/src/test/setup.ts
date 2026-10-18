import "@testing-library/jest-dom/vitest";
import "./layout";

// Node's built-in localStorage is inert without --localstorage-file and shadows jsdom's, so tests get an
// in-memory Storage; one that refuses storage can still spy on Storage.prototype.
class MemoryStorage implements Storage {
  #entries = new Map<string, string>();

  get length(): number {
    return this.#entries.size;
  }

  clear(): void {
    this.#entries.clear();
  }

  getItem(key: string): string | null {
    return this.#entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.#entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.#entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.#entries.set(key, String(value));
  }
}

// Always replaced, so a test that spies on Storage.prototype reaches the storage the code uses.
Object.defineProperty(globalThis, "Storage", { value: MemoryStorage, writable: true, configurable: true });
const storage = new MemoryStorage();
Object.defineProperty(globalThis, "localStorage", { value: storage, writable: true, configurable: true });
if (typeof window !== "undefined") {
  Object.defineProperty(window, "localStorage", { value: storage, writable: true, configurable: true });
}

// jsdom leaves scrollTo unimplemented and logs an error each time the router restores the scroll position.
if (typeof window !== "undefined") {
  window.scrollTo = () => {};
}

// jsdom has no canvas and logs an error for every context asked of it, which axe asks for.
if (typeof HTMLCanvasElement !== "undefined") {
  HTMLCanvasElement.prototype.getContext = (() => null) as typeof HTMLCanvasElement.prototype.getContext;
}

// jsdom draws <dialog> but leaves showModal and close unimplemented; these open and close it as a browser would.
if (typeof HTMLDialogElement !== "undefined" && !HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute("open", "");
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    if (!this.hasAttribute("open")) return;
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  };
}
