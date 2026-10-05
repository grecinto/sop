export type HostRequest = RunRequest | CancelRequest | ShutdownRequest;

export type RunRequest = {
  type: "run";
  requestId: string;
  sessionId?: string;
  workstreamId?: string;
  clineSessionId: string;
  task: string;
  workspaceRoot: string;
  providerId: string;
  modelId: string;
  apiKey?: string;
  baseUrl?: string;
  systemPrompt?: string;
  mode?: "plan" | "act" | "yolo";
  enableCheckpoints?: boolean;
  mcpUrl?: string;
  mcpBearerToken?: string;
  guidance?: {
    teamPractices?: string[];
    successCriteria?: string[];
    constraints?: string[];
    implementationNotes?: string[];
    referenceContext?: unknown[];
    metadata?: Record<string, unknown>;
  };
};

export type CancelRequest = {
  type: "cancel";
  requestId: string;
};

export type ShutdownRequest = {
  type: "shutdown";
};

export type HostEvent = {
  type: "started" | "progress" | "result" | "error" | "cancelled" | "session_invalidated" | "stopped";
  requestId?: string;
  sessionId?: string;
  message?: string;
  result?: unknown;
};
