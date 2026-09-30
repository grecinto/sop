import { createInterface } from "node:readline";
import { ClineCore, createTool } from "@cline/core";
import type { CoreSessionEvent } from "@cline/core";
import type { HostEvent, HostRequest, RunRequest } from "./protocol.js";

const core = await ClineCore.create({ clientName: "sop-ai-studio" });
const activeSessions = new Map<string, string>();

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

function normalizeResult(raw: any): Record<string, unknown> {
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
    },
  };
}

function guidancePrompt(request: RunRequest): string {
  const guidance = request.guidance;
  if (!guidance) {
    return request.task;
  }

  return [
    request.task,
    "",
    "AI Studio guidance (use as constraints and context; inspect the workspace before editing):",
    JSON.stringify(guidance, null, 2),
  ].join("\n");
}

function createAIStudioGuidanceTool(request: RunRequest) {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) {
    return undefined;
  }
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  return createTool({
    name: "ai_studio_get_task_guidance",
    description: "Retrieve authoritative AI Studio task guidance, phase context, and workspace context for this delegated task.",
    inputSchema: {
      type: "object",
      properties: { session_id: { type: "string", description: "AI Studio session id" } },
      required: ["session_id"],
    },
    execute: async (input: { session_id: string }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}),
        },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: `guidance-${request.requestId}`,
          method: "tools/call",
          params: { name: "get_task_guidance", arguments: { session_id: input.session_id } },
        }),
      });
      if (!response.ok) {
        throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
      }
      return await response.json();
    },
  });
}

function createAIStudioMemoryTool(request: RunRequest) {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) return undefined;
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  return createTool({
    name: "ai_studio_search_memory",
    description: "Search authoritative AI Studio semantic memory for relevant engineering knowledge and prior outcomes.",
    inputSchema: {
      type: "object",
      properties: { query: { type: "string" }, limit: { type: "integer", minimum: 1, maximum: 20 } },
      required: ["query"],
    },
    execute: async (input: { query: string; limit?: number }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}) },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: `memory-${request.requestId}`,
          method: "tools/call",
          params: { name: "search_memory", arguments: { query: input.query, limit: input.limit ?? 5 } },
        }),
      });
      if (!response.ok) throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
      return await response.json();
    },
  });
}

function createAIStudioUndoHistoryTool(request: RunRequest) {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) return undefined;
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  return createTool({
    name: "ai_studio_search_undo_history",
    description: "Search canonical AI Studio undo history for prior changes in this workspace. This tool is read-only.",
    inputSchema: {
      type: "object",
      properties: { workspace_root: { type: "string" }, session_id: { type: "string" }, query: { type: "string" }, limit: { type: "integer", minimum: 1, maximum: 20 } },
      required: ["workspace_root"],
    },
    execute: async (input: { workspace_root: string; session_id?: string; query?: string; limit?: number }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}) },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: `undo-${request.requestId}`,
          method: "tools/call",
          params: { name: "search_undo_history", arguments: input },
        }),
      });
      if (!response.ok) throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
      return await response.json();
    },
  });
}

function createAIStudioValidationTool(request: RunRequest) {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) return undefined;
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  return createTool({
    name: "ai_studio_record_validation",
    description: "Record the result of tests, builds, or other validation in AI Studio.",
    inputSchema: {
      type: "object",
      properties: { session_id: { type: "string" }, status: { type: "string" }, summary: { type: "string" }, command: { type: "string" } },
      required: ["session_id", "status", "summary"],
    },
    execute: async (input: { session_id: string; status: string; summary: string; command?: string }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}) },
        body: JSON.stringify({ jsonrpc: "2.0", id: `validation-${request.requestId}`, method: "tools/call", params: { name: "record_validation", arguments: input } }),
      });
      if (!response.ok) throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
      return await response.json();
    },
  });
}

function createAIStudioRestoreTool(request: RunRequest) {
  const url = request.mcpUrl ?? process.env.SOP_AI_STUDIO_MCP_URL;
  if (!url) return undefined;
  const bearerToken = request.mcpBearerToken ?? process.env.SOP_AI_STUDIO_MCP_TOKEN;
  return createTool({
    name: "ai_studio_restore_undo_transaction",
    description: "Restore an AI Studio undo transaction after explicit confirmation. This changes workspace files and refreshes the current session.",
    inputSchema: {
      type: "object",
      properties: { workspace_root: { type: "string" }, session_id: { type: "string" }, transaction_id: { type: "string" }, confirm: { type: "boolean" } },
      required: ["workspace_root", "transaction_id", "confirm"],
    },
    execute: async (input: { workspace_root: string; session_id?: string; transaction_id: string; confirm: boolean }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}) },
        body: JSON.stringify({ jsonrpc: "2.0", id: `restore-${request.requestId}`, method: "tools/call", params: { name: "restore_undo_transaction", arguments: input } }),
      });
      if (!response.ok) throw new Error(`AI Studio MCP request failed with HTTP ${response.status}`);
      return await response.json();
    },
  });
}

async function run(request: RunRequest): Promise<void> {
  const sessionId = request.sessionId ?? `sop-${request.requestId}`;
  activeSessions.set(request.requestId, sessionId);
  emit({ type: "started", requestId: request.requestId, sessionId });

  const unsubscribe = core.subscribe((event: CoreSessionEvent) => {
    emit({
      type: "progress",
      requestId: request.requestId,
      sessionId,
      result: event,
    });
  }, { sessionId });

  try {
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
        mode: request.mode ?? "act",
        enableTools: true,
        enableSpawnAgent: false,
        enableAgentTeams: false,
        yolo: request.mode === "yolo",
        systemPrompt: request.systemPrompt ?? "You are the coding copilot for AI Studio. Inspect the workspace, make the smallest correct change, run relevant validation, and report what changed.",
        extraTools: [createAIStudioGuidanceTool(request), createAIStudioMemoryTool(request), createAIStudioUndoHistoryTool(request), createAIStudioValidationTool(request), createAIStudioRestoreTool(request)].filter((tool): tool is NonNullable<typeof tool> => tool !== undefined),
        checkpoint: {
          enabled: request.enableCheckpoints ?? true,
        },
      },
    });

    emit({
      type: "result",
      requestId: request.requestId,
      sessionId: started.sessionId,
      result: normalizeResult(started.result ?? { finishReason: "completed", text: "Cline completed without a result payload." }),
    });
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      emit({ type: "cancelled", requestId: request.requestId, sessionId });
    } else {
      emit({ type: "error", requestId: request.requestId, sessionId, message: serializeError(error) });
    }
  } finally {
    unsubscribe();
    activeSessions.delete(request.requestId);
  }
}

async function handle(request: HostRequest): Promise<boolean> {
  switch (request.type) {
    case "run":
      await run(request);
      return true;
    case "cancel": {
      const sessionId = activeSessions.get(request.requestId);
      if (sessionId) {
        await core.abort(sessionId);
        emit({ type: "cancelled", requestId: request.requestId, sessionId });
      }
      return true;
    }
    case "shutdown":
      emit({ type: "stopped" });
      await core.dispose();
      return false;
  }
}

const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of input) {
  if (!line.trim()) continue;
  try {
    const request = JSON.parse(line) as HostRequest;
    if (!(await handle(request))) break;
  } catch (error) {
    emit({ type: "error", message: serializeError(error) });
  }
}

await core.dispose();
