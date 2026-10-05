---
sidebar_position: 1
title: AI Studio User Guide
description: Complete guide to using AI Studio, the Northstar AI interface, for coding, debugging, and knowledge management.
---

# AI Studio User Guide

Welcome to AI Studio, the user interface for Northstar AI, an intelligent coding platform that runs on your own infrastructure with persistent memory, adaptive phases, and privacy-first architecture.

## What is Northstar AI?

Northstar AI is a self-hosted AI platform that helps you code, debug, test, and manage knowledge in your own environment. It gives you:

- **Full control**: Runs on your infrastructure, uses your chosen LLM provider
- **LLM as Orchestrator**: The AI chooses the right approach and tools for each task
- **Persistent memory**: Keeps useful context from your codebase and workstreams
- **Privacy-first**: Your code and data never leave your environment
- **Open source**: Inspect, extend, and customize everything

### The LLM as Orchestrator Paradigm

Northstar AI is built around a simple principle: **the LLM guides the work**. It reasons about your task, chooses an approach, and adapts as the work develops.

**What this means for you:**
- 🎯 **Adaptive execution** - The AI chooses the right approach for your specific situation, not a pre-programmed script
- 🧠 **Intelligent decision-making** - Task classification, tool selection, and flow control all driven by AI reasoning
- 🔄 **Natural adjustments** - The AI can pivot, revisit phases, or change strategy based on what it discovers
- 🛟 **Built-in recovery** - If execution is stuck, the AI can pause for clarification, move backward to the right phase, or stop cleanly instead of forcing a broken plan forward
- 💬 **Conversational control** - You guide with natural language, not configuration files

This gives each task a natural flow that can respond to your goals, feedback, and discoveries.

## Key Features

### Intelligent Coding with LLM Orchestration
Built on the **LLM as Orchestrator** paradigm, Northstar AI breaks down complex tasks intelligently—the AI reasons about your task and orchestrates the solution dynamically:

**Smart Planning** 🤖 - Just describe what you want to accomplish, and the AI analyzes your task, determines the best approach, and generates a custom plan. Whether debugging, implementing features, or refactoring, it adapts to your specific situation—no pre-programmed scripts.

**Proven Templates** 📚 - For common tasks like testing, debugging, or refactoring, Northstar AI can follow battle-tested patterns from your team's knowledge base. This ensures consistency and lets you encode your team's best practices.

**Real-Time Adjustments** - Change your mind mid-execution? Just type your modification and the AI adapts its orchestration without starting over. Add error handling, include extra validation, or refine the approach as you go.

**Phase Recovery** - Northstar AI can revisit Investigation or Planning, pause for one missing fact, or stop when the environment requires your attention.

**Transparent Reasoning** 💬 - Northstar AI shows you what it understood from your request before taking action. See the execution plan, significant discoveries during analysis, and LLM thinking—all in collapsible sections so you control the level of detail. This transparency lets you interject or correct before execution begins.

**Intelligent Undo** 🔄 - Experiment freely with searchable restore history connected to your sessions and workstreams. Find and restore past changes using natural language, such as "undo authentication middleware changes."

**Multi-language support**: Works seamlessly with Go, Python, JavaScript/TypeScript, Java, Rust, C#, HTML/CSS, React, and Angular. Automatically detects your project type and uses the right tools for testing, analysis, and code generation.

[Learn about Intelligent Coding →](./coding-workflows)

### Persistent Memory System
Unlike stateless copilots, Northstar AI maintains context across sessions through structured knowledge spaces. It remembers your coding patterns, project structure, and preferences.

[Learn about Memory & Spaces →](./space-management)

### Fleet Learning
Optionally share validated coding patterns across your team without exposing proprietary code. Improve the AI's suggestions based on what has worked well in your organization.

[Learn about Fleet Learning →](../fleet-learning)

### Northstar AI Connections
Connect Northstar AI to the runtime that powers your workspace and work securely with your files and tools.

[Learn about Northstar AI Connections →](./remote-daemon-access)

### Coding Execution
The AI Studio Data Manager has one Chat Send action. Northstar AI Ask owns classification and orchestration, and may delegate a bounded coding task to the daemon's default Cline SDK backend. Continue remains compatibility/reference code, not a required editor companion.

[Learn about IDE copilot backends →](../sdk/ide-copilot-backend)

### AI Window Experience
The AI window is the main surface for interaction. It stays visible while the AI runs commands, streams output, and keeps you informed about what is happening in real time.

[Learn about the AI Window →](./ai-window)

## Getting Started

1. **Set up your environment** - Install and configure Northstar AI on your infrastructure
2. **Configure your LLM provider** - Add API keys for OpenAI, Google Gemini, or other providers
3. **Start coding** - Ask Northstar AI to help with implementation, debugging, or testing
4. **Build knowledge spaces** - Organize project documentation and patterns for better context
5. **Customize your workspace** - Tailor the system to your team's practices

## What's Next?

- [Coding Workflows](./coding-workflows) - Learn how to use intelligent multi-step workflows
- [Memory Advantage](../ai-studio-memory-advantage) - Understand Northstar AI's persistent memory
- [Joltrin SDK](../sdk/intro) - Integrate the SDK into your tools

## Support

Northstar AI is open source and community-supported. For SDK licensing customers, we provide 24/7 email-based support for integration and deployment questions.
