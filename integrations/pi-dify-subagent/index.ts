/**
 * dify-subagent — delegate self-contained, one-shot subtasks to a Dify2API-backed model.
 *
 * Registers:
 *  - tool `dify-subagent`: send exactly one system + one user message (blocking)
 *    to the Dify2API gateway and return the model's text.
 *  - command `/dify-subagent`: configure the gateway connection (URL + API key,
 *    validated by fetching the model list) and per-service model selection.
 *
 * Presets are markdown files with frontmatter in presets/*.md (re-read per call).
 * Config lives in <extension-dir>/config.json (mode 0600). Zero external deps.
 */

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { type ExtensionAPI, getAgentDir } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

type SystemPromptPolicy = "locked" | "optional";

interface Preset {
	name: string;
	description: string;
	model?: string;
	service?: string;
	systemPromptPolicy: SystemPromptPolicy;
	timeoutMs?: number;
	resultLimit?: number;
	systemPrompt: string;
}

interface PluginConfig {
	baseUrl?: string;
	apiKey?: string;
	/** service name (e.g. "general-preview") -> full model id (e.g. "[general-preview]gpt-5.5") */
	serviceModels?: Record<string, string>;
}

interface ModelListState {
	baseUrl: string;
	models: string[];
}

const DEFAULTS = {
	baseUrl: "http://localhost:10086/v1",
	timeoutMs: 300_000,
	maxTaskChars: 100_000,
	resultLimit: 4_000,
	timeoutFloorMs: 1_000,
	timeoutCeilMs: 900_000,
	previewChars: 500,
	preset: "general-preview",
	tmpSubdir: "dify-subagent",
	modelsFetchTimeoutMs: 15_000,
} as const;

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

function env(name: string): string | undefined {
	const v = process.env[name];
	return v && v.trim() ? v.trim() : undefined;
}

function envInt(name: string): number | undefined {
	const v = env(name);
	if (!v) return undefined;
	const n = Number(v);
	return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined;
}

function clampTimeout(ms: number): number {
	return Math.min(Math.max(Math.floor(ms), DEFAULTS.timeoutFloorMs), DEFAULTS.timeoutCeilMs);
}

/** Extract the service prefix from a "[service]backend-model" id; null if not bracketed. */
function serviceOf(modelId: string): string | null {
	const m = modelId.match(/^\[([^\]]+)\]/);
	return m ? m[1] : null;
}

function normalizeBaseUrl(raw: string): string {
	return raw.trim().replace(/\/+$/, "");
}

function maskKey(key: string): string {
	return `****(len ${key.length})`;
}

// ---------------------------------------------------------------------------
// config file
// ---------------------------------------------------------------------------

function configPath(): string {
	return path.join(getAgentDir(), "extensions", "dify-subagent", "config.json");
}

function readConfig(): PluginConfig {
	try {
		const raw = fs.readFileSync(configPath(), "utf8");
		const cfg = JSON.parse(raw);
		return typeof cfg === "object" && cfg !== null ? (cfg as PluginConfig) : {};
	} catch {
		return {};
	}
}

function writeConfig(cfg: PluginConfig): void {
	const p = configPath();
	fs.mkdirSync(path.dirname(p), { recursive: true });
	fs.writeFileSync(p, JSON.stringify(cfg, null, 2) + "\n", { encoding: "utf8", mode: 0o600 });
}

/** Effective settings: config.json > env > built-in default. */
function effectiveBaseUrl(cfg: PluginConfig): string {
	return normalizeBaseUrl(cfg.baseUrl || env("DIFY2API_BASE_URL") || DEFAULTS.baseUrl);
}

function effectiveApiKey(cfg: PluginConfig): string | undefined {
	return cfg.apiKey || env("DIFY2API_API_KEY");
}

// ---------------------------------------------------------------------------
// model list (fetched from the gateway, cached per baseUrl)
// ---------------------------------------------------------------------------

let modelListCache: ModelListState | null = null;

interface FetchModelsResult {
	ok: boolean;
	models: string[];
	error?: string;
}

async function fetchModelList(baseUrl: string, apiKey: string | undefined): Promise<FetchModelsResult> {
	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), DEFAULTS.modelsFetchTimeoutMs);
	try {
		const resp = await fetch(`${baseUrl}/models`, {
			headers: apiKey ? { Authorization: `Bearer ${apiKey}` } : {},
			signal: controller.signal,
		});
		if (!resp.ok) {
			const body = (await resp.text().catch(() => "")).slice(0, 300);
			return { ok: false, models: [], error: `HTTP ${resp.status}${body ? `: ${body}` : ""}` };
		}
		const data = (await resp.json()) as { data?: { id?: string }[] };
		const models = (data.data ?? []).map((m) => m.id).filter((id): id is string => typeof id === "string");
		return { ok: true, models };
	} catch (e) {
		return { ok: false, models: [], error: `network error: ${(e as Error)?.message ?? e}` };
	} finally {
		clearTimeout(timer);
	}
}

/** Cached model list; refreshes when baseUrl changed or force=true. */
async function getModelList(baseUrl: string, apiKey: string | undefined, force = false): Promise<FetchModelsResult> {
	if (!force && modelListCache && modelListCache.baseUrl === baseUrl) {
		return { ok: true, models: modelListCache.models };
	}
	const result = await fetchModelList(baseUrl, apiKey);
	if (result.ok) modelListCache = { baseUrl, models: result.models };
	return result;
}

function groupByService(models: string[]): Map<string, string[]> {
	const groups = new Map<string, string[]>();
	for (const id of models) {
		const svc = serviceOf(id);
		if (!svc) continue;
		const list = groups.get(svc) ?? [];
		list.push(id);
		groups.set(svc, list);
	}
	return groups;
}

function randomPick<T>(items: T[]): T {
	return items[Math.floor(Math.random() * items.length)];
}

function sameStringSet(a: string[], b: string[]): boolean {
	if (a.length !== b.length) return false;
	const set = new Set(a);
	return b.every((x) => set.has(x));
}

// ---------------------------------------------------------------------------
// presets
// ---------------------------------------------------------------------------

function presetDirs(cwd: string): string[] {
	const dirs: string[] = [];
	const override = env("DIFY_SUBAGENT_PRESETS_DIR");
	if (override) dirs.push(override);
	dirs.push(path.join(cwd, ".pi", "extensions", "dify-subagent", "presets"));
	dirs.push(path.join(getAgentDir(), "extensions", "dify-subagent", "presets"));
	return dirs;
}

function parseIntField(raw: string | undefined): number | undefined {
	if (!raw) return undefined;
	const n = Number(raw);
	return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined;
}

function parsePresetFile(filePath: string): Preset | null {
	let raw: string;
	try {
		raw = fs.readFileSync(filePath, "utf8");
	} catch {
		return null;
	}
	const fallbackName = path.basename(filePath).replace(/\.md$/i, "");
	const meta: Record<string, string> = {};
	let body = raw;
	const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
	if (m) {
		for (const line of m[1].split(/\r?\n/)) {
			const kv = line.match(/^([A-Za-z_][\w-]*)\s*:\s*(.*)$/);
			if (kv) meta[kv[1]] = kv[2].trim().replace(/^["']|["']$/g, "");
		}
		body = m[2];
	}
	return {
		name: meta.name || fallbackName,
		description: meta.description || "(no description)",
		model: meta.model || undefined,
		service: meta.service || undefined,
		systemPromptPolicy: meta.system_prompt_policy === "locked" ? "locked" : "optional",
		timeoutMs: parseIntField(meta.timeout_ms),
		resultLimit: parseIntField(meta.result_limit),
		systemPrompt: body.trim(),
	};
}

function loadPresets(cwd: string): Preset[] {
	const byName = new Map<string, Preset>();
	for (const dir of presetDirs(cwd)) {
		let entries: fs.Dirent[];
		try {
			entries = fs.readdirSync(dir, { withFileTypes: true });
		} catch {
			continue;
		}
		for (const e of entries) {
			if (!e.isFile() || !e.name.toLowerCase().endsWith(".md")) continue;
			const preset = parsePresetFile(path.join(dir, e.name));
			if (preset && !byName.has(preset.name)) byName.set(preset.name, preset);
		}
	}
	return [...byName.values()];
}

function presetListText(presets: Preset[]): string {
	if (presets.length === 0) return "(none found — expected *.md files under the dify-subagent presets directory)";
	return presets.map((p) => `- ${p.name}: ${p.description}`).join("\n");
}

/** Service a preset belongs to: explicit `service` field, else model prefix, else its name. */
function presetService(preset: Preset): string {
	return preset.service || (preset.model && serviceOf(preset.model)) || preset.name;
}

// ---------------------------------------------------------------------------
// results
// ---------------------------------------------------------------------------

interface ToolDetails {
	preset: string;
	service: string;
	model?: string;
	modelSource?: "config" | "preset" | "random";
	latencyMs?: number;
}

function errResult(text: string, details?: Partial<ToolDetails>) {
	return { content: [{ type: "text" as const, text }], isError: true, details };
}

function okResult(text: string, details?: Partial<ToolDetails>) {
	return { content: [{ type: "text" as const, text }], details };
}

const Params = Type.Object({
	task: Type.Optional(
		Type.String({
			description:
				"Self-contained task content, sent as the user message. Required for general/custom presets. Batch multiple subtasks into one task when possible — each call is billed per response, not per token.",
		}),
	),
	preset: Type.Optional(
		Type.String({
			description: `Preset name from presets/*.md (default: "${DEFAULTS.preset}"). Unknown names return the available list.`,
		}),
	),
	system_prompt: Type.Optional(
		Type.String({
			description:
				'Custom system prompt. Only accepted by presets with system_prompt_policy "optional" (e.g. "custom-preview"); "locked" presets reject it.',
		}),
	),
	timeout_ms: Type.Optional(
		Type.Number({ description: "Per-call timeout in milliseconds (default: from preset file, usually 300000)." }),
	),
	result_limit: Type.Optional(
		Type.Number({
			description:
				"Max characters returned inline; longer results are saved to a temp file and only a path + preview is returned (default: from preset file).",
		}),
	),
	url: Type.Optional(
		Type.String({
			description: 'Page URL to summarize. REQUIRED when using the "website-summary" preset.',
		}),
	),
	image_paths: Type.Optional(
		Type.Array(Type.String(), {
			description:
				'Local image file paths or http(s) image URLs (max 10). REQUIRED when using the "image-processing" preset.',
		}),
	),
});

function mimeFromExt(p: string): string {
	const ext = p.toLowerCase().split(".").pop() ?? "";
	switch (ext) {
		case "png":
			return "image/png";
		case "jpg":
		case "jpeg":
			return "image/jpeg";
		case "gif":
			return "image/gif";
		case "webp":
			return "image/webp";
		default:
			return "application/octet-stream";
	}
}

// ---------------------------------------------------------------------------
// extension entry
// ---------------------------------------------------------------------------

export default function (pi: ExtensionAPI) {
	// ---------------- command: /dify-subagent ----------------
	pi.registerCommand("dify-subagent", {
		description:
			"Configure the dify-subagent gateway (url, api key) and per-service model selection. Usage: /dify-subagent [show|setup|url <baseUrl>|key|model <service>]",
		getArgumentCompletions: (prefix) => {
			const subs = ["setup", "url", "key", "model", "show"];
			const filtered = subs.filter((s) => s.startsWith(prefix));
			return filtered.length > 0 ? filtered.map((s) => ({ value: s, label: s })) : null;
		},

		handler: async (args, ctx) => {
			const [sub, ...rest] = args.trim().split(/\s+/).filter(Boolean);
			const cfg = readConfig();

			const showConfig = () => {
				const baseUrl = effectiveBaseUrl(cfg);
				const keyInfo = effectiveApiKey(cfg) ? maskKey(effectiveApiKey(cfg)!) : "(none)";
				const mappings = Object.entries(cfg.serviceModels ?? {});
				const mappingText =
					mappings.length > 0 ? mappings.map(([s, m]) => `  ${s} -> ${m}`).join("\n") : "  (none — preset defaults / gateway list apply)";
				ctx.ui.notify(
					`dify-subagent config\nbaseUrl: ${baseUrl}\napiKey: ${keyInfo}\nservice models:\n${mappingText}\nconfig file: ${configPath()}`,
					"info",
				);
			};

			/** Validate by fetching /v1/models; on success save and report services & models. */
			const validateAndSave = async (next: PluginConfig, askModelSelection: boolean) => {
				const baseUrl = effectiveBaseUrl(next);
				const apiKey = effectiveApiKey(next);
				const result = await fetchModelList(baseUrl, apiKey);

				if (!result.ok) {
					const saveAnyway = await ctx.ui.confirm(
						"Validation failed",
						`Could not fetch the model list from ${baseUrl}:\n${result.error}\n\nSave the config anyway?`,
					);
					if (saveAnyway) {
						writeConfig(next);
						modelListCache = null;
						ctx.ui.notify(`Saved (unvalidated) to ${configPath()}`, "warning");
					} else {
						ctx.ui.notify("Config NOT saved.", "info");
					}
					return;
				}

				const groups = groupByService(result.models);
				const summary =
					groups.size > 0
						? [...groups.entries()].map(([s, ms]) => `${s}:\n${ms.map((m) => `  ${m}`).join("\n")}`).join("\n")
						: "(no [service]model entries in the model list)";
				await ctx.ui.confirm("Validation OK — available services & models", summary);

				// Per-service model selection when a service offers multiple models.
				const serviceModels: Record<string, string> = { ...(next.serviceModels ?? {}) };
				if (askModelSelection) {
					for (const [svc, ms] of groups) {
						if (ms.length < 2) continue;
						const current = serviceModels[svc];
						const picked = await ctx.ui.select(
							`Service "${svc}" offers ${ms.length} models — pick one${current ? ` (current: ${current})` : ""}:`,
							ms,
						);
						if (picked) serviceModels[svc] = picked;
					}
				}

				writeConfig({ ...next, serviceModels });
				modelListCache = { baseUrl, models: result.models };
				ctx.ui.notify(`Saved to ${configPath()}`, "info");
			};

			switch (sub ?? "show") {
				case "show":
					showConfig();
					return;

				case "url": {
					const input = rest.join(" ").trim() || (await ctx.ui.input("Gateway base URL:", DEFAULTS.baseUrl));
					if (!input) return;
					const url = normalizeBaseUrl(input);
					if (!/^https?:\/\//i.test(url)) {
						ctx.ui.notify(`Invalid URL "${url}" — must start with http:// or https://`, "error");
						return;
					}
					await validateAndSave({ ...cfg, baseUrl: url }, true);
					return;
				}

				case "key": {
					// Interactive input keeps the key out of the session transcript.
					const key = await ctx.ui.input("API Key (leave empty to clear):", "");
					if (key === undefined) return;
					const next = { ...cfg };
					if (key.trim()) next.apiKey = key.trim();
					else delete next.apiKey;
					await validateAndSave(next, false);
					return;
				}

				case "setup": {
					const urlInput = await ctx.ui.input("Gateway base URL:", effectiveBaseUrl(cfg));
					if (urlInput === undefined) return;
					const url = normalizeBaseUrl(urlInput || DEFAULTS.baseUrl);
					if (!/^https?:\/\//i.test(url)) {
						ctx.ui.notify(`Invalid URL "${url}" — must start with http:// or https://`, "error");
						return;
					}
					const keyInput = await ctx.ui.input(
						`API Key (leave empty to keep ${cfg.apiKey ? "current" : "none"}):`,
						"",
					);
					if (keyInput === undefined) return;
					const next: PluginConfig = { ...cfg, baseUrl: url };
					if (keyInput.trim()) next.apiKey = keyInput.trim();
					await validateAndSave(next, true);
					return;
				}

				case "model": {
					const baseUrl = effectiveBaseUrl(cfg);
					const apiKey = effectiveApiKey(cfg);
					const result = await getModelList(baseUrl, apiKey, true);
					if (!result.ok) {
						ctx.ui.notify(`Could not fetch the model list from ${baseUrl}: ${result.error}`, "error");
						return;
					}
					const groups = groupByService(result.models);
					if (groups.size === 0) {
						ctx.ui.notify("No [service]model entries in the model list.", "warning");
						return;
					}
					let service = rest[0];
					if (!service || !groups.has(service)) {
						const picked = await ctx.ui.select("Select a service to configure:", [...groups.keys()]);
						if (!picked) return;
						service = picked;
					}
					const candidates = groups.get(service)!;
					const current = cfg.serviceModels?.[service];
					const picked = await ctx.ui.select(
						`Model for service "${service}"${current ? ` (current: ${current})` : ""}:`,
						candidates,
					);
					if (!picked) return;
					const next: PluginConfig = { ...cfg, serviceModels: { ...(cfg.serviceModels ?? {}), [service]: picked } };
					writeConfig(next);
					ctx.ui.notify(`Service "${service}" will now use ${picked}`, "info");
					return;
				}

				default:
					ctx.ui.notify(
						`Unknown subcommand "${sub}". Usage: /dify-subagent [show|setup|url <baseUrl>|key|model <service>]`,
						"warning",
					);
			}
		},
	});

	// ---------------- tool: dify-subagent ----------------
	pi.registerTool({
		name: "dify-subagent",
		label: "Dify Subagent",
		description: [
			"Delegate a self-contained, one-shot subtask (no tools, no history) to the Dify2API sub-model and get its text back.",
			"Good for offloading long inputs: summarization, translation, drafting, code snippet review, second opinions.",
			"Each call sends exactly one system + one user message. Keep requested output short — overly long results are offloaded to a temp file (path + preview returned) instead of entering your context.",
		].join(" "),
		parameters: Params,

		async execute(_toolCallId, params, signal, _onUpdate, ctx) {
			const cfg = readConfig();
			const baseUrl = effectiveBaseUrl(cfg);
			const apiKey = effectiveApiKey(cfg);
			const maxTaskChars = envInt("DIFY2API_MAX_TASK_CHARS") ?? DEFAULTS.maxTaskChars;

			// --- resolve preset -------------------------------------------------
			const presets = loadPresets(ctx.cwd);
			const presetName = params.preset?.trim() || DEFAULTS.preset;
			const preset = presets.find((p) => p.name === presetName);
			if (!preset) {
				return errResult(
					`Unknown preset "${presetName}". Available presets:\n${presetListText(presets)}\n\n` +
						`Retry with one of these names, or omit "preset" to use the default ("${DEFAULTS.preset}").`,
				);
			}
			const service = presetService(preset);

			// --- FR-13: policy pre-check (before any network call, no credit burned)
			if (preset.systemPromptPolicy === "locked" && params.system_prompt?.trim()) {
				return errResult(
					`Preset "${preset.name}" has a fixed system prompt (system_prompt_policy: locked) and does not accept a custom "system_prompt". ` +
						`Use preset "custom" for fully custom prompts.`,
					{ preset: preset.name, service },
				);
			}

			// --- per-service input validation + FR-12 length pre-check (no network call)
			if (service !== "website-summary" && !params.task?.trim()) {
				return errResult(
					`Preset "${preset.name}" (service "${service}") requires the "task" parameter.`,
					{ preset: preset.name, service },
				);
			}
			if ((params.task?.length ?? 0) > maxTaskChars) {
				return errResult(
					`task is ${params.task!.length} characters, exceeding the ${maxTaskChars} character limit. ` +
						`Split it into smaller parts and call again. No request was sent.`,
					{ preset: preset.name, service },
				);
			}

			// --- refresh the model list on EVERY call; update the record if changed
			const fresh = await fetchModelList(baseUrl, apiKey);
			if (fresh.ok && (!modelListCache || modelListCache.baseUrl !== baseUrl || !sameStringSet(modelListCache.models, fresh.models))) {
				modelListCache = { baseUrl, models: fresh.models };
			}
			const candidates = fresh.ok ? (groupByService(fresh.models).get(service) ?? []) : [];
			const notifyUser = (msg: string) => {
				if (ctx.hasUI) ctx.ui.notify(msg, "warning");
			};

			// --- resolve model: config mapping > preset default (validated) > random
			let model: string | undefined;
			let modelSource: ToolDetails["modelSource"];
			let randomNote = "";

			const configured = cfg.serviceModels?.[service];
			if (configured && !(fresh.ok && !fresh.models.includes(configured))) {
				// Configured mapping still valid (or the list could not be verified).
				model = configured;
				modelSource = "config";
			} else if (configured) {
				// Previously selected model is GONE from the gateway list: notify the
				// user, then try to continue with a random sibling model.
				notifyUser(
					`dify-subagent: your selected model "${configured}" for service "${service}" is no longer offered by the gateway.`,
				);
				if (candidates.length > 0) {
					model = randomPick(candidates);
					modelSource = "random";
					randomNote =
						`\n\n[Note: previously selected model "${configured}" is no longer available; ` +
						`randomly selected "${model}" instead. Pick a new one via /dify-subagent model ${service}.]`;
				} else {
					notifyUser(`dify-subagent: no model with prefix "[${service}]" exists in the gateway model list.`);
					return errResult(
						`No model available for service "${service}": your selection "${configured}" is gone and the gateway offers no other model with prefix "[${service}]". ` +
							`Check the gateway or pick a model via /dify-subagent model.`,
						{ preset: preset.name, service },
					);
				}
			} else if (fresh.ok) {
				if (candidates.length === 0) {
					// NR-3 rule 4 (amended): prefix absent — always notify the user first.
					notifyUser(`dify-subagent: no model with prefix "[${service}]" exists in the gateway model list.`);
					return errResult(
						`No model available for service "${service}": the gateway model list contains no model with prefix "[${service}]". ` +
							`Check the gateway or pick a model via /dify-subagent model.`,
						{ preset: preset.name, service },
					);
				}
				if (preset.model && candidates.includes(preset.model)) {
					model = preset.model;
					modelSource = "preset";
				} else {
					model = randomPick(candidates);
					modelSource = "random";
					randomNote =
						`\n\n[Note: service "${service}" had no configured/available model; ` +
						`randomly selected "${model}" from the gateway model list. ` +
						`Configure one via /dify-subagent model ${service}.]`;
				}
			} else if (preset.model) {
				// Model list unavailable — proceed with the preset default
				// (V0.1.0 gateways treat the model field as an echo).
				model = preset.model;
				modelSource = "preset";
			} else {
				return errResult(
					`No model available for service "${service}": the model list could not be fetched (${fresh.error ?? "unknown error"}). ` +
						`Check the gateway or pick a model via /dify-subagent model.`,
					{ preset: preset.name, service },
				);
			}

			// --- build messages per service contract ------------------------------
			let messages: { role: string; content: string }[];
			switch (service) {
				case "general":
					// general has NO system slot: the prompt is built into the Dify App.
					if (params.system_prompt?.trim()) {
						return errResult(
							`Service "general" does not accept a system_prompt (its prompt is built into the Dify App). ` +
								`Use preset "custom" to supply your own.`,
							{ preset: preset.name, service },
						);
					}
					messages = [{ role: "user", content: params.task! }];
					break;

				case "website-summary": {
					const url = params.url?.trim();
					if (!url) {
						return errResult(
							`Preset "${preset.name}" (service "website-summary") requires the "url" parameter.`,
							{ preset: preset.name, service },
						);
					}
					if (!/^https?:\/\//i.test(url)) {
						return errResult(`url must start with http:// or https://`, { preset: preset.name, service });
					}
					messages = [{ role: "user", content: url }];
					const instruction = params.system_prompt?.trim() || preset.systemPrompt;
					if (instruction) messages.unshift({ role: "system", content: instruction });
					break;
				}

				case "image-processing": {
					const paths = params.image_paths ?? [];
					if (paths.length === 0) {
						return errResult(
							`Preset "${preset.name}" (service "image-processing") requires "image_paths" (local file paths or http(s) URLs, max 10).`,
							{ preset: preset.name, service },
						);
					}
					if (paths.length > 10) {
						return errResult(`At most 10 images per call, got ${paths.length}.`, { preset: preset.name, service });
					}
					const parts: Record<string, unknown>[] = [{ type: "text", text: params.task! }];
					for (const p of paths) {
						if (/^https?:\/\//i.test(p)) {
							parts.push({ type: "image_url", image_url: { url: p } });
							continue;
						}
						let data: Buffer;
						try {
							data = fs.readFileSync(p);
						} catch (e) {
							return errResult(`Cannot read image file "${p}": ${(e as Error)?.message ?? e}`, { preset: preset.name, service });
						}
						if (data.length > 10 * 1024 * 1024) {
							return errResult(`Image "${p}" exceeds the 10MB limit (${data.length} bytes).`, { preset: preset.name, service });
						}
						parts.push({
							type: "image_url",
							image_url: { url: `data:${mimeFromExt(p)};base64,${data.toString("base64")}` },
						});
					}
					messages = [{ role: "user", content: parts }];
					const instruction = params.system_prompt?.trim() || preset.systemPrompt;
					if (instruction) messages.unshift({ role: "system", content: instruction });
					break;
				}

				default: {
					// custom (and unknown services): optional system + user
					const sys =
						preset.systemPromptPolicy === "locked"
							? preset.systemPrompt
							: params.system_prompt?.trim() || preset.systemPrompt;
						messages = sys
							? [
									{ role: "system", content: sys },
									{ role: "user", content: params.task! },
								]
							: [{ role: "user", content: params.task! }];
				}
			}

			const timeoutMs = clampTimeout(
				params.timeout_ms ?? preset.timeoutMs ?? envInt("DIFY2API_TIMEOUT_MS") ?? DEFAULTS.timeoutMs,
			);
			const resultLimit = Math.floor(params.result_limit ?? preset.resultLimit ?? DEFAULTS.resultLimit);

			// --- fire request with timeout + user-abort -------------------------
			const controller = new AbortController();
			const timer = setTimeout(() => controller.abort(new Error("timeout")), timeoutMs);
			const onUserAbort = () => controller.abort(new Error("aborted"));
			signal?.addEventListener("abort", onUserAbort, { once: true });

			const startedAt = Date.now();
			let resp: Response;
			try {
				resp = await fetch(`${baseUrl}/chat/completions`, {
					method: "POST",
					headers: {
						"Content-Type": "application/json",
						...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
					},
					body: JSON.stringify({
						model,
						stream: false,
						messages,
					}),
					signal: controller.signal,
				});
			} catch (e) {
				const waited = ((Date.now() - startedAt) / 1000).toFixed(1);
				if (controller.signal.aborted && String((e as Error)?.message ?? e) !== "aborted") {
					return errResult(
						`Timeout: no response after ${waited}s (limit ${Math.round(timeoutMs / 1000)}s, preset "${preset.name}"). ` +
							`Retry with a larger timeout_ms, or split the task into smaller parts.`,
						{ preset: preset.name, service, model, modelSource },
					);
				}
				if (controller.signal.aborted) {
					return errResult(`Call aborted after ${waited}s.`, { preset: preset.name, service, model, modelSource });
				}
				return errResult(
					`Network error calling ${baseUrl}/chat/completions: ${(e as Error)?.message ?? e}. ` +
						`Check that the Dify2API gateway is reachable (current: ${baseUrl}; configure via /dify-subagent url).`,
					{ preset: preset.name, service, model, modelSource },
				);
			} finally {
				clearTimeout(timer);
				signal?.removeEventListener("abort", onUserAbort);
			}

			const latencyMs = Date.now() - startedAt;

			// --- HTTP-level errors ------------------------------------------------
			if (!resp.ok) {
				let body = "";
				try {
					body = (await resp.text()).slice(0, 2000);
				} catch {
					/* ignore */
				}
				const hint =
					resp.status === 401 || resp.status === 403
						? "Check the API key (/dify-subagent key)."
						: resp.status === 400
							? "The gateway rejected the message layout (this tool always sends system+user; please report this)."
							: resp.status >= 500
								? "The Dify workflow or upstream model may have failed."
								: "";
				return errResult(
					`HTTP ${resp.status} from Dify2API after ${(latencyMs / 1000).toFixed(1)}s. ${hint}\n` +
						`Response excerpt: ${body || "(empty)"}\n` +
						`You may retry as-is, adjust the input, or give up.`,
					{ preset: preset.name, service, model, modelSource, latencyMs },
				);
			}

			// --- parse -------------------------------------------------------------
			let content: unknown;
			try {
				const data = (await resp.json()) as {
					choices?: { message?: { content?: unknown } }[];
				};
				content = data.choices?.[0]?.message?.content;
			} catch (e) {
				return errResult(`Failed to parse Dify2API response as JSON: ${(e as Error)?.message ?? e}.`);
			}
			if (typeof content !== "string" || content.length === 0) {
				return errResult("Dify2API returned an empty or missing message content.", {
					preset: preset.name,
					service,
					model,
					modelSource,
					latencyMs,
				});
			}

			const details: ToolDetails = { preset: preset.name, service, model, modelSource, latencyMs };

			// --- FR-8: oversized results go to a temp file -------------------------
			if (content.length > resultLimit) {
				try {
					const dir = path.join(os.tmpdir(), DEFAULTS.tmpSubdir);
					fs.mkdirSync(dir, { recursive: true });
					const stamp = new Date().toISOString().replace(/[:.]/g, "-");
					const rand = Math.random().toString(36).slice(2, 8);
					const filePath = path.join(dir, `${stamp}-${rand}.md`);
					fs.writeFileSync(filePath, content, { encoding: "utf8", mode: 0o600 });
					const preview = content.slice(0, DEFAULTS.previewChars);
					return okResult(
						`Result saved to file (${content.length} chars, over the ${resultLimit} inline limit): ${filePath}\n` +
							`Read it with your read tool if you need more than the preview below.\n\n` +
							`--- preview (first ${preview.length} chars) ---\n${preview}${randomNote}`,
						details,
					);
				} catch (e) {
					return okResult(
						`${content.slice(0, resultLimit)}\n\n` +
							`[Truncated: full result was ${content.length} chars; writing the temp file failed: ${(e as Error)?.message ?? e}]${randomNote}`,
						details,
					);
				}
			}

			return okResult(content + randomNote, details);
		},
	});
}
