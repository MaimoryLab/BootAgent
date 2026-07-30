import * as AgentService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/agentservice.js";
import * as ProfileService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/profileservice.js";
import * as ProviderService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/providerservice.js";
import * as StatusService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/statusservice.js";
import { OneAgentApiError } from "./errors";
import type {
	ActivateAgentResponse,
	AgentCatalogItem,
	AgentInstallResult,
	AgentStatus,
	DetectedConfig,
	EnvironmentProfile,
	InstallRequest,
	InstallResponse,
	ModelsResponse,
	PlatformId,
	AgentGroupId,
	ProbeResponse,
	ProfileSummary,
	ProviderId,
	StatusResponse,
} from "../types/api";

export { OneAgentApiError, describeError } from "./errors";

type RecordValue = Record<string, unknown>;

function record(value: unknown): RecordValue {
	return value !== null && typeof value === "object" ? (value as RecordValue) : {};
}

function stringValue(value: unknown, fallback = ""): string {
	return typeof value === "string" ? value : fallback;
}

function nullableString(value: unknown): string | null {
	return typeof value === "string" ? value : null;
}

function optionalNullableString(source: RecordValue, key: string): string | null | undefined {
	if (!(key in source)) {
		return undefined;
	}
	return nullableString(source[key]);
}

function booleanValue(value: unknown, fallback = false): boolean {
	return typeof value === "boolean" ? value : fallback;
}

function numberValue(value: unknown, fallback = 0): number {
	return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringArray(value: unknown): string[] {
	return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function stringMap(value: unknown): Record<string, string> {
	const source = record(value);
	return Object.fromEntries(
		Object.entries(source).filter((entry): entry is [string, string] => typeof entry[1] === "string"),
	);
}

function booleanMap(value: unknown): Record<string, boolean> {
	const source = record(value);
	return Object.fromEntries(
		Object.entries(source).filter((entry): entry is [string, boolean] => typeof entry[1] === "boolean"),
	);
}

function protocolId(value: unknown): ProbeResponse["protocol"] {
	return value === "openai" || value === "anthropic" || value === "responses" ? value : undefined;
}

function platformId(value: unknown): PlatformId {
	return value === "macos" || value === "windows" || value === "linux" ? value : "linux";
}

function groupId(value: unknown): AgentGroupId {
	return value === "auto" || value === "gateway" || value === "platform" || value === "ide" ? value : "auto";
}

function normalizeDetected(value: unknown): DetectedConfig | null {
	if (value === null || value === undefined) {
		return null;
	}
	const source = record(value);
	return {
		baseUrl: stringValue(source.baseUrl),
		model: stringValue(source.model),
		managedByOneAgent: booleanValue(source.managedByOneAgent),
		unreadable: nullableString(source.unreadable),
	};
}

function normalizeAgentStatus(value: unknown): AgentStatus {
	const source = record(value);
	return {
		installed: booleanValue(source.installed),
		configured: booleanValue(source.configured),
		guideOnly: booleanValue(source.guideOnly),
		config: stringValue(source.config),
		version: nullableString(source.version),
		lockedVersion: nullableString(source.lockedVersion),
		canInstall: booleanValue(source.canInstall),
		provider: nullableString(source.provider),
		model: nullableString(source.model),
		baseUrl: nullableString(source.baseUrl),
		updatedAt: nullableString(source.updatedAt),
		detected: normalizeDetected(source.detected),
	};
}

function normalizeCatalogItem(value: unknown): AgentCatalogItem {
	const source = record(value);
	const configMode = source.configMode;
	return {
		id: stringValue(source.id),
		name: stringValue(source.name),
		group: groupId(source.group),
		configMode: configMode === "guide" ? "guide" : "auto",
		guideOnly: booleanValue(source.guideOnly),
		lockedVersion: nullableString(source.lockedVersion),
		protocol: protocolId(source.protocol) ?? null,
		platforms: stringArray(source.platforms) as AgentCatalogItem["platforms"],
		platformNote: stringValue(source.platformNote),
		rank: numberValue(source.rank),
	};
}

function normalizeStatus(value: unknown): StatusResponse {
	const source = record(value);
	const rawAgents = record(source.agents);
	const agents: Record<string, AgentStatus> = {};
	for (const [id, item] of Object.entries(rawAgents)) {
		agents[id] = normalizeAgentStatus(item);
	}
	const rawProviders = record(source.providers);
	const providers: StatusResponse["providers"] = {};
	for (const [id, item] of Object.entries(rawProviders)) {
		const provider = record(item);
		providers[id] = {
			name: stringValue(provider.name),
			home: stringValue(provider.home),
			base_url: stringValue(provider.base_url),
			...(typeof provider.anthropic_base_url === "string" ? { anthropic_base_url: provider.anthropic_base_url } : {}),
		};
	}
	const rawMirrors = Array.isArray(source.mirrors) ? source.mirrors : [];
	const mirrors = rawMirrors.map((item) => {
		const mirror = record(item);
		return {
			id: stringValue(mirror.id),
			name: stringValue(mirror.name),
			registry: stringValue(mirror.registry),
			upstream: stringValue(mirror.upstream),
			note: stringValue(mirror.note),
		};
	});
	const rawGroups = Array.isArray(source.groups) ? source.groups : [];
	const groups = rawGroups.map((item) => {
		const group = record(item);
		return {
			id: groupId(group.id),
			name: stringValue(group.name),
		};
	});
	const environment = source.environment && typeof source.environment === "object"
		? (source.environment as EnvironmentProfile)
		: null;
	return {
		apiVersion: numberValue(source.apiVersion, 1),
		platform: {
			os: platformId(record(source.platform).os),
			arch: stringValue(record(source.platform).arch),
			shell: stringValue(record(source.platform).shell),
		},
		capabilities: {
			canInstall: booleanMap(record(source.capabilities).canInstall),
			supportedAgentIds: stringArray(record(source.capabilities).supportedAgentIds),
		},
		agents,
		catalog: (Array.isArray(source.catalog) ? source.catalog : []).map(normalizeCatalogItem),
		groups,
		providers,
		mirrors,
		paths: stringMap(source.paths),
		backups: booleanMap(source.backups),
		environment,
		environmentError: nullableString(source.environmentError),
		profiles: Array.isArray(source.profiles) ? source.profiles.map(normalizeProfile) : [],
		activeProfile: nullableString(source.activeProfile),
	};
}

function normalizeProfile(value: unknown): ProfileSummary {
	const source = record(value);
	return {
		id: stringValue(source.id),
		label: stringValue(source.label, stringValue(source.id)),
		provider: stringValue(source.provider),
		baseUrl: nullableString(source.baseUrl),
		model: nullableString(source.model),
		agentIds: stringArray(source.agentIds),
		activatedAt: nullableString(source.activatedAt),
		hasKey: booleanValue(source.hasKey),
	};
}

function normalizeProbe(value: unknown): ProbeResponse {
	const source = record(value);
	const protocols: Partial<Record<NonNullable<ProbeResponse["protocol"]>, ProbeResponse>> = {};
	const rawProtocols = record(source.protocols);
	for (const [key, item] of Object.entries(rawProtocols)) {
		if (key === "openai" || key === "anthropic" || key === "responses") {
			protocols[key] = normalizeProbe(item);
		}
	}
	return {
		ok: booleanValue(source.ok),
		reachable: booleanValue(source.reachable),
		status: numberValue(source.status),
		message: stringValue(source.message),
		error_code: nullableString(source.error_code),
		retryable: booleanValue(source.retryable),
		...(protocolId(source.protocol) ? { protocol: protocolId(source.protocol) } : {}),
		...(Object.keys(protocols).length ? { protocols } : {}),
	};
}

function normalizeInstallResult(value: unknown): AgentInstallResult {
	const source = record(value);
	return {
		agent: stringValue(source.agent),
		status: stringValue(source.status) as AgentInstallResult["status"],
		...(typeof source.installed === "boolean" ? { installed: source.installed } : {}),
		...("version" in source ? { version: optionalNullableString(source, "version") } : {}),
		...("lockedVersion" in source ? { lockedVersion: optionalNullableString(source, "lockedVersion") } : {}),
		...(typeof source.registry === "string" ? { registry: source.registry } : {}),
		...(typeof source.config === "string" ? { config: source.config } : {}),
		...(typeof source.code === "number" ? { code: source.code } : {}),
		...(typeof source.error_code === "string" ? { error_code: source.error_code } : {}),
		...(typeof source.message === "string" ? { message: source.message } : {}),
		retryable: booleanValue(source.retryable),
	};
}

function normalizeInstall(value: unknown): InstallResponse {
	const source = record(value);
	const rawProbes = record(source.probes);
	const probes: Partial<Record<NonNullable<ProbeResponse["protocol"]>, ProbeResponse>> = {};
	for (const [key, item] of Object.entries(rawProbes)) {
		if (key === "openai" || key === "anthropic" || key === "responses") {
			probes[key] = normalizeProbe(item);
		}
	}
	return {
		ok: booleanValue(source.ok),
		code: numberValue(source.code),
		results: Array.isArray(source.results) ? source.results.map(normalizeInstallResult) : [],
		log: stringValue(source.log),
		next: stringValue(source.next),
		probe: source.probe === null || source.probe === undefined ? null : normalizeProbe(source.probe),
		...(Object.keys(probes).length ? { probes } : {}),
	};
}

function normalizeModels(value: unknown): ModelsResponse {
	const source = record(value);
	return {
		...normalizeProbe(source),
		models: stringArray(source.models),
	};
}

function parseCause(value: unknown): RecordValue {
	if (typeof value === "string") {
		try {
			return record(JSON.parse(value));
		} catch {
			return {};
		}
	}
	return record(value);
}

/** Convert a Wails bridge rejection into the stable frontend error contract. */
export function normalizeWailsError(error: unknown): OneAgentApiError {
	if (error instanceof OneAgentApiError) {
		return error;
	}
	const source = record(error);
	const cause = parseCause(source.cause);
	const hasCause = Object.keys(cause).length > 0;
	const message = hasCause
		? stringValue(cause.message, "OneAgent request failed")
		: "无法调用本机 OneAgent 服务";
	const code = hasCause ? stringValue(cause.error_code, "INTERNAL_ERROR") : "INTERNAL_ERROR";
	const status = hasCause ? numberValue(cause.status, 500) : 500;
	const retryable = hasCause ? booleanValue(cause.retryable, true) : true;
	return new OneAgentApiError(message, code, retryable, status);
}

async function call<T>(operation: () => PromiseLike<T>): Promise<T> {
	try {
		return await operation();
	} catch (error) {
		throw normalizeWailsError(error);
	}
}

export const wailsApi = {
	status: () => call(() => StatusService.GetStatus()).then(normalizeStatus),
	probe: (input: {
		provider: ProviderId;
		apiBaseUrl: string;
		apiKey: string;
		model: string;
		agents?: string[];
	}) =>
		call(() =>
			ProviderService.Probe({
				provider: input.provider,
				api_base_url: input.apiBaseUrl,
				api_key: input.apiKey,
				model: input.model,
				agents: input.agents?.length ? input.agents : null,
			}),
		).then(normalizeProbe),
	models: (input: { provider: ProviderId; apiBaseUrl: string; apiKey: string }) =>
		call(() =>
			ProviderService.ListModels({
				provider: input.provider,
				api_base_url: input.apiBaseUrl,
				api_key: input.apiKey,
			}),
		).then(normalizeModels),
	install: (input: InstallRequest) =>
		call(() =>
			AgentService.Install({
				agents: input.agents ?? null,
				profile_agents: input.profile_agents ?? null,
				provider: input.provider,
				api_base_url: input.api_base_url ?? "",
				api_key: input.api_key,
				model: input.model,
				small_fast_model: input.small_fast_model ?? "",
				profile_id: input.profile_id ?? "",
				configure: input.configure,
				install_agent: input.install_agent,
				locked_version: input.locked_version ?? false,
				latest: input.latest ?? false,
				skip_test: input.skip_test,
				registry: input.registry ?? "",
				timeout: input.timeout ?? 180,
			}),
		).then(normalizeInstall),
	openRegister: (provider: Exclude<ProviderId, "custom">, agents: string[]) =>
		call(() => ProviderService.OpenRegistration({ provider, agents: agents.length ? agents : null })),
	activateAgent: (
		agentId: string,
		input: {
			provider: ProviderId;
			apiBaseUrl: string;
			apiKey: string;
			model: string;
			profileId?: string;
			smallFastModel?: string;
		},
	) =>
		call(() =>
			AgentService.Activate({
				agent_id: agentId,
				provider: input.provider,
				api_base_url: input.apiBaseUrl,
				api_key: input.apiKey,
				model: input.model,
				profile_id: input.profileId ?? "",
				small_fast_model: input.smallFastModel ?? "",
			}),
		) as Promise<ActivateAgentResponse>,
	listProfiles: () => call(() => ProfileService.ListProfiles()).then((value) => (value ?? []).map(normalizeProfile)),
	saveProfile: (input: {
		id: string;
		label: string;
		provider: ProviderId;
		apiBaseUrl: string;
		apiKey: string;
		model: string;
		configMode: string;
		agentIds: string[];
	}) =>
		call(() =>
			ProfileService.SaveProfile({
				id: input.id,
				label: input.label,
				provider: input.provider,
				api_base_url: input.apiBaseUrl,
				api_key: input.apiKey,
				model: input.model,
				config_mode: input.configMode,
				agent_ids: input.agentIds,
			}),
		).then(normalizeProfile),
};

export type WailsApi = typeof wailsApi;
