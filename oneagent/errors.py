from __future__ import annotations


EXIT_CODES = {
    "INVALID_REQUEST": 2,
    "INVALID_ORIGIN": 2,
    "PREREQUISITE_MISSING": 3,
    "AGENT_INSTALL_FAILED": 4,
    "CONFIG_WRITE_FAILED": 5,
    "API_KEY_REJECTED": 6,
    "PROVIDER_UNREACHABLE": 6,
    "MODELS_UNSUPPORTED": 6,
    "TIMEOUT": 8,
    "INTERNAL_ERROR": 10,
}


class OneAgentError(Exception):
    def __init__(
        self,
        code: str,
        message: str,
        *,
        retryable: bool = False,
        status: int = 400,
        exit_code: int | None = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.message = message
        self.retryable = retryable
        self.status = status
        self.exit_code = exit_code if exit_code is not None else EXIT_CODES.get(code, 10)

    def to_dict(self) -> dict[str, object]:
        return {
            "ok": False,
            "error": self.message,
            "message": self.message,
            "status": self.status,
            "error_code": self.code,
            "retryable": self.retryable,
        }
