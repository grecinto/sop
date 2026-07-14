# KB Task Classification Guide

## Purpose

The task classifier helps the KB quality system choose the most relevant Knowledge Bases for a coding request. It lives in [lib/sop/ai/agent/codingworkflow/task_classifier.go](../lib/sop/ai/agent/codingworkflow/task_classifier.go) and produces a task type plus ranked KB candidates.

## How it works

The classifier looks at three signals:

1. The task description text
2. The active file extension
3. Domain keywords such as error, api, database, concurrency, or security

It maps those signals into one of the built-in task types:

- implementation
- debugging
- testing
- refactoring
- documentation
- architecture

## Extending the classifier

To add a new task type:

1. Add a new constant in [lib/sop/ai/agent/codingworkflow/task_classifier.go](../lib/sop/ai/agent/codingworkflow/task_classifier.go)
2. Expand the scoring logic in `classifyTaskType()`
3. Add a new case in `recommendKBs()` for KB recommendations
4. Add or update tests in [lib/sop/ai/agent/codingworkflow/task_classifier_test.go](../lib/sop/ai/agent/codingworkflow/task_classifier_test.go)

## Recommended heuristics

- Prefer specific keywords over broad ones
- Use file extensions as a weak signal, not the only signal
- Keep confidence values modest and explainable
- Add domain-specific KB recommendations only when there is a clear match

## Example

A request such as "fix the auth error in the API handler" should score highly for debugging and also recommend an error-handling or security-related KB.

## Tuning advice

- Start with a small keyword set and add only patterns that are clearly useful
- Track classifier outcomes in the effectiveness tracker so the system can learn from real usage
- Review the task distribution in the quality dashboard after rollout
