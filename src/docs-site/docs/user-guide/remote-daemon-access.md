---
sidebar_position: 4
title: Remote Daemon Access
description: Connect AI Studio to a local or remote daemon securely after signing in to HQ.
---

# AI Studio Remote Daemon Access

AI Studio can connect to a daemon running on your own machine or on another machine in your private network. This gives you a secure, private way to run AI workflows close to your data and tools.

## How it works

When you sign in to HQ, AI Studio can use that authenticated session to establish a private connection to the daemon you choose. In version 1, any user who is already authenticated to HQ can connect to a daemon as long as they present a valid HQ-issued token. The daemon does not maintain a separate user store in this release; it relies on the HQ authentication token to authorize the session.

The daemon then uses a short-lived session token for follow-up requests, so the connection stays secure without exposing the daemon to the public internet.

## Connect to a local daemon

If the daemon is running on the same machine, you can usually connect using the local address:

- Example: http://127.0.0.1:9090

## Connect to a remote daemon

If the daemon is running on a different machine, point AI Studio to that machine's address instead. For example:

- Example: http://192.168.1.50:9090

Use the local network IP address or hostname of the machine running the daemon. The machine running AI Studio must be able to reach the daemon over your network.

If your deployment exposes a daemon URL setting or connection field, enter the daemon address there.

## Security and session behavior

- AI Studio authenticates to HQ first.
- The daemon connection is established through a secure daemon session rather than an open public endpoint.
- Sessions are short-lived and refreshed automatically so your connection stays protected without requiring repeated manual sign-in.

## Troubleshooting

- If the daemon is not reachable, the connection will fail.
- Make sure the daemon is running and listening on the correct port.
- If you are using a remote daemon, confirm that your firewall and network routing allow traffic from the client machine.
