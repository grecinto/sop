# Workflow Script Library

This directory contains reusable JSON scripts for workflow optimization.

## Purpose

Scripts enable 5-10x faster execution for deterministic tasks compared to LLM calls:
- **LLM call**: 1-3 seconds + token costs
- **Script execution**: 50-200ms, zero LLM costs

## Script Format

Each script is a JSON file with this structure:

```json
{
  "name": "script_name",
  "description": "What this script does",
  "input": "expected_input_variable",
  "output": "result_variable_name",
  "script": [
    {
      "op": "operation_name",
      "args": {...},
      "result_var": "variable_name"
    }
  ]
}
```

## Available Scripts

### extract_go_test_errors.json
Parses Go test output and extracts failure messages.
- **Input**: test_output_file_path
- **Output**: formatted error summary
- **Speed**: ~100ms vs 2s LLM call

### find_test_file.json  
Locates test files by name pattern.
- **Input**: test_name_pattern
- **Output**: file path
- **Speed**: ~50ms vs 1.5s LLM call

## Usage in Workflows

Add an `optimization_profile` to workflow steps:

```json
{
  "step": 2,
  "actor": "test_debugger.analyzer",
  "query_template": "Read test errors from {tool_result}",
  "optimization_profile": {
    "prefer_script": true,
    "script_library": ["extract_go_test_errors"],
    "fallback_to_llm": true,
    "max_script_time_ms": 5000
  }
}
```

## Creating New Scripts

1. Identify deterministic operations (file I/O, grep, parsing)
2. Create JSON script with clear input/output
3. Test with `execute_script` tool
4. Add to workflow optimization_profile
5. Measure speedup (should be 5-10x)

## Performance Guidelines

- Scripts should complete in <500ms
- Use local operations (file reads, grep, simple parsing)
- Avoid complex logic - use LLM fallback instead
- Set appropriate timeouts
