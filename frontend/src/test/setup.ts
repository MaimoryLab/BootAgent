import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

// vitest runs without injected globals, so RTL's built-in auto-cleanup (which
// registers on a global afterEach) never engages; do it explicitly.
afterEach(() => cleanup());

/**
 * Node exposes its own experimental `localStorage`, which shadows the jsdom
 * implementation and reads as `undefined` unless the process was started with
 * `--localstorage-file`. Tests assert that an API key never reaches browser
 * storage, and against `undefined` those assertions cannot fail for the right
 * reason -- `JSON.stringify(undefined)` is itself `undefined`, so a matcher
 * either errors or vacuously passes. Installing a real in-memory Storage makes
 * the assertions meaningful: a leak now has somewhere to be found.
 */
function memoryStorage(): Storage {
	let entries = new Map<string, string>();
	const storage: Storage = {
		get length() {
			return entries.size;
		},
		clear() {
			entries = new Map();
		},
		getItem(key: string) {
			return entries.has(key) ? (entries.get(key) as string) : null;
		},
		key(index: number) {
			return [...entries.keys()][index] ?? null;
		},
		removeItem(key: string) {
			entries.delete(key);
		},
		setItem(key: string, value: string) {
			entries.set(String(key), String(value));
		},
	};
	// JSON.stringify on a real Storage serializes its own enumerable keys. The
	// object above keeps entries in a Map, so expose them the same way.
	return new Proxy(storage, {
		get(target, property, receiver) {
			if (typeof property === "string" && !(property in target)) {
				return entries.get(property);
			}
			return Reflect.get(target, property, receiver);
		},
		ownKeys() {
			return [...entries.keys()];
		},
		getOwnPropertyDescriptor(_target, property) {
			if (typeof property === "string" && entries.has(property)) {
				return { value: entries.get(property), enumerable: true, configurable: true, writable: true };
			}
			return undefined;
		},
	});
}

function installStorage(name: "localStorage" | "sessionStorage"): void {
	const storage = memoryStorage();
	for (const target of [window, globalThis]) {
		Object.defineProperty(target, name, { value: storage, configurable: true, writable: true });
	}
}

beforeEach(() => {
	installStorage("localStorage");
	installStorage("sessionStorage");
});

/**
 * jsdom 30 ships no HTMLDialogElement methods at all -- `show`, `showModal`,
 * `close` and `requestClose` are all undefined, though the `open` property does
 * reflect the attribute. Modals call showModal() because that is what puts them
 * on the top layer, so without this every test rendering one throws.
 *
 * There is no top layer to emulate here, and jsdom does no layout, so this
 * tracks the observable state a test can assert on: `open`, the implicit
 * "dialog" role that follows it, and the close/cancel events. Whether the
 * element truly escapes its stacking context is a browser question, which is
 * why the E2E suite is what covers position.
 */
const dialog = window.HTMLDialogElement.prototype;
if (typeof dialog.showModal !== "function") {
	dialog.show = function show(this: HTMLDialogElement) {
		this.setAttribute("open", "");
	};
	dialog.showModal = function showModal(this: HTMLDialogElement) {
		this.setAttribute("open", "");
	};
	dialog.close = function close(this: HTMLDialogElement, returnValue?: string) {
		if (!this.hasAttribute("open")) return;
		this.removeAttribute("open");
		if (returnValue !== undefined) this.returnValue = returnValue;
		this.dispatchEvent(new window.Event("close"));
	};
	dialog.requestClose = function requestClose(this: HTMLDialogElement, returnValue?: string) {
		if (!this.hasAttribute("open")) return;
		// Cancel is the event Esc fires, and it is preventable; only close when
		// nothing prevented it, as the platform does.
		if (this.dispatchEvent(new window.Event("cancel", { cancelable: true }))) this.close(returnValue);
	};
}
