import { wailsApi } from "./wails";

/**
 * The single page-facing backend surface.
 *
 * Every call goes through the generated Wails bindings to a registered Go
 * service. There is no HTTP transport and no runtime transport selection: the
 * desktop app does not open a business port, so a bridge failure is a real
 * failure rather than something to retry over a second channel.
 */
export type BackendApi = typeof wailsApi;

export const api: BackendApi = wailsApi;

export { describeError, describeFailure, isCancellationError } from "./errors";
