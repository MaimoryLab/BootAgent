import { afterEach, describe, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({
	status: vi.fn(),
	probe: vi.fn(),
	models: vi.fn(),
	install: vi.fn(),
	register: vi.fn(),
	activate: vi.fn(),
	profiles: vi.fn(),
}));

vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/statusservice.js", () => ({
	GetStatus: bridge.status,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/providerservice.js", () => ({
	Probe: bridge.probe,
	ListModels: bridge.models,
	OpenRegistration: bridge.register,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/agentservice.js", () => ({
	Install: bridge.install,
	Activate: bridge.activate,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/profileservice.js", () => ({
	ListProfiles: bridge.profiles,
}));

import { normalizeWailsError, wailsApi } from "./wails";

describe("Wails backend adapter", () => {
	afterEach(() => {
		vi.resetAllMocks();
	});

	it("normalizes a generated status payload without exposing bridge-specific nulls", async () => {
		bridge.status.mockResolvedValue({
			apiVersion: 1,
			platform: { os: "linux", arch: "arm64", shell: "bash" },
			capabilities: { canInstall: { codex: true }, supportedAgentIds: ["codex"] },
			agents: { codex: { installed: true, configured: false, guideOnly: false, config: "", version: null, lockedVersion: "0.1.0", canInstall: true, provider: null, model: null, baseUrl: null, updatedAt: null, detected: null } },
			catalog: [{ id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "0.1.0", protocol: "responses", platforms: ["linux"], platformNote: "", rank: 1 }],
			groups: [{ id: "auto", name: "自动配置" }],
			providers: { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } },
			mirrors: [], paths: {}, backups: {}, profiles: null, activeProfile: null, environment: null, environmentError: null,
		});

		await expect(wailsApi.status()).resolves.toMatchObject({
			platform: { os: "linux", arch: "arm64" },
			agents: { codex: { installed: true, detected: null } },
			profiles: [],
		});
	});

	it("maps provider requests to generated snake_case bindings", async () => {
		bridge.probe.mockResolvedValue({ ok: true, reachable: true, status: 204, message: "ok", error_code: null, retryable: false, protocol: "responses" });
		await expect(wailsApi.probe({ provider: "custom", apiBaseUrl: "https://proxy.test/v1", apiKey: "secret", model: "m", agents: [] })).resolves.toMatchObject({ ok: true, protocol: "responses" });
		expect(bridge.probe).toHaveBeenCalledWith({
			provider: "custom", api_base_url: "https://proxy.test/v1", api_key: "secret", model: "m", agents: null,
		});
	});

	it("fills binding defaults and preserves install null fields", async () => {
		bridge.install.mockResolvedValue({
			ok: true, code: 0,
			results: [{ agent: "codex", status: "configured", config: "", installed: false, version: null, lockedVersion: "0.145.0", retryable: false }],
			log: "", next: "", probe: null, probes: null,
		});
		const result = await wailsApi.install({
			agents: ["codex"], provider: "ppio", api_base_url: "", api_key: "secret", model: "m",
			configure: true, install_agent: false, skip_test: true,
		});
		expect(result.results[0]).toMatchObject({ installed: false, version: null, lockedVersion: "0.145.0" });
		expect(bridge.install).toHaveBeenCalledWith(expect.objectContaining({
			agents: ["codex"], profile_agents: null, profile_id: "", timeout: 180, locked_version: false, latest: false,
		}));
	});

	it("maps activation and registration calls", async () => {
		bridge.activate.mockResolvedValue({ ok: true, agent: "codex", config: "/c", provider: "ppio", model: "m", restart: "r", next: "n" });
		bridge.register.mockResolvedValue({ ok: true, url: "https://ppio.com/", message: "opened" });
		await wailsApi.activateAgent("codex", { provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m" });
		await wailsApi.openRegister("ppio", []);
		expect(bridge.activate).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "codex", profile_id: "", small_fast_model: "" }));
		expect(bridge.register).toHaveBeenCalledWith({ provider: "ppio", agents: null });
	});

	it("restores a structured Go error cause and hides unknown bridge failures", () => {
		expect(normalizeWailsError({ name: "RuntimeError", cause: { error_code: "API_KEY_REJECTED", message: "key rejected", status: 401, retryable: false } })).toMatchObject({
		message: "key rejected", code: "API_KEY_REJECTED", status: 401, retryable: false,
	});
		expect(normalizeWailsError(new Error("internal implementation detail"))).toMatchObject({
		message: "无法调用本机 OneAgent 服务", code: "INTERNAL_ERROR", retryable: true,
	});
	});
});
