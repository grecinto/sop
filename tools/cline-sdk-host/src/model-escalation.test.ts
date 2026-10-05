import assert from "node:assert/strict";
import test from "node:test";
import {
  createModelEscalationState,
  requestModelEscalation,
  restoreBaseModel,
} from "./model-escalation.js";

test("escalates once and restores the configured base model", async () => {
  const updates: string[] = [];
  const updater = { updateSessionModel: async (_sessionId: string, modelId: string) => { updates.push(modelId); } };
  const state = createModelEscalationState("model-standard", "model-reasoning");

  const result = await requestModelEscalation(updater, "session-1", state, "Repeated validation failures");
  assert.deepEqual(result, { status: "escalated", fromModelId: "model-standard", toModelId: "model-reasoning" });
  assert.deepEqual(await requestModelEscalation(updater, "session-1", state, "Try again"), { status: "already_attempted" });
  await restoreBaseModel(updater, "session-1", state);
  await restoreBaseModel(updater, "session-1", state);

  assert.deepEqual(updates, ["model-reasoning", "model-standard"]);
  assert.equal(state.restored, true);
});

test("does not enable escalation without a distinct configured target", async () => {
  const state = createModelEscalationState("model-standard", " model-standard ");
  const updater = { updateSessionModel: async () => { throw new Error("must not be called"); } };
  assert.deepEqual(await requestModelEscalation(updater, "session-1", state, "Stalled"), { status: "not_configured" });
});

test("does not retry a failed model switch", async () => {
  const state = createModelEscalationState("model-standard", "model-reasoning");
  const updater = { updateSessionModel: async () => { throw new Error("unsupported model"); } };
  await assert.rejects(requestModelEscalation(updater, "session-1", state, "Repeated failures"), /unsupported model/);
  assert.equal(state.applied, false);
  assert.equal((await requestModelEscalation(updater, "session-1", state, "Try again")).status, "already_attempted");
});