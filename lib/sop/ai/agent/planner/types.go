package planner

import (
	cw "github.com/sharedcode/sop/ai/agent/codingworkflow"
)

// Type aliases for codingworkflow types used by phase implementations
type SharedInfrastructure = cw.SharedInfrastructure
type Task = cw.Task
type RunState = cw.RunState
type ContextPack = cw.ContextPack
type Patch = cw.Patch
type STMReader = cw.STMReader
type WorkStreamReader = cw.WorkStreamReader
type WorkStreamContextReader = cw.WorkStreamContextReader
type PhasePlanReader = cw.PhasePlanReader

// Constants
const CodeFormatInstructions = cw.CodeFormatInstructions
