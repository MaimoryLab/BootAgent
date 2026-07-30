import { describe, expect, it, vi } from "vitest";

import { isWailsRuntime, selectBackend, type BackendApi } from "./api";

describe("backend transport selection", () => {
	it("recognizes the native Wails schemes", () => {
		expect(isWailsRuntime({ protocol: "wails:", hostname: "localhost" })).toBe(true);
		expect(isWailsRuntime({ protocol: "http:", hostname: "wails.localhost" })).toBe(true);
	});

	it("keeps ordinary HTTP hosts on the legacy adapter", () => {
		expect(isWailsRuntime({ protocol: "http:", hostname: "127.0.0.1" })).toBe(false);
		expect(isWailsRuntime({ protocol: "https:", hostname: "example.test" })).toBe(false);
		const nativeApi = {} as BackendApi;
		expect(selectBackend({ protocol: "http:", hostname: "127.0.0.1" }, nativeApi)).not.toBe(nativeApi);
		expect(selectBackend({ protocol: "wails:", hostname: "localhost" }, nativeApi)).toBe(nativeApi);
	});

	it("treats an unknown location as the browser transport", () => {
		// jsdom and the Python GUI both land here. Defaulting to the native
		// adapter instead would make every page fail on a missing bridge.
		expect(isWailsRuntime(undefined)).toBe(false);
		expect(isWailsRuntime({})).toBe(false);
		const nativeApi = {} as BackendApi;
		expect(selectBackend(undefined, nativeApi)).not.toBe(nativeApi);
	});

	it("exposes the HTTP adapter under the shared page-facing surface", async () => {
		// Pages import `api` and never choose a transport themselves, so both
		// adapters have to present the same method set.
		const { api } = await import("./api");
		const { wailsApi } = await import("./wails");
		expect(Object.keys(api).sort()).toEqual(Object.keys(wailsApi).sort().filter((key) => key in api));
		for (const method of ["status", "probe", "models", "install", "openRegister", "activateAgent"] as const) {
			expect(typeof api[method]).toBe("function");
		}
	});

	it("loads the native adapter lazily and only on first use", async () => {
		// The Wails runtime import must not be evaluated in the browser build, so
		// the native surface is a set of thunks until one is actually called.
		const nativeStatus = { apiVersion: 1 };
		const loaded = vi.fn();
		vi.doMock("./wails", () => {
			loaded();
			return { wailsApi: { status: () => Promise.resolve(nativeStatus) } };
		});
		vi.resetModules();
		const { selectBackend: freshSelect } = await import("./api");
		const native = freshSelect({ protocol: "wails:", hostname: "localhost" });
		expect(loaded).not.toHaveBeenCalled();
		await expect(native.status()).resolves.toBe(nativeStatus);
		expect(loaded).toHaveBeenCalledTimes(1);
		// A second call reuses the resolved module rather than importing again.
		await native.status();
		expect(loaded).toHaveBeenCalledTimes(1);
		vi.doUnmock("./wails");
		vi.resetModules();
	});
});
