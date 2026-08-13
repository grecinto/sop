---
sidebar_position: 4
title: AI Studio Connections
description: Connect AI Studio securely to the runtime that powers your workspace.
---

# AI Studio Connections

AI Studio connects to the runtime that provides access to your files, tools, and project context. This keeps your work close to the environment where it belongs.

## How it works

Choose the AI Studio connection for your workspace. Once connected, AI Studio uses a protected session for follow-up requests.

Your workspace connection stays within the environment you choose and does not need to be exposed to the public internet.

## Connect to your workspace

If your workspace provides a connection address, enter it in AI Studio:

- Example: http://127.0.0.1:9090

If your organization provides a private workspace address, enter that address instead.

## Security and session behavior

- The connection uses a secure session rather than an open public endpoint.
- Keep the workspace connection within the environment where you use it.
- Session credentials are refreshed as needed so the connection stays protected.

## Troubleshooting

- If the workspace is not reachable, the connection will fail.
- Make sure the connection address is correct and available to your environment.
