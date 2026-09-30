# SDK Documentation

**Coming soon**: SDK licensing with access to our 24/7 email-based support system.

This section covers the SDK architecture: the vector database design, the comparison to traditional centroid-based systems, and the AI database platform.

Traditional vector databases use overnight K-Means clustering in data centers to organize embeddings. The SDK architecture uses **nested semantic categories** and a **relativity-based Triangulation algorithm**, making vectorization a **general-purpose infrastructure capability** accessible through the **UI, API, LLM management, or tool calls**.

<div>
  <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
    <path d="M12 2a10 10 0 1 0 10 10A10.01 10.01 0 0 0 12 2Zm0 2a7.95 7.95 0 0 1 6.9 4.05A8.38 8.38 0 0 0 14.2 8.3c-1.2 0-2.3.34-3.27.94A6.97 6.97 0 0 1 12 4Zm-3.8 3.3a6.97 6.97 0 0 1 2.5-.8c.4 0 .79.04 1.17.12a8.53 8.53 0 0 0-1.87 2.2A6.97 6.97 0 0 1 8.2 7.3Zm-1.4 2.74A6.98 6.98 0 0 1 11 6.03c.2.43.36.89.46 1.37A9.2 9.2 0 0 0 7.8 10.04Zm10.79 3.8a8.25 8.25 0 0 1-2.97 2.57 6.95 6.95 0 0 1-4.5-.53c.76-.81 1.3-1.78 1.59-2.87a10.05 10.05 0 0 0 4.24 1.07c.56 0 1.1-.05 1.64-.24Zm-2.76 4.12a7.94 7.94 0 0 1-5.57-.95 8.35 8.35 0 0 0 2.47-1.7c.74-.77 1.24-1.77 1.56-2.9a8.95 8.95 0 0 1 1.54 5.55Z"/>
  </svg>
  <span>Efficient by design</span>
</div>

## Overview

The SDK originates from [Scalable Objects Persistence (SOP)](https://github.com/sharedcode/sop), the MIT-licensed open source foundation beneath AI Studio.

The SDK provides:

- **Vector Database**: A new architecture that eliminates K-Means clustering
- **AI Database Platform**: ACID transactional B-trees optimized for AI workloads
- **Swarm Computing**: Distributed, resilient retrieval and memory infrastructure with no single point of failure
- **End-user hosting**: The SDK can run on customer servers, private infrastructure, or edge deployments
- **Integration Options**: Use the SDK through UI, API, or tool calls
- **Parallel Ask Flow**: Split complex requests into parallel sub-asks, gather the results at the end, and keep each execution isolated so one failing task does not affect the others
- **KB-driven workflows**: The agent can follow knowledge-base-defined workflows(data driven!) for tasks such as "fix failing tests", orchestrating the steps from failure capture to analysis and verification
- **Dynamic Planner & Workflow Architecture**: AI-generated workflows with participant pattern execution through the Ask pipeline, ensuring 100% pipeline reuse with provider continuity and memory integration
- **Phase + domain token efficiency**: Structured phases and domain-aware tool surfaces keep the active prompt focused, reduce irrelevant tool exposure, and cut wasted retry turns without falling back to rigid routing
- **Multi-language coding workflows**: Shared readers/analyzers and workflow/script generators support Go, Python, JavaScript/TypeScript, Java, Rust, C#, HTML/CSS, React, and Angular
- **Ask as Participant**: Use AI Studio Ask as a bounded participant through typed in-process or HTTP contracts, including integrations with external agent controllers
- **Undo and Restore**: Session-aware, workstream-based change history with natural language search and restore support

## Why this matters for AI platforms

AI Studio is more than a vector index. It brings memory, routing, retrieval, and distributed execution together in one platform for copilots, agents, and real-time AI systems.

Key advantages include:

- **No single point of failure**: one node can fail without taking the whole system offline
- **Elastic scale**: capacity grows by adding more independent nodes rather than expanding one monolithic service
- **Real-time local retrieval**: important context can stay local and low-latency while still participating in a larger distributed network when needed
- **Lower infrastructure overhead**: because the SDK can be hosted in end-user servers, it reduces reliance on always-on cloud vector services
- **Better fit for AI workloads**: retrieval and memory operate as first-class platform capabilities, not as an afterthought

## Why Phase + Domain Conserves Tokens

The workflow architecture is not only about control flow. It is also a token-efficiency strategy.

By combining **phase awareness** with **domain awareness**, AI Studio avoids the common failure mode where every turn has to expose every tool, every instruction set, and every recovery path at once.

This reduces token waste in three ways:

- **Smaller active tool surface**: Investigation, Planning, Execution, Review, and Delivery each foreground only the kinds of actions that make sense in that moment.
- **Smaller active domain surface**: The model reasons inside the relevant domain slice instead of paying prompt budget for unrelated platform capabilities.
- **Fewer wasted retries**: When the workflow can pause for clarification, move backward to the right phase, or stop cleanly, it avoids spending tokens on blind continuation.

The result is better per-token usefulness, not just a smaller prompt. AI Studio keeps the LLM in control while still making each turn denser and more relevant.

For a customer-facing explanation of why this matters competitively, read [Why AI Studio Uses Fewer Tokens](../token-efficiency-advantage).

## Next Steps

- Read the [SDK History](./history) page for the SOP origin, licensing, and source-availability model
- Learn about the [Vector Database Architecture](./vector-database)
- Explore the [AI Database Platform](./ai-database)
- Read [Why AI Studio Uses Fewer Tokens](../token-efficiency-advantage)
- Understand the [Workflow Architecture](./workflow-architecture) - dynamic phases, workstreams, and the Ask pipeline execution model
- Read about [IDE Copilot Backends](./ide-copilot-backend) - how SOP Ask delegates bounded coding work to its configured backend, with Cline SDK as the default
- Follow the [Ask Participant Mode](./ask-participant-mode) guide for concrete embedding, HTTP, and NDJSON streaming examples
- Read about [Undo and Restore](./undo-system) - session-aware workstream history, search, and recovery
