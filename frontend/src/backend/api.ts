import { httpApi } from "../api/client";
import { OneAgentApiError, describeError } from "./errors";

/** The two frontend transports intentionally share one page-facing surface. */
export type BackendApi = typeof httpApi;

export interface BackendLocation {
	protocol?: string;
	hostname?: string;
}

/**
 * Wails v3 uses a custom `wails:` scheme on macOS/Linux and the
 * `wails.localhost` virtual host on Windows. The regular browser GUI keeps
 * using its HTTP adapter; there is no error-triggered fallback from Wails.
 */
export function isWailsRuntime(location: BackendLocation | undefined): boolean {
	return location?.protocol === "wails:" || location?.hostname === "wails.localhost";
}

export function selectBackend(location: BackendLocation | undefined, nativeApi: BackendApi = lazyWailsApi): BackendApi {
	return isWailsRuntime(location) ? nativeApi : httpApi;
}

type WailsModule = typeof import("./wails");
let wailsModule: Promise<WailsModule> | undefined;

function loadWails(): Promise<WailsModule> {
	return (wailsModule ??= import("./wails"));
}

function lazyWailsMethod<Key extends keyof BackendApi>(key: Key): BackendApi[Key] {
	return ((...args: unknown[]) =>
		loadWails().then(({ wailsApi }) => {
			const method = wailsApi[key] as (...values: unknown[]) => unknown;
			return method(...args);
		})) as BackendApi[Key];
}

const lazyWailsApi: BackendApi = {
	status: lazyWailsMethod("status"),
	probe: lazyWailsMethod("probe"),
	models: lazyWailsMethod("models"),
	install: lazyWailsMethod("install"),
	openRegister: lazyWailsMethod("openRegister"),
	activateAgent: lazyWailsMethod("activateAgent"),
};

function currentLocation(): BackendLocation | undefined {
	if (typeof globalThis === "undefined" || !("location" in globalThis)) {
		return undefined;
	}
	return globalThis.location;
}

/**
 * Single page-facing backend. In jsdom and the Python GUI this resolves to
 * `httpApi`; a packaged Wails window resolves to the generated service
 * adapter above.
 */
export const api = selectBackend(currentLocation());

export { OneAgentApiError, describeError };
