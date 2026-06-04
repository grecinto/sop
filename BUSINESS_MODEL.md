# Business Model & Commercialization Strategy

This document outlines the business model and commercialization strategy for the `sop` (Shared Object Platform) project.

## 1. Open Core Model

We will adopt an "Open Core" business model. This model allows us to maintain a free, open-source core project while offering commercial products and services with advanced features built upon it.

- **Public Repository (`sop`)**:
    - **License**: MIT License.
    - **Content**: The core `sop` database engine, its command-line interface (CLI), and the Go language bindings. This repository will remain open and will be the hub for community contributions.
    - **Purpose**: To foster a community, encourage adoption, and establish `sop` as a standard.

- **Private Repository (`sop-cloud`)**:
    - **License**: Proprietary/Commercial.
    - **Content**: This repository will contain all commercial code, including:
        - The advanced, user-friendly UI, featuring the **floating tool window**.
        - Cloud services and "Software as a Service" (SaaS) offerings.
        - Managed hosting solutions.
        - Enterprise-grade features (e.g., advanced security, analytics, integrations).
        - Bindings for other languages that are part of the commercial offering.
    - **Purpose**: To generate revenue by selling products and services to professional and enterprise users.

## 2. The Floating Tool Window

A key commercial feature will be the "floating tool window" integrated into the UI.

- **Concept**: An intuitive, always-on-top interface that provides AI-powered assistance, script management, and data interaction capabilities directly within the user's workflow.
- **Value Proposition**: This tool will significantly enhance productivity by offering a seamless way to build, test, and execute complex operations without leaving the primary work context. It represents a major step up from a standard CLI or a basic UI.
- **Commercial Justification**: As a sophisticated and powerful feature, the floating tool window will be a primary driver for users to upgrade from the free, open-source version to a paid, commercial license. It will be exclusively available as part of the `sop-cloud` offering.

## 3. Revenue Streams

The primary revenue streams will be:

- **Licenses**: Selling licenses for the commercial version of the `sop` application, which includes the advanced UI and floating tool window.
- **SaaS Subscriptions**: Offering tiered subscription plans for cloud-hosted versions of `sop`.
- **Support & Services**: Providing enterprise-level support, training, and consulting services.

## 4. Technical Architecture Models

To support both our SaaS offerings and advanced on-premise or desktop use-cases, we will support two primary technical architectures for AI and RAG (Retrieval-Augmented Generation) processing.

### a. Standard Server-Side RAG (Default Model)

This is the default architecture for our cloud/SaaS product.

-   **Description**: All AI processing, including RAG, happens on the server. The client is a "thin" interface that interacts with the powerful `sop-cloud` backend.
-   **Workflow**:
    1.  The client sends a query to the server.
    2.  The server retrieves relevant context from its knowledge bases (vector stores, databases).
    3.  The server constructs an augmented prompt with the query and the retrieved context.
    4.  The server sends the prompt to the LLM.
    5.  The server receives the response and streams it back to the client.
-   **Use Case**: Ideal for web interfaces, standard deployments, and scenarios where the client device has limited resources. This model ensures consistency, security, and centralized management.

### b. Hybrid Client-Side RAG (Advanced Model)

This architecture is designed for advanced users and will be a key feature of the commercial desktop application.

-   **Description**: This hybrid model leverages client-side resources for the RAG process, while the server maintains control over orchestration, metering, and access to certain features. It allows users to use their own LLM models and local data securely.
-   **Workflow**:
    1.  The client application (e.g., the commercial desktop app with the floating tool window) has its own local RAG capabilities.
    2.  The server's role shifts to **delegation and metering**. It authenticates the user and logs the transaction for billing/analytics but offloads the core AI work.
    3.  The client application performs the RAG process locally: it retrieves context from local data sources, constructs the prompt, and interacts directly with the user's configured LLM (which could be a local model or a cloud-based one).
-   **Use Case**: Perfect for developers, data scientists, and enterprises who need to work with sensitive local data, use custom or local LLMs, and require offline capabilities. This provides maximum flexibility and privacy while still being part of the commercial `sop` ecosystem.

