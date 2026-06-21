# Business Model & Commercialization Strategy

This document captures the product framing for the private overlay repository. The public SOP code remains the shared engine; this repository adds the product-specific runtime, UI, and deployment behavior that justify a private commercial layer.

## 1. Private overlay model

The private repository is best treated as an overlay on top of the public SOP engine rather than as a full replacement. The public repository carries the reusable engine and generic AI/runtime primitives; this repository carries the parts that are deployment-specific, product-specific, or proprietary.

- **Public layer**: shared storage, AI runtime primitives, and reusable engine behavior.
- **Private layer**: product UI, host/desktop runtime, private deployment logic, and proprietary workflow behavior.

This lets the public repo stay generic while the private repo focuses on differentiated product value.

## 2. Product differentiation

A key commercial feature will be the "floating tool window" integrated into the UI.

- **Concept**: An intuitive, always-on-top interface that provides AI-powered assistance, script management, and data interaction capabilities directly within the user's workflow.
- **Value Proposition**: This tool will significantly enhance productivity by offering a seamless way to build, test, and execute complex operations without leaving the primary work context. It represents a major step up from a standard CLI or a basic UI.
- **Commercial Justification**: As a sophisticated and powerful feature, the floating tool window will be a primary driver for users to upgrade from the free, open-source version to a paid, commercial license. It will be exclusively available as part of the `sop-cloud` offering.

## 3. Revenue model

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

