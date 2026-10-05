---
sidebar_position: 4
title: Northstar AI Connections
description: Connect Northstar AI securely to the runtime that powers your workspace.
---

# Northstar AI Connections

Northstar AI connects to the runtime that provides access to your files, tools, and project context. This keeps your work close to the environment where it belongs.

## How it works

Choose the Northstar AI connection for your workspace. Once connected, Northstar AI uses a protected session for follow-up requests.

Your workspace connection stays within the environment you choose and does not need to be exposed to the public internet.

## Connect to your workspace

If your workspace provides a connection address, enter it in Northstar AI:

- Example: http://127.0.0.1:9090

If your organization provides a private workspace address, enter that address instead.

## Security and session behavior

- The connection uses a secure session rather than an open public endpoint.
- Keep the workspace connection within the environment where you use it.
- Session credentials are refreshed as needed so the connection stays protected.

## Troubleshooting

- If the workspace is not reachable, the connection will fail.
- Make sure the connection address is correct and available to your environment.
