import { afterEach, describe, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({
	status: vi.fn(),
	probe: vi.fn(),
	models: vi.fn(),
	install: vi.fn(),
	register: vi.fn(),
	activate: vi.fn(),
	profiles: vi.fn(),
	saveProfile: vi.fn(),
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
	SaveProfile: bridge.saveProfile,
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

	// The generated bindings type these payloads, but the adapter is the last
	// boundary before React state: a missing or wrongly typed field has to become
	// a safe default rather than an undefined that renders as "undefined".
	it("defaults every field when the bridge returns an empty payload", async () => {
		bridge.status.mockResolvedValue({});
		await expect(wailsApi.status()).resolves.toMatchObject({
			// The wire contract's only version so far; an absent field means v1
			// rather than an unusable 0.
			apiVersion: 1,
			platform: { os: "linux", arch: "", shell: "" },
			capabilities: { canInstall: {}, supportedAgentIds: [] },
			agents: {},
			catalog: [],
			groups: [],
			providers: {},
			mirrors: [],
			paths: {},
			backups: {},
			profiles: [],
			activeProfile: null,
			environment: null,
			environmentError: null,
		});
	});

	it("coerces unknown enum values and wrongly typed fields to safe defaults", async () => {
		bridge.status.mockResolvedValue({
			apiVersion: "not-a-number",
			platform: { os: "plan9", arch: 7, shell: null },
			capabilities: { canInstall: { codex: "yes", opencode: true }, supportedAgentIds: ["codex", 9] },
			agents: {
				codex: {
					installed: "yes", configured: null, guideOnly: 0, config: 5,
					version: 1, lockedVersion: "0.1.0", canInstall: true,
					provider: undefined, model: {}, baseUrl: [], updatedAt: "t",
					detected: { baseUrl: 1, model: null, managedByOneAgent: "true", unreadable: "why" },
				},
			},
			catalog: [{ id: "x", name: "X", group: "nope", configMode: "guide", guideOnly: true, lockedVersion: null, protocol: "smoke-signals", platforms: ["freebsd"], platformNote: null, rank: "3" }],
			groups: [{ id: "auto", name: "自动配置" }],
			mirrors: [{ id: "m", name: "M", registry: "r", upstream: "u", note: "n" }],
			paths: { codex: "/c", bogus: 5 },
			backups: { codex: true, bogus: "yes" },
			profiles: "not-an-array",
		});

		const status = await wailsApi.status();
		// An unrecognized OS must not leak through as a value the UI cannot render.
		expect(status.platform).toMatchObject({ os: "linux", arch: "", shell: "" });
		expect(status.apiVersion).toBe(1);
		expect(status.capabilities.canInstall).toEqual({ opencode: true });
		expect(status.capabilities.supportedAgentIds).toEqual(["codex"]);
		expect(status.agents.codex).toMatchObject({
			installed: false, configured: false, guideOnly: false, config: "",
			version: null, provider: null, model: null, baseUrl: null,
		});
		expect(status.agents.codex.detected).toMatchObject({
			baseUrl: "", model: "", managedByOneAgent: false, unreadable: "why",
		});
		expect(status.catalog[0]).toMatchObject({ group: "auto", configMode: "guide", protocol: null, rank: 0 });
		expect(status.paths).toEqual({ codex: "/c" });
		expect(status.backups).toEqual({ codex: true });
		expect(status.profiles).toEqual([]);
	});

	it("keeps a per-protocol probe map and drops protocols it does not know", async () => {
		bridge.probe.mockResolvedValue({
			ok: false, reachable: true, status: 404, message: "no", error_code: "PROTOCOL_UNSUPPORTED", retryable: false,
			protocols: {
				responses: { ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false, protocol: "responses" },
				anthropic: { ok: false, reachable: true, status: 404, message: "no", error_code: "PROTOCOL_UNSUPPORTED", retryable: false },
				telepathy: { ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false },
			},
		});
		const probe = await wailsApi.probe({ provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m", agents: ["codex"] });
		expect(Object.keys(probe.protocols ?? {}).sort()).toEqual(["anthropic", "responses"]);
		expect(probe.protocols?.responses).toMatchObject({ ok: true, protocol: "responses" });
		expect(bridge.probe).toHaveBeenCalledWith(expect.objectContaining({ agents: ["codex"] }));
	});

	it("returns a model list and a probe verdict from one call", async () => {
		bridge.models.mockResolvedValue({
			ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false,
			models: ["model-a", 7, "model-b"],
		});
		const models = await wailsApi.models({ provider: "ppio", apiBaseUrl: "", apiKey: "secret" });
		expect(models.models).toEqual(["model-a", "model-b"]);
		expect(models).toMatchObject({ ok: true, status: 200 });
	});

	it("omits install fields the backend left out and keeps explicit nulls", async () => {
		// Field presence is the contract: an absent version means "not reported",
		// a null version means "reported as unknown". Collapsing them would change
		// what the wizard shows.
		bridge.install.mockResolvedValue({
			ok: false, code: 4,
			results: [
				{ agent: "codex", status: "failed", code: 4, error_code: "AGENT_INSTALL_FAILED", message: "npm failed", retryable: true },
				{ agent: "aider", status: "guide-only", message: "install from docs", retryable: false },
				{ agent: "opencode", status: "installed", installed: true, version: null, lockedVersion: null, registry: "npmmirror", config: "/c", retryable: false },
			],
			log: "log", next: "next",
			probe: { ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false },
			probes: { responses: { ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false } },
		});
		const result = await wailsApi.install({
			agents: ["codex"], provider: "ppio", api_key: "secret", model: "m",
			configure: true, install_agent: true, skip_test: false,
			profile_agents: ["codex"], profile_id: "team", registry: "npmmirror",
			locked_version: true, timeout: 90, small_fast_model: "fast",
		});
		expect(result.results[0]).toEqual({ agent: "codex", status: "failed", code: 4, error_code: "AGENT_INSTALL_FAILED", message: "npm failed", retryable: true });
		expect("version" in result.results[1]).toBe(false);
		expect("installed" in result.results[1]).toBe(false);
		expect(result.results[2]).toMatchObject({ version: null, lockedVersion: null, registry: "npmmirror" });
		expect(result.probe).toMatchObject({ ok: true });
		expect(result.probes?.responses).toMatchObject({ ok: true });
		expect(bridge.install).toHaveBeenCalledWith(expect.objectContaining({
			profile_agents: ["codex"], profile_id: "team", registry: "npmmirror",
			locked_version: true, timeout: 90, small_fast_model: "fast",
		}));
	});

	it("treats a missing install envelope as an empty result rather than throwing", async () => {
		bridge.install.mockResolvedValue({ ok: true, code: 0, results: null, log: null, next: null, probe: undefined, probes: null });
		const result = await wailsApi.install({
			agents: ["codex"], provider: "ppio", api_key: "secret", model: "m",
			configure: true, install_agent: false, skip_test: true,
		});
		expect(result).toMatchObject({ results: [], log: "", next: "", probe: null });
		expect("probes" in result).toBe(false);
	});

	it("projects profiles without ever carrying a credential field", async () => {
		bridge.profiles.mockResolvedValue([
			{ id: "team", label: "Team", provider: "ppio", baseUrl: null, model: "m", agentIds: ["codex"], activatedAt: "t", hasKey: true },
			{ id: "solo", provider: "novita", baseUrl: "https://api.novita.ai/openai", model: null, agentIds: null, activatedAt: null, hasKey: false },
		]);
		const profiles = await wailsApi.listProfiles();
		// A label defaults to the id rather than rendering as blank.
		expect(profiles[1]).toEqual({
			id: "solo", label: "solo", provider: "novita",
			baseUrl: "https://api.novita.ai/openai", model: null, agentIds: [],
			activatedAt: null, hasKey: false,
		});
		expect(profiles[0]).toMatchObject({ hasKey: true });
		for (const profile of profiles) {
			expect(Object.keys(profile)).not.toContain("apiKey");
			expect(Object.keys(profile)).not.toContain("api_key");
		}
	});

	it("treats an absent profile list as empty", async () => {
		bridge.profiles.mockResolvedValue(null);
		await expect(wailsApi.listProfiles()).resolves.toEqual([]);
	});

	it("sends a saved profile as snake_case and returns the public summary", async () => {
		const save = bridge.saveProfile;
		save.mockResolvedValue({ id: "team", label: "Team", provider: "ppio", baseUrl: null, model: "m", agentIds: ["codex"], activatedAt: "t", hasKey: true });
		const summary = await wailsApi.saveProfile({
			id: "team", label: "Team", provider: "ppio", apiBaseUrl: "",
			apiKey: "secret", model: "m", configMode: "provider", agentIds: ["codex"],
		});
		expect(summary).toMatchObject({ id: "team", hasKey: true });
		expect(save).toHaveBeenCalledWith({
			id: "team", label: "Team", provider: "ppio", api_base_url: "",
			api_key: "secret", model: "m", config_mode: "provider", agent_ids: ["codex"],
		});
	});

	it("reads a structured cause that arrived as a JSON string", async () => {
		// MarshalError may hand the cause across as text; the stable error code
		// has to survive that, and unparseable text must not become the message.
		expect(normalizeWailsError({ cause: '{"error_code":"TIMEOUT","message":"probe timed out","status":504,"retryable":true}' })).toMatchObject({
			message: "probe timed out", code: "TIMEOUT", status: 504, retryable: true,
		});
		expect(normalizeWailsError({ cause: "not json at all" })).toMatchObject({
			message: "无法调用本机 OneAgent 服务", code: "INTERNAL_ERROR", status: 500,
		});
		const partial = normalizeWailsError({ cause: { message: "partial" } });
		expect(partial).toMatchObject({ message: "partial", code: "INTERNAL_ERROR", status: 500, retryable: true });
	});

	it("passes an already-normalized error through unchanged", async () => {
		const { OneAgentApiError } = await import("./errors");
		const original = new OneAgentApiError("already mapped", "INVALID_REQUEST", false, 400);
		expect(normalizeWailsError(original)).toBe(original);
	});

	it("wraps a rejected binding call and keeps the API key out of the message", async () => {
		bridge.status.mockRejectedValue({ cause: { error_code: "PROVIDER_UNREACHABLE", message: "cannot reach provider", status: 502, retryable: true } });
		await expect(wailsApi.status()).rejects.toMatchObject({ code: "PROVIDER_UNREACHABLE", retryable: true });

		bridge.probe.mockRejectedValue(new Error("dial tcp: secret-key-value in transport detail"));
		const failure = await wailsApi.probe({ provider: "ppio", apiBaseUrl: "", apiKey: "secret-key-value", model: "m" }).catch((error) => error);
		expect(failure.message).not.toContain("secret-key-value");
		expect(failure.code).toBe("INTERNAL_ERROR");
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
