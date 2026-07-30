import { describe, expect, it } from "vitest";

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
});
