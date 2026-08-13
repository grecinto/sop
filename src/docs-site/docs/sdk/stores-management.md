# Stores Management

## Technical reference for developers

**Stores** provide a visual database management experience for AI Studio and its SDK. This page is intended for developers who want to understand how Stores fit into the platform, how to work with them programmatically, and how they differ from other data-oriented features.

This document focuses on technical usage, integration patterns, and implementation workflows rather than day-to-day UI guidance.

## Development workflows: Code-First vs Data-First

AI Studio supports two complementary development approaches, giving you flexibility in how you build and manage your applications:

### Code-First workflow
Define stores and logic in your application code first, then use Stores UI to inspect and manage them:

1. **Create stores programmatically** in your Go/Python/Java application
2. **Define your data structures** and business logic in code
3. **Use Stores UI** to inspect, debug, and manage the data
4. **Iterate** by refining code and verifying results visually

This workflow is ideal when:
- You know your data model upfront
- You want maximum control and type safety
- You're building performance-critical systems
- Your team prefers code-driven development

### Data-First workflow  
Create stores and seed data in the Stores UI, then open them from your application code:

1. **Design your database schema** in the Stores UI
2. **Create and configure stores** visually
3. **Seed initial data** through the interface
4. **Open those stores** from your application code
5. **Build your application logic** around the existing data structures

This workflow is ideal when:
- You're prototyping or exploring data models
- You want to see results immediately
- You're learning the data model
- Non-developers need to participate in schema design

### Hybrid approach
Most teams use both workflows:
- Start **Data-First** for rapid prototyping and schema exploration
- Transition to **Code-First** as the application matures and requirements solidify
- Continue using **Stores UI** for data inspection, debugging, and ad-hoc operations

Both approaches provide full access to AI Studio's data capabilities. Choose the one that best fits your team's preferences and project needs.

## What you can do with Stores

### Browse and inspect data
- View all data stores within your SOP databases
- Inspect items stored in B-tree structures
- Navigate through your database organization
- Search and filter data in real-time

### Manage your data
- Add new items to stores
- Update existing records
- Delete items individually or in batches
- Execute batch operations with scripts

### Query and analyze
- Run queries against your stores
- Join data across multiple stores
- Apply filters and conditions
- View results instantly

### Work with transactions
- Start and manage database transactions
- Commit changes atomically
- Roll back when needed
- Inspect transaction state

## Why Stores Matters for AI Studio Developers

Professional database management tools make development more productive. Stores makes it easier to build, debug, and operate AI Studio applications.

**Accelerates development:**
- Instantly inspect data structures without writing inspection code
- Prototype and iterate visually before committing to code
- Verify that knowledge items and transactions behave correctly
- Debug retrieval logic with real-time feedback

**Lowers barriers to entry:**
- New developers can explore AI Studio visually before diving into the API
- Non-developers can participate in schema design and data operations
- Visual tools make complex B-tree operations intuitive
- Learn the data model through interactive exploration

**Professional operations:**
- Manage production databases with confidence
- Run ad-hoc queries and operations safely
- Clean up test environments efficiently
- Verify data integrity across deployments

**Built for the community:**
Stores gives AI Studio developers a practical visual way to work with structured data alongside the platform's open-source foundation.

## UI vs programmatic API

The AI Studio SDK provides both visual tools and a powerful programmatic API. This dual approach gives teams a flexible developer experience for exploration, automation, and production use.

**Use the Stores UI for:**
- Development and debugging workflows
- Exploring unfamiliar databases
- Ad-hoc queries and data operations
- Administrative tasks and data curation
- Learning AI Studio's data architecture and capabilities

**Use the programmatic API for:**
- Production application logic
- Performance-critical operations
- Automated workflows and pipelines
- Custom business logic
- CI/CD and infrastructure automation

```go
// Production code uses the programmatic API
import "github.com/sharedcode/sop"

db, _ := sop.OpenDatabase(ctx, config)
store := db.Store("users")
item, _ := store.Get(ctx, "user123")
```

Both approaches work together naturally. The visual tools accelerate development and operations, while the programmatic API provides the control and performance needed for production systems.

## AI-powered database operations

Stores integrates with AI Studio, bringing conversational database management to your workspace. This makes database operations accessible through natural language.

**Ask questions about your data:**
- "What stores exist in the system database?"
- "Show me the last 10 knowledge items added"
- "What's in the spaces store?"
- "How many users are in the database?"

**Perform operations conversationally:**
- "Add a test item to the users store"
- "Update the email for user123"
- "Delete all test data from the orders store"
- "Create a new store for product reviews"

**Get AI assistance:**
- Generate migration scripts
- Explain data relationships
- Suggest query optimizations
- Debug data issues

This conversational interface is particularly valuable during development, making database exploration intuitive for both experienced and new AI Studio developers.

## Common use cases

### Building an AI application with knowledge bases
1. Verify knowledge items are indexed correctly using the Stores UI
2. Test semantic search queries through the visual interface
3. Inspect category structures
4. Debug retrieval paths, then implement in your application code

### Migrating data to SOP
1. Write migration scripts in the Stores UI
2. Execute and verify results in real-time
3. Inspect data layout and relationships
4. Iterate quickly with immediate feedback

### Managing knowledge for RAG applications
1. Browse knowledge items by category using Stores
2. Update or remove outdated content through the UI
3. Test different queries visually
4. Verify data organization before deployment

### Debugging production issues
1. Connect to your database via Stores
2. Inspect actual data state through the UI
3. Compare expected vs actual storage
4. Export problem data for analysis

## Next steps

- Learn about the [Vector Database Architecture](./vector-database)
- Explore the [AI Database Platform](./ai-database)
- See [SDK overview](./intro) for programmatic database access
- Visit the [SOP GitHub repository](https://github.com/sharedcode/sop) to learn about the open-source foundation behind the SDK

## Open-Source Foundation

AI Studio builds on the open-source SOP foundation and continues to benefit from its community and persistence technologies.

**What this brings to AI Studio developers:**
- Enterprise-quality tooling at no cost
- Visual database management that rivals commercial products
- AI-powered operations that set new standards for database UX
- Support for both hobbyist and enterprise use cases

**Learn more:** Visit the [SOP community on GitHub](https://github.com/sharedcode/sop) to explore the open-source project behind the SDK.
