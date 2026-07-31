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
- **Persistent memory**: Learns from your codebase and workflow patterns
- **Structured workflows**: Multi-step intelligent task execution
- **Privacy-first**: Your code and data never leave your environment
- **Open source**: Inspect, extend, and customize everything

## Key Features

### Intelligent Coding Workflows
AI Studio breaks down complex tasks into structured, multi-step workflows that guide you from problem to solution:

**Smart Planning** 🤖 - Just describe what you want to accomplish, and AI Studio figures out the steps needed. Whether you're debugging a tricky issue, implementing a new feature, or refactoring code, it creates a custom plan that fits your specific situation.

**Proven Templates** 📚 - For common tasks like testing, debugging, or refactoring, AI Studio can follow battle-tested patterns from your team's knowledge base. This ensures consistency and lets you encode your team's best practices.

**Real-Time Adjustments** - Change your mind mid-workflow? Just type your modification and the AI adapts without starting over. Add error handling, include extra validation, or refine the approach as you go.

**Transparent Reasoning** 💬 - AI Studio shows you what it understood from your request before taking action. See the execution plan, significant discoveries during analysis, and LLM thinking—all in collapsible sections so you control the level of detail. This transparency lets you interject or correct before execution begins.

**Intelligent Undo** 🔄 - Experiment fearlessly with an industry-leading undo system that treats each workflow as a searchable transaction. Find and restore past changes using natural language: "undo authentication middleware changes" just works. No git history pollution, no manual bisecting.

**Multi-language support**: Works seamlessly with Go, Python, Java, Rust, JavaScript, TypeScript, and C#. Automatically detects your project type and uses the right tools for testing, analysis, and code generation.

[Learn about Coding Workflows →](./coding-workflows)

### Persistent Memory System
Unlike stateless copilots, AI Studio maintains context across sessions through structured knowledge spaces. It remembers your coding patterns, project structure, and preferences.

[Learn about Memory & Spaces →](./space-management)

### Fleet Learning
Optionally share validated coding patterns across your team without exposing proprietary code. Improve the AI's suggestions based on what has worked well in your organization.

[Learn about Fleet Learning →](../fleet-learning)

### Remote Daemon Access
Connect AI Studio to daemons running on local or remote machines in your private network. Work securely without exposing systems to the public internet.

[Learn about Remote Access →](./remote-daemon-access)

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
