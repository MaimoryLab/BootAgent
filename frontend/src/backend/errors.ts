import type { Translate } from "../i18n";
import { failureCopyFor } from "./failureCopy";

export class OneAgentApiError extends Error {
	readonly code: string;
	readonly retryable: boolean;
	readonly status: number;

	constructor(message: string, code: string, retryable: boolean, status: number) {
		super(message);
		this.name = "OneAgentApiError";
		this.code = code;
		this.retryable = retryable;
		this.status = status;
	}
}

export interface FailureDetail {
	message: string;
	code: string;
	retryable: boolean;
	/** The HTTP status behind the failure, when one reached the transport. */
	status?: number;
	/** One line of what to do about it, set only by describeFailure. */
	hint?: string;
}

export function isCancellationError(error: unknown): boolean {
	const name = error && typeof error === "object" ? (error as { name?: unknown }).name : undefined;
	return name === "CancelError" || name === "CancelledRejectionError";
}

/** Normalize any thrown value into the stable frontend error contract. */
export function describeError(error: unknown, fallback: string): FailureDetail {
	if (error instanceof OneAgentApiError) {
		return { message: error.message, code: error.code, retryable: error.retryable, status: error.status };
	}
	return {
		message: error instanceof Error ? error.message : fallback,
		code: "INTERNAL_ERROR",
		retryable: true,
	};
}

/**
 * describeError, with the message replaced by localised copy where a code maps to
 * any.
 *
 * Separate from describeError rather than folded into it: this needs a Translate,
 * which only components have, and describeError is also called from places that
 * just want the code or the retryable flag. Callers that show a message to a user
 * should prefer this one.
 *
 * The English backend message is dropped, not appended. It is written for a
 * maintainer reading a log -- "Cannot reach endpoint: dial tcp: lookup
 * api.example.com: no such host" -- and showing both would leave the user to
 * decide which half to trust.
 */
export function describeFailure(error: unknown, fallback: string, t: Translate): FailureDetail {
	const detail = describeError(error, fallback);
	const copy = failureCopyFor(detail.code, detail.status, t);
	if (!copy) return detail;
	return { ...detail, message: copy.message, hint: copy.hint };
}
