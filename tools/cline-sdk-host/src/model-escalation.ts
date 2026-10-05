export type ModelEscalationState = {
  baseModelId: string;
  targetModelId?: string;
  attempted: boolean;
  applied: boolean;
  restored: boolean;
  reason?: string;
  error?: string;
  inProgress: boolean;
};

export type SessionModelUpdater = {
  updateSessionModel(sessionId: string, modelId: string): Promise<void>;
};

export function createModelEscalationState(baseModelId: string, targetModelId?: string): ModelEscalationState {
  const normalizedBase = baseModelId.trim();
  const normalizedTarget = targetModelId?.trim();
  return {
    baseModelId: normalizedBase,
    targetModelId: normalizedTarget && normalizedTarget !== normalizedBase ? normalizedTarget : undefined,
    attempted: false,
    applied: false,
    restored: false,
    inProgress: false,
  };
}

export async function requestModelEscalation(
  updater: SessionModelUpdater,
  sessionId: string,
  state: ModelEscalationState,
  reason: string,
): Promise<{ status: string; fromModelId?: string; toModelId?: string }> {
  if (!state.targetModelId) return { status: "not_configured" };
  if (state.attempted || state.inProgress) return { status: "already_attempted" };
  const normalizedReason = reason.trim().slice(0, 500);
  if (!normalizedReason) return { status: "reason_required" };

  state.attempted = true;
  state.inProgress = true;
  state.reason = normalizedReason;
  try {
    await updater.updateSessionModel(sessionId, state.targetModelId);
    state.applied = true;
    return { status: "escalated", fromModelId: state.baseModelId, toModelId: state.targetModelId };
  } catch (error) {
    state.error = error instanceof Error ? error.message : String(error);
    throw error;
  } finally {
    state.inProgress = false;
  }
}

export async function restoreBaseModel(
  updater: SessionModelUpdater,
  sessionId: string,
  state: ModelEscalationState,
): Promise<void> {
  if (!state.applied || state.restored) return;
  await updater.updateSessionModel(sessionId, state.baseModelId);
  state.restored = true;
}