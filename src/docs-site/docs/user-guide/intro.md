---
sidebar_position: 1
title: AI Studio User Guide
description: Complete guide to using AI Studio for coding, debugging, and knowledge management.
---

# AI Studio User Guide

Welcome to AI Studio - an intelligent coding assistant that runs on your own infrastructure with persistent memory, structured workflows, and privacy-first architecture.

## What is AI Studio?

AI Studio is a self-hosted AI platform that helps you code, debug, test, and manage knowledge without relying on third-party cloud services. Unlike consumer copilots that operate through closed wrappers, AI Studio gives you:

- **Full control**: Runs on your infrastructure, uses your chosen LLM provider
- **LLM as Orchestrator**: The AI makes all decisions—no rigid scripts, pure intelligent reasoning
- **Persistent memory**: Learns from your codebase and workflow patterns
- **Privacy-first**: Your code and data never leave your environment
- **Open source**: Inspect, extend, and customize everything

### The LLM as Orchestrator Paradigm

AI Studio's architecture is built on a fundamental principle: **the LLM is always in control**. Unlike traditional automation systems with fixed workflows and rule-based routing, AI Studio lets the AI reason about your task and orchestrate the solution dynamically.

**What this means for you:**
- 🎯 **Adaptive execution** - The AI chooses the right approach for your specific situation, not a pre-programmed script
- 🧠 **Intelligent decision-making** - Task classification, tool selection, and flow control all driven by AI reasoning
- 🔄 **Natural adjustments** - The AI can pivot, revisit phases, or change strategy based on what it discovers
- 💬 **Conversational control** - You guide with natural language, not configuration files

This is the core difference between AI Studio and rigid workflow systems—the intelligence orchestrates everything.

## Key Features

### Intelligent Coding with LLM Orchestration
Built on the **LLM as Orchestrator** paradigm, AI Studio breaks down complex tasks intelligently—the AI reasons about your task and orchestrates the solution dynamically:

**Smart Planning** 🤖 - Just describe what you want to accomplish, and the AI analyzes your task, determines the best approach, and generates a custom plan. Whether debugging, implementing features, or refactoring, it adapts to your specific situation—no pre-programmed scripts.

**Proven Templates** 📚 - For common tasks like testing, debugging, or refactoring, AI Studio can follow battle-tested patterns from your team's knowledge base. This ensures consistency and lets you encode your team's best practices.

**Real-Time Adjustments** - Change your mind mid-execution? Just type your modification and the AI adapts its orchestration without starting over. Add error handling, include extra validation, or refine the approach as you go.

**Transparent Reasoning** 💬 - AI Studio shows you what it understood from your request before taking action. See the execution plan, significant discoveries during analysis, and LLM thinking—all in collapsible sections so you control the level of detail. This transparency lets you interject or correct before execution begins.

**Intelligent Undo** 🔄 - Experiment fearlessly with an industry-leading undo system that treats each workflow as a searchable transaction. Find and restore past changes using natural language: "undo authentication middleware changes" just works. No git history pollution, no manual bisecting.

**Multi-language support**: Works seamlessly with Go, Python, JavaScript/TypeScript, Java, Rust, C#, HTML/CSS, React, and Angular. Automatically detects your project type and uses the right tools for testing, analysis, and code generation.

[Learn about Intelligent Coding →](./coding-workflows)

### Persistent Memory System
Unlike stateless copilots, AI Studio maintains context across sessions through structured knowledge spaces. It remembers your coding patterns, project structure, and preferences.

[Learn about Memory & Spaces →](./space-management)

### Fleet Learning
Optionally share validated coding patterns across your team without exposing proprietary code. Improve the AI's suggestions based on what has worked well in your organization.

[Learn about Fleet Learning →](../fleet-learning)

### Remote Daemon Access
Connect AI Studio to daemons running on local or remote machines in your private network. Work securely without exposing systems to the public internet.

[Learn about Remote Access →](./remote-daemon-access)

### Continue as a Companion
If you use Continue in your editor, it can act as a thin companion that sends coding requests to the SOP daemon for execution. The daemon remains the runtime that orchestrates workflows, approvals, memory, and long-running tool execution.

[Learn about the Continue facade architecture →](../sdk/continue-facade-architecture)

### AI Window Experience
The AI window is the main surface for interaction. It stays visible while the AI runs commands, streams output, and keeps you informed about what is happening in real time.

[Learn about the AI Window →](./ai-window)

## Getting Started

1. **Set up your environment** - Install and configure AI Studio on your infrastructure
2. **Configure your LLM provider** - Add API keys for OpenAI, Google Gemini, or other providers
3. **Start coding** - Ask AI Studio to help with implementation, debugging, or testing
4. **Build knowledge spaces** - Organize project documentation and patterns for better context
5. **Customize workflows** - Tailor the system to your team's practices

## What's Next?

- [Coding Workflows](./coding-workflows) - Learn how to use intelligent multi-step workflows
- [Memory Advantage](../ai-studio-memory-advantage) - Understand AI Studio's persistent memory
- [SDK Documentation](../sdk/intro) - Integrate AI Studio into your tools

## Support

AI Studio is open source and community-supported. For SDK licensing customers, we provide 24/7 email-based support for integration and deployment questions.
