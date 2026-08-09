package agent

import (
	"context"
	"time"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/memory"
)

func (a *CopilotAgent) InitializePhysicalMemory(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if a.Memory == nil {
		a.Memory = memory.NewMemoryUnit(ai.AgentIDOmni)
	}
	if a.systemDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithCancel(ctx)
	a.memoryWorkersCancel = cancel

	a.Memory.BindSession(ctx)
	return a.Memory.StartMemoryWorkers(ctx, a.systemDB)
}

// StartSleepCycle launches background consolidators over the isolated Avatar STM
// migrating Short-Term episodic memories into Semantic Long-Term memory.
func (a *CopilotAgent) StartSleepCycle(ctx context.Context, hourlyInterval int, idleTimeoutMinutes int, nowFn func() time.Time) {
	if a == nil || a.Memory == nil {
		return
	}
	if a.service != nil && !a.service.BiomimicryEnabled() {
		log.Info("CopilotAgent: Biomimicry disabled; skipping sleep cycle consolidation", "agent_id", a.Memory.AgentID)
		return
	}
	if nowFn == nil {
		nowFn = time.Now
	}

	// Wrap the actual consolidating logic to avoid duplication
	runSleepCycle := func() {
		if a.systemDB == nil {
			return
		}

		tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
		if err != nil {
			return
		}
		if a.Memory == nil {
			tx.Rollback(ctx)
			return
		}
		a.Memory.BindSession(ctx)
		if _, err := a.Memory.OpenShortTermMemory(ctx, a.systemDB, tx); err != nil {
			tx.Rollback(ctx)
			return
		}
		stmStore := a.Memory.STMStore()
		stm := stmStore.Primary()

		if removed, err := stmStore.PruneExpired(ctx, time.Now()); err != nil {
			log.Warn("CopilotAgent: Failed pruning stale STM episodes before sleep cycle", "agent_id", a.Memory.AgentID, "error", err)
			tx.Rollback(ctx)
			return
		} else if removed > 0 {
			log.Debug("CopilotAgent: Pruned stale STM episodes before sleep cycle", "agent_id", a.Memory.AgentID, "count", removed)
		}

		var embedder ai.Embeddings
		if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
			embedder = a.service.Domain().Embedder()
		}

		ltm, err := a.systemDB.OpenKnowledgeBase(ctx, a.Memory.LongTermMemoryName(), tx, a.brain, embedder, false)
		if err != nil {
			tx.Rollback(ctx)
			return
		}

		retiredCount, err := a.applyLTMLifecycleRetention(ctx, ltm, nowFn())
		if err != nil {
			log.Warn("CopilotAgent: Failed applying lifecycle retention during sleep cycle", "agent_id", a.Memory.AgentID, "error", err)
		} else if retiredCount > 0 {
			log.Debug("CopilotAgent: Archived stale LTM entries during sleep cycle", "agent_id", a.Memory.AgentID, "count", retiredCount)
		}

		var thoughts []memory.Thought[map[string]any]
		var itemIDs []string

		ok, _ := stm.First(ctx)
		for ok {
			key := stm.GetCurrentKey()
			if key.Key != "root_anchor" {
				val, _ := stm.GetCurrentValue(ctx)
				if payload, valid := val.(map[string]any); valid {
					thoughtText := ""
					if txt, has := payload["thought"].(string); has {
						thoughtText = txt
					}

					thoughts = append(thoughts, memory.Thought[map[string]any]{
						Summaries: []string{thoughtText},
						Data:      payload,
					})
					itemIDs = append(itemIDs, key.Key)
				}
			}
			ok, _ = stm.Next(ctx)
		}

		if len(thoughts) == 0 {
			tx.Rollback(ctx)
			return
		}

		err = ltm.IngestThoughts(ctx, thoughts, a.Memory.AgentID)
		if err != nil {
			tx.Rollback(ctx)
			return
		}

		err = ltm.TriggerSleepCycle(ctx)
		if err != nil {
			log.Warn("CopilotAgent: LTM TriggerSleepCycle encountered error", "agent_id", a.Memory.AgentID, "error", err)
		}

		for i, id := range itemIDs {
			var payload map[string]any
			if i < len(thoughts) {
				payload = thoughts[i].Data
			}
			err = stmStore.RemoveEpisode(ctx, id, payload)
			if err != nil {
				log.Warn("CopilotAgent: Failed to remove scrubbed item from STM", "id", id, "error", err)
			}
		}

		// Prune expired session states (7-day TTL)
		if removed, err := PruneExpiredSessions(ctx, stm, nowFn()); err != nil {
			log.Warn("CopilotAgent: Failed to prune expired sessions", "agent_id", a.Memory.AgentID, "error", err)
		} else if removed > 0 {
			log.Debug("CopilotAgent: Pruned expired session states", "agent_id", a.Memory.AgentID, "count", removed)
		}

		tx.Commit(ctx)
		a.Memory.CloseShortTermMemory()
		log.Debug("CopilotAgent: Sleep Cycle completed successfully.", "agent_id", a.Memory.AgentID)
	}

	// 1. "Quick Nap" Sensor (Idle Ticker) - recommended 5-10 minutes
	if idleTimeoutMinutes > 0 {
		go func() {
			log.Info("CopilotAgent: Initiating Quick Nap Idle Sensor", "agent_id", a.Memory.AgentID, "timeout_minutes", idleTimeoutMinutes)
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lastLog := a.Memory.LastEpisodeTS.Load()
					if shouldTriggerQuickNap(lastLog, idleTimeoutMinutes, nowFn()) {
						// Perform a safe check: are there items in STM unvectorized?
						tx, err := a.systemDB.BeginTransaction(ctx, sop.ForReading)
						if err == nil {
							a.Memory.BindSession(ctx)
							stm, stmErr := a.systemDB.OpenBtree(ctx, a.Memory.ShortTermMemoryName(), tx)
							if stmErr == nil {
								// Root anchor check
								count := stm.Count()
								if count > 1 {
									log.Info("CopilotAgent: Avatar is IDLE. Triggering 'Quick Nap' memory consolidation.", "agent_id", a.Memory.AgentID)
									runSleepCycle()
									a.Memory.LastEpisodeTS.Store(0) // Reset after sweeping
								}
							}
							tx.Rollback(ctx)
						}
					}
				}
			}
		}()
	}

	// 2. "Deep Sleep" Scheduler (Every 30 Minutes)
	go func() {
		log.Info("CopilotAgent: Initiating Deep Sleep Scheduler (30-min cycles)", "agent_id", a.Memory.AgentID)

		// Align to next 30-minute mark (e.g., :00 or :30)
		now := nowFn()
		nextCycle := now.Truncate(30 * time.Minute).Add(30 * time.Minute)
		if nextCycle.Before(now) || nextCycle.Equal(now) {
			nextCycle = nextCycle.Add(30 * time.Minute)
		}
		time.Sleep(nextCycle.Sub(now))

		// Tick every 30 minutes
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				log.Info("CopilotAgent: Running Scheduled Deep Sleep Cycle", "agent_id", a.Memory.AgentID, "time", t.Format("15:04"))
				runSleepCycle()
				a.Memory.LastEpisodeTS.Store(0) // Reset idle sensor
			}
		}
	}()

	// 3. Weekly compaction sweep for old archived memories
	go func() {
		log.Info("CopilotAgent: Initiating Weekly LTM Compaction Sweep", "agent_id", a.Memory.AgentID)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if a.systemDB == nil || a.Memory == nil {
					continue
				}
				tx, err := a.systemDB.BeginTransaction(ctx, sop.ForWriting)
				if err != nil {
					continue
				}
				var embedder ai.Embeddings
				if a.service != nil && a.service.Domain() != nil && a.service.Domain().Embedder() != nil {
					embedder = a.service.Domain().Embedder()
				}
				ltm, err := a.systemDB.OpenKnowledgeBase(ctx, a.Memory.LongTermMemoryName(), tx, a.brain, embedder, false)
				if err != nil {
					tx.Rollback(ctx)
					continue
				}
				prunedCount, err := a.applyLTMLifecycleCompaction(ctx, ltm, time.Now())
				if err != nil {
					log.Warn("CopilotAgent: Failed applying lifecycle compaction", "agent_id", a.Memory.AgentID, "error", err)
					tx.Rollback(ctx)
					continue
				}
				if prunedCount > 0 {
					log.Debug("CopilotAgent: Pruned archived LTM entries", "agent_id", a.Memory.AgentID, "count", prunedCount)
				}
				if err := tx.Commit(ctx); err != nil {
					log.Warn("CopilotAgent: Failed committing lifecycle compaction", "agent_id", a.Memory.AgentID, "error", err)
				}
			}
		}
	}()
}
func shouldTriggerQuickNap(lastLogTS int64, idleTimeoutMinutes int, now time.Time) bool {
	if lastLogTS == 0 || idleTimeoutMinutes <= 0 {
		return false
	}
	idleTime := now.Sub(time.UnixMilli(lastLogTS))
	return idleTime >= time.Duration(idleTimeoutMinutes)*time.Minute
}

func shouldTriggerDeepSleep(hour int, hourlyInterval int) bool {
	if hour == 0 {
		return true // Always sweep at midnight
	}
	if hourlyInterval > 0 && hour%hourlyInterval == 0 {
		return true
	}
	return false
}
