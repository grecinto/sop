---
sidebar_position: 3
title: Space Content Management
description: Organize knowledge in Spaces, manage categories, and vectorize content for AI retrieval.
---

# Space Content Management

## Overview

Spaces in AI Studio are semantic memory systems that organize knowledge through **nested categories** and **vector embeddings**. This guide covers best practices for managing content, categories, and vectorization.

## Core Concepts

### Categories

Categories are the primary organizational structure in a Space. They create a hierarchical tree that helps both humans and AI systems navigate and retrieve information efficiently.

**Key points:**
- Categories can be nested to create meaningful hierarchies
- Each category can contain items (documents, notes, references)
- Categories participate in semantic search through **category center vectors**

### Vector Embeddings

Vector embeddings are mathematical representations of content that enable semantic search. When you vectorize content:

1. **Text is converted to embeddings** - Each item's content becomes a vector (array of numbers)
2. **Semantic similarity** - Similar content has similar vectors
3. **Fast retrieval** - The system can quickly find relevant items using vector math

## Vectorization Workflow

### When to Vectorize

⚠️ **Important**: Categories and their items must be **vectorized** to participate in semantic search and AI-assisted retrieval.

Vectorize your categories when:
- ✅ You've added new items to a category
- ✅ You've updated existing content
- ✅ You want the AI to find and use this knowledge
- ✅ You're preparing a Space for AI chat sessions

### Vectorization Scopes

The UI offers three vectorization options:

#### 1. **Entire Space**
Vectorizes all items across all categories in the Space.

**Use when:**
- Setting up a new Space
- After bulk imports or updates
- Normalizing embedder dimensions across the Space

#### 2. **Current Category**
Vectorizes all items in the currently selected category and its subcategories.

**Use when:**
- You've updated content in a specific category
- You want to incrementally vectorize large Spaces
- You're working with a specific domain area

#### 3. **Current Page**
Vectorizes only the items visible on the current page.

**Use when:**
- Making quick updates to a few items
- Testing vectorization with a small batch
- Working with rate-limited embedder providers

### How to Vectorize

1. **From the Space View:**
   - Click the ✨ sparkle icon next to "Categories" header
   - Choose your vectorization scope
   - Click "Confirm Vectorize"

2. **Progress Tracking:**
   - Vectorization runs as a background task
   - Progress is shown in the task monitor
   - Large Spaces may take several minutes

## Best Practices

### Organize Before Vectorizing

1. **Create a clear category structure** - Group related content together
2. **Add all relevant items** - Batch additions are more efficient
3. **Review content quality** - Better content = better embeddings
4. **Then vectorize** - Run vectorization after organization is complete

### Incremental Updates

For active Spaces with frequent updates:

1. **Add or update items** in a category
2. **Vectorize that category** (not the entire Space)
3. **Test with AI queries** to verify the content is discoverable
4. **Repeat** as needed

### Embedder Consistency

⚠️ **Dimension Mismatch Warning**: If you change the Space's embedder (e.g., from local Nomic to Google Gemini), the dimension of vectors may change.

**Resolution:**
- The UI will warn you about dimension mismatches
- **Vectorize the entire Space** to normalize all vectors
- Partial vectorization with mismatched dimensions will fail

### Non-Vectorized Categories

Categories that are **not vectorized** will still:
- ✅ Appear in the Space navigation tree
- ✅ Be viewable and editable through the UI
- ❌ **NOT** participate in semantic search
- ❌ **NOT** be retrieved by AI during research

**This is by design** - you can create Spaces with both vectorized (searchable) and non-vectorized (reference-only) content.

## Semantic Search Flow

When the AI performs research in a vectorized Space:

1. **Query vectorization** - Your question becomes an embedding
2. **Navigate the category tree** - Find the nearest root category
3. **Drill down** - Use category centers as anchors to find relevant subcategories
4. **Retrieve items** - Find the most semantically similar items
5. **Ground the response** - AI answers using retrieved content

This triangulation algorithm is **O(log N) per level**, making it fast even for large Spaces.

## Common Workflows

### Courseware / Learning Material

```
📁 Course Name (Space)
  ├─ 📂 Week 1: Introduction
  │   ├─ Lecture Notes
  │   ├─ Reading List
  │   └─ Exercises
  ├─ 📂 Week 2: Core Concepts
  │   └─ ...
  └─ 📂 Resources
      ├─ Glossary
      └─ Reference Materials
```

**Vectorization strategy:**
- Vectorize entire Space after initial setup
- Vectorize individual weeks as content is updated
- Keep the Resources category always vectorized for quick lookup

### Code / Technical Knowledge

```
📁 Project Best Practices (Space)
  ├─ 📂 Architecture
  │   ├─ Design Patterns
  │   └─ System Overview
  ├─ 📂 Testing
  │   ├─ Unit Test Conventions
  │   └─ Integration Patterns
  └─ 📂 Debugging
      ├─ Common Errors
      └─ Troubleshooting Guide
```

**Vectorization strategy:**
- Vectorize categories as they reach a stable state
- Keep debugging and testing categories current
- Vectorize architecture once per major version

### Research / Documentation

```
📁 Research Notes (Space)
  ├─ 📂 Literature Review
  ├─ 📂 Methodology
  ├─ 📂 Data Analysis
  └─ 📂 Drafts (non-vectorized)
      └─ Work in Progress
```

**Vectorization strategy:**
- Keep completed sections vectorized
- Leave "Drafts" non-vectorized until content stabilizes
- Vectorize entire Space before publication

## Monitoring

### Check Vectorization Status

- **Last Vectorized**: Shown in Space Settings (Edit Space modal)
- **Item Count**: Visible in category view
- **Task History**: Check completed vectorization tasks in task monitor

### Verify Semantic Search

Test your vectorization:

1. **Open AI Chat**
2. **Select your Space** in the dropdown
3. **Ask a question** related to your content
4. **Check the response** - Does it reference the right categories?

If the AI doesn't find your content, the category likely needs vectorization.

## Troubleshooting

### "No items to vectorize" Error

**Cause**: The selected category or page has no items.

**Solution**: 
- Add items to the category first
- Or select a different scope (entire Space or parent category)

### Dimension Mismatch Error

**Cause**: You changed the Space's embedder, and vectors have different dimensions.

**Solution**:
- Vectorize the **entire Space** to regenerate all vectors
- Or revert to the original embedder

### Items Not Found in AI Search

**Cause**: Category or items haven't been vectorized yet.

**Solution**:
- Vectorize the category containing the content
- Wait for vectorization task to complete
- Test again with AI chat

## Summary

✨ **Remember**: Vectorization is what makes your content discoverable by AI. Organize your categories thoughtfully, then vectorize to unlock semantic search and intelligent retrieval.

For more details on the underlying algorithm, see [Vector Database Architecture](../sdk/vector-database.md).
