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

The SDK provides:

- **Vector Database**: A new architecture that eliminates K-Means clustering
- **AI Database Platform**: ACID transactional B-trees optimized for AI workloads
- **Swarm Computing**: Distributed, resilient retrieval and memory infrastructure with no single point of failure
- **End-user hosting**: The SDK can run on customer servers, private infrastructure, or edge deployments
- **Integration Options**: Use the SDK through UI, API, or tool calls
- **Parallel Ask Flow**: Split complex requests into parallel sub-asks, gather the results at the end, and keep each execution (proess) isolated so one failing task does not bring down the others or the daemon
- **KB-driven workflows**: The agent can follow knowledge-base-defined workflows(data driven!) for tasks such as "fix failing tests", orchestrating the steps from failure capture to analysis and verification
- **Dynamic Planner & Workflow Architecture**: AI-generated workflows with participant pattern execution through the Ask pipeline, ensuring 100% pipeline reuse with provider continuity and memory integration
- **Workflow Undo System**: Semantic, domain-aware undo with multi-dimensional indexing, natural language search, and time-ranked results. Each workflow runs as an atomic transaction that can be intelligently searched and restored

## Why this matters for AI platforms

SOP is not just a vector index. It is an AI-native platform for memory, routing, retrieval, and distributed execution. The architecture is designed to scale through independent nodes rather than a single centralized bottleneck, which makes it well suited for copilots, agents, and other real-time AI systems.

Key advantages include:

- **No single point of failure**: one node can fail without taking the whole system offline
- **Elastic scale**: capacity grows by adding more independent nodes rather than expanding one monolithic service
- **Real-time local retrieval**: important context can stay local and low-latency while still participating in a larger distributed network when needed
- **Lower infrastructure overhead**: because the SDK can be hosted in end-user servers, it reduces reliance on always-on cloud vector services
- **Better fit for AI workloads**: retrieval and memory operate as first-class platform capabilities, not as an afterthought

## Next Steps

- Learn about the [Vector Database Architecture](./vector-database)
- Explore the [AI Database Platform](./ai-database)
- Understand the [Workflow Architecture](./workflow-architecture) - Dynamic Planner, KB workflows, and the Ask pipeline execution model
- Read about the [Workflow Undo System](./undo-system) - Transaction management, multi-dimensional indexing, and semantic search integration
