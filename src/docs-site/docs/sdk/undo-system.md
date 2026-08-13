# Undo and Restore

This document explains how AI Studio tracks, searches, and restores coding changes through session-aware workstreams.

## Overview

AI Studio organizes coding work around two connected ideas:

- a **session**, which holds the broader conversation and runtime context
- a **workstream**, which carries one focused line of work through its phases

Undo and restore are attached to that workstream history. As a workstream edits files, AI Studio records enough information to make those changes searchable and restorable later.

This gives you a practical recovery system for real coding work:

- changes stay grouped by the work that produced them
- history can be searched in natural language
- restore can bring back an earlier state when a change should be reversed
- recovery stays connected to the surrounding session and workstream context

## How It Fits the Product Model

Undo is part of the same runtime model that drives the rest of AI Studio.

### Session Context

The session holds the broader conversation context, runtime state, and user interaction history.

### Workstream Context

Inside the session, a workstream tracks the focused task the agent is actively carrying forward. That includes:

- the current phase
- prior phases
- captured artifacts
- completion summaries
- updated files

When a workstream changes code, undo history is recorded against that line of work so the change remains understandable later.

### Restore Records

Each undoable change set is stored as a restore record with its own internal identifier. That identifier is useful for storage, retrieval, and restore operations, but the user-facing concept remains the workstream history for a session.

## What Gets Stored

When AI Studio makes file changes during a workstream, it stores:

- metadata about the change
- per-file restore data
- summaries of what changed
- code artifact information for search
- session and workstream identifiers when available

This means undo history is not just a raw patch log. It can also carry enough semantic context to answer questions such as:

- "show me the auth changes from earlier"
- "restore the parser fix from this morning"
- "find the work where session timeout logic changed"

## Storage Design

The undo system uses a layered design so restore remains practical while search stays useful.

### Layer 1: Semantic Space Storage

Searchable undo history is indexed in Space so records can be found by meaning, time, repository area, and related code artifacts.

This layer supports:

- semantic search
- repository-oriented organization
- workstream review history
- time-aware ranking

### Layer 2: File Restore Data

For each changed file, AI Studio stores the restore data needed to reconstruct or reverse a prior state.

The current implementation focuses on per-file restore payloads and change summaries rather than a heavyweight full-history system.

### Layer 3: Working Metadata

Undo records also carry operational metadata such as status, execution timing, retry counts, and ownership fields like session and workstream identifiers.

## Search Model

Undo history is designed to be searched the way people remember work: by topic, component, file area, or time window.

### What Search Uses

Search can draw on:

- description text
- workflow type labels
- file paths
- extracted code artifacts
- repository grouping
- timestamps

This makes it possible to search for changes by intent as well as by file location.

### Time and Meaning Together

Results are not only matched semantically. They can also be ranked with recency in mind, which helps when several related changes exist.

That means a query such as "restore the latest auth middleware fix" can prefer a recent relevant record over an older one with similar wording.

## Restore Operations

Restore is built around practical recovery flows.

### Full Restore

You can restore the full change set captured for a record when an entire line of work should be reversed.

### Selective Restore

You can also restore only the files that matter when the broader workstream was useful but one part of it should be backed out.

### Preview Before Restore

Restore can be previewed before changes are applied so the user can inspect what will happen first.

## Repository and Code Awareness

Undo history is more useful when it understands where a change belongs.

The system enriches restore records with code-aware signals such as:

- namespaces
- packages
- files
- functions
- types

Those signals help organize undo history by meaningful repository structure instead of only by timestamp.

## Workstream Review History

Undo records are also stored in a review-friendly structure so AI Studio can present prior work in a way that follows how coding tasks were actually carried out.

In practice, this supports views such as:

- recent change history for a repository
- grouped workstream summaries
- file-level review entries
- semantic browsing of change records

## API and Internal Record IDs

The current backend APIs still use record identifiers that are transaction-shaped in naming. That is mainly an implementation and transport detail.

For public documentation, the more accurate mental model is:

- sessions hold the broader runtime context
- workstreams hold the active line of work
- undo stores restorable change history for that workstream

So while an API may refer to an internal record ID for restore, the feature itself should be understood as workstream-based history and recovery.

## Storage and Retention

Undo history is designed to stay compact enough for regular coding use while still preserving useful restore information.

Retention policies can be used to control:

- how long history is kept
- how many records are retained
- how much disk usage is allowed
- whether important milestones should be preserved longer

This makes the system suitable for day-to-day coding work without turning local history into an unmanaged archive.

## SDK Integration

For SDK users, the main integration point is straightforward: when your coding flow edits files inside a workstream, the undo layer can capture those changes and attach them to the surrounding session and workstream context.

That gives your integration:

- searchable restore history
- file-aware rollback behavior
- semantic indexing for prior changes
- continuity with the rest of AI Studio's runtime model

## Summary

AI Studio's undo system is best understood as **session-aware, workstream-based change recovery**.

It keeps restore history connected to the actual work that produced it, makes that history searchable, and supports safe recovery when part or all of a coding task should be reversed.

The important public model is simple:

- **session** provides the broader context
- **workstream** carries the task
- **undo history** preserves the restorable changes from that work
