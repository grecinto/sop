import { createInterface } from "node:readline";
import { ClineCore, createTool } from "@cline/core";
import type { CoreSessionEvent } from "@cline/core";
import type { HostEvent, HostRequest, RunRequest } from "./protocol.js";

const core = await ClineCore.create({ clientName: "sop-ai-studio" });
const activeSessions = new Map<string, string>();
const activeRuns = new Map<string, Promise<void>>();
const sessions = new Map<string, { fingerprint: string; request: RunRequest }>();

function emit(event: HostEvent): void {
  process.stdout.write(`${JSON.stringify(event)}\n`);
}

function serializeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function resolveApiKey(request: RunRequest): string | undefined {
  return request.apiKey
    ?? process.env.SOP_CLINE_API_KEY
    ?? (request.providerId === "openai-native" ? process.env.OPENAI_API_KEY : undefined)
    ?? (request.providerId === "anthropic" ? process.env.ANTHROPIC_API_KEY : undefined)
    ?? (request.providerId === "gemini" ? process.env.GEMINI_API_KEY : undefined);
}

type UsageCounters = {
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  totalCost: number;
};

type UsageSummary = {
  usage?: UsageCounters;
  aggregateUsage?: UsageCounters;
};

function subtractUsage(current?: UsageCounters, previous?: UsageCounters): UsageCounters | undefined {
  if (!current) return undefined;
  return {
    inputTokens: Math.max(0, current.inputTokens - (previous?.inputTokens ?? 0)),
    outputTokens: Math.max(0, current.outputTokens - (previous?.outputTokens ?? 0)),
    cacheReadTokens: Math.max(0, current.cacheReadTokens - (previous?.cacheReadTokens ?? 0)),
    cacheWriteTokens: Math.max(0, current.cacheWriteTokens - (previous?.cacheWriteTokens ?? 0)),
    totalCost: Math.max(0, current.totalCost - (previous?.totalCost ?? 0)),
  };
}

function normalizeResult(raw: any, usage?: { before?: UsageSummary; after?: UsageSummary }): Record<string, unknown> {
  const finishReason = String(raw?.finishReason ?? "completed").toLowerCase();
  const status = ["completed", "success", "complete"].includes(finishReason)
    ? "completed"
    : "failed";
  return {
    status,
    summary: String(raw?.text ?? ""),
    metadata: {
      finish_reason: raw?.finishReason,
      iterations: raw?.iterations,
      usage: raw?.usage,
      model: raw?.model,
      ...(usage?.after ? {
        session_usage: usage.after.usage,
        aggregate_session_usage: usage.after.aggregateUsage,
        task_usage: subtractUsage(usage.after.usage, usage.before?.usage),
        aggregate_task_usage: subtractUsage(usage.after.aggregateUsage, usage.before?.aggregateUsage),
      } : {}),
    },
  };
}

function guidancePrompt(request: RunRequest): string {
  if (!request.guidance) return request.task;
  return [
    request.task,
    "",
    "AI Studio guidance (use as constraints and context; inspect the workspace before editing):",
    JSON.stringify(request.guidance, null, 2),
  ].join("\n");
}

function sessionFingerprint(request: RunRequest): string {
  return JSON.stringify({
    workspaceRoot: request.workspaceRoot,
    providerId: request.providerId,
    modelId: request.modelId,
    baseUrl: request.baseUrl,
    systemPrompt: request.systemPrompt,
    yolo: request.mode === "yolo",
  });
}

async function callAIStudioMCP(
  request: RunRequest,
  requestSuffix: string,
  toolName: string,
  args: Record<string, unknown>,
): Promise<unknown> {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) throw new Error("AI Studio MCP URL is not configured");
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  const scopedArgs = { ...args };
  if (request.sessionId) scopedArgs.session_id = request.sessionId;
  if (request.workstreamId) scopedArgs.workstream_id = request.workstreamId;
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}),
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: `${requestSuffix}-${request.requestId}`,
      method: "tools/call",
      params: { name: toolName, arguments: scopedArgs },
    }),
  });
  if (!response.ok) throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
  return await response.json();
}

function createAIStudioTools(session: { request: RunRequest }, sessionId: string) {
  if (!(session.request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL)) return [];
  return [
    createTool({
      name: "ai_studio_get_task_guidance",
      description: "Retrieve authoritative AI Studio task guidance, phase context, and workspace context for this delegated task.",
      inputSchema: {
        type: "object",
        properties: { session_id: { type: "string", description: "AI Studio session id" } },
        required: ["session_id"],
      },
      execute: async (input: { session_id: string }) => callAIStudioMCP(session.request, "guidance", "get_task_guidance", input),
    }),
    createTool({
      name: "ai_studio_search_memory",
      description: "Search authoritative AI Studio semantic memory for relevant engineering knowledge and prior outcomes.",
      inputSchema: {
        type: "object",
        properties: { query: { type: "string" }, limit: { type: "integer", minimum: 1, maximum: 20 } },
        required: ["query"],
      },
      execute: async (input: { query: string; limit?: number }) => callAIStudioMCP(session.request, "memory", "search_memory", { ...input, limit: input.limit ?? 5 }),
    }),
    createTool({
      name: "ai_studio_search_undo_history",
      description: "Search canonical AI Studio undo history for prior changes in this workspace. This tool is read-only.",
      inputSchema: {
        type: "object",
        properties: { workspace_root: { type: "string" }, session_id: { type: "string" }, query: { type: "string" }, limit: { type: "integer", minimum: 1, maximum: 20 } },
        required: ["workspace_root"],
      },
      execute: async (input: { workspace_root: string; session_id?: string; query?: string; limit?: number }) => callAIStudioMCP(session.request, "undo", "search_undo_history", { ...input, limit: input.limit ?? 5 }),
    }),
    createTool({
      name: "ai_studio_record_validation",
      description: "Record the result of tests, builds, or other validation in AI Studio.",
      inputSchema: {
        type: "object",
        properties: { session_id: { type: "string" }, status: { type: "string" }, summary: { type: "string" }, command: { type: "string" } },
        required: ["session_id", "status", "summary"],
      },
      execute: async (input: { session_id: string; status: string; summary: string; command?: string }) => callAIStudioMCP(session.request, "validation", "record_validation", input),
    }),
    createTool({
      name: "ai_studio_restore_undo_transaction",
      description: "Restore an AI Studio undo transaction after explicit confirmation. This changes workspace files and refreshes the current session.",
      inputSchema: {
        type: "object",
        properties: { workspace_root: { type: "string" }, session_id: { type: "string" }, transaction_id: { type: "string" }, confirm: { type: "boolean" } },
        required: ["workspace_root", "transaction_id", "confirm"],
      },
      execute: async (input: { workspace_root: string; session_id?: string; transaction_id: string; confirm: boolean }) => {
        const response = await callAIStudioMCP(session.request, "restore", "restore_undo_transaction", input);
        if (input.confirm && !(typeof response === "object" && response !== null && "error" in response)) {
          emit({
            type: "session_invalidated",
            requestId: session.request.requestId,
            sessionId,
            message: "SOP restored the workspace; the Cline session will be restarted before the next task.",
          });
        }
        return response;
      },
    }),
  ];
}

async function run(request: RunRequest): Promise<void> {
  const sessionId = request.clineSessionId;
  if (!sessionId) {
    emit({ type: "error", requestId: request.requestId, message: "Cline session ID is required." });
    return;
  }
  const fingerprint = sessionFingerprint(request);
  let session = sessions.get(sessionId);
  const isNewSession = !session;
  if (session && session.fingerprint !== fingerprint) {
    emit({ type: "error", requestId: request.requestId, sessionId, message: "Cline session configuration changed; use a new SOP workstream session." });
    return;
  }
  if (session) {
    Object.assign(session.request, request);
  } else {
    session = { fingerprint, request };
    sessions.set(sessionId, session);
  }
  activeSessions.set(request.requestId, sessionId);
  emit({ type: "started", requestId: request.requestId, sessionId, message: isNewSession ? "Starting Cline workstream session" : "Continuing Cline workstream session" });

  const unsubscribe = core.subscribe((event: CoreSessionEvent) => {
    emit({ type: "progress", requestId: request.requestId, sessionId, result: event });
  }, { sessionId });

  try {
    let usageBefore: UsageSummary | undefined;
    try {
      usageBefore = await core.getAccumulatedUsage(sessionId);
    } catch {
      usageBefore = undefined;
    }
    let rawResult: any;
    if (isNewSession) {
      const started = await core.start({
        sessionId,
        prompt: guidancePrompt(request),
        interactive: false,
        config: {
          providerId: request.providerId,
          modelId: request.modelId,
          apiKey: resolveApiKey(request),
          baseUrl: request.baseUrl,
          cwd: request.workspaceRoot,
          workspaceRoot: request.workspaceRoot,
          mode: request.mode === "plan" ? "plan" : "act",
          enableTools: true,
          enableSpawnAgent: false,
          enableAgentTeams: false,
          yolo: request.mode === "yolo",
          systemPrompt: request.systemPrompt ?? "You are the coding copilot for AI Studio. Inspect the workspace, make the smallest correct change, run relevant validation, and report what changed.",
          extraTools: createAIStudioTools(session, sessionId),
          checkpoint: { enabled: request.enableCheckpoints ?? true },
        },
      });
      rawResult = started.result;
    } else {
      rawResult = await core.send({
        sessionId,
        prompt: guidancePrompt(request),
        mode: request.mode === "plan" ? "plan" : "act",
      });
    }
    let usageAfter: UsageSummary | undefined;
    try {
      usageAfter = await core.getAccumulatedUsage(sessionId);
    } catch {
      usageAfter = undefined;
    }
    emit({
      type: "result",
      requestId: request.requestId,
      sessionId,
      result: normalizeResult(
        rawResult ?? { finishReason: "completed", text: "Cline completed without a result payload." },
        usageAfter ? { before: usageBefore, after: usageAfter } : undefined,
      ),
    });
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      emit({ type: "cancelled", requestId: request.requestId, sessionId });
    } else {
      if (isNewSession) {
        sessions.delete(sessionId);
        emit({ type: "session_invalidated", requestId: request.requestId, sessionId, message: "Initial Cline session failed; the host will be restarted before retry." });
      }
      emit({ type: "error", requestId: request.requestId, sessionId, message: serializeError(error) });
    }
  } finally {
    unsubscribe();
    activeSessions.delete(request.requestId);
  }
}

async function handle(request: HostRequest): Promise<boolean> {
  switch (request.type) {
    case "run": {
      if (activeRuns.has(request.requestId)) {
        emit({ type: "error", requestId: request.requestId, sessionId: request.clineSessionId, message: "Cline request is already running." });
        return true;
      }
      const pending = run(request).finally(() => activeRuns.delete(request.requestId));
      activeRuns.set(request.requestId, pending);
      return true;
    }
    case "cancel": {
      const sessionId = activeSessions.get(request.requestId);
      if (sessionId) {
        try {
          await core.abort(sessionId);
        } catch (error) {
          emit({ type: "error", requestId: request.requestId, sessionId, message: serializeError(error) });
        }
      }
      return true;
    }
    case "shutdown":
      await Promise.allSettled([...new Set(activeSessions.values())].map(sessionId => core.abort(sessionId)));
      await Promise.allSettled(activeRuns.values());
      emit({ type: "stopped" });
      return false;
  }
}

const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of input) {
  if (!line.trim()) continue;
  try {
    if (!(await handle(JSON.parse(line) as HostRequest))) break;
  } catch (error) {
    emit({ type: "error", message: serializeError(error) });
  }
}
await core.dispose();