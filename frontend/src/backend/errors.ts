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
}

export function isCancellationError(error: unknown): boolean {
	const name = error && typeof error === "object" ? (error as { name?: unknown }).name : undefined;
	return name === "CancelError" || name === "CancelledRejectionError";
}

/** Normalize any thrown value into the stable frontend error contract. */
export function describeError(error: unknown, fallback: string): FailureDetail {
	if (error instanceof OneAgentApiError) {
		return { message: error.message, code: error.code, retryable: error.retryable };
	}
	return {
		message: error instanceof Error ? error.message : fallback,
		code: "INTERNAL_ERROR",
		retryable: true,
	};
}
