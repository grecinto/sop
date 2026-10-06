# Northstar AI, AI Studio, and Local Daemon

Northstar AI is the product/platform and React website for subscriptions, product information, and Joltrin SDK information. AI Studio is Northstar AI's Data Manager web UI, including the AI Chat window. It is served by `httpserver` or the daemon using shared `sopbackend` routes and templates. The Northstar AI website can launch AI Studio in daemon mode.

## Runtime Modes

- **HQ/site mode:** the Data Manager connects to the configured hosted backend and applies its site/admin access rules.
- **Daemon mode:** the Data Manager connects to a local SOP daemon, normally `http://127.0.0.1:9090`. The site grants access to the UI route for authenticated users; the daemon independently authenticates API requests.
- The `daemon_url` query option can select a custom daemon URL. The rendered template safely escapes it for JavaScript.

The daemon-mode launch URL is `/app?mode=daemon`. The Northstar AI website's AI Studio link opens that shared Data Manager surface; it does not launch a second React chat implementation.

## Authentication Flow

The Data Manager performs the daemon's challenge-response bootstrap, then uses daemon-issued access/refresh tokens for protected API calls. The daemon validates that session independently; a site login is not a substitute for daemon authentication.

Use the supported bootstrap and refresh endpoints rather than copying tokens between clients. See [AUTHENTICATION.md](AUTHENTICATION.md) and the daemon's [`SECURITY_PROTOCOL.md`](../lib/sop/cmd/sop-daemon/SECURITY_PROTOCOL.md) for the security contracts.

## Chat and Coding Delegation

The normal AI Studio Chat Send action calls `/api/ask`. Northstar AI Ask/ReAct owns classification, capability routing, memory/context handling, and the outer workflow. For a suitable coding request, Ask can delegate one bounded task through the Joltrin `ideworker.Backend` contract; the daemon defaults to the Cline SDK host.

```text
Data Manager Send
    -> /api/ask
    -> Northstar AI Ask/ReAct
    -> optional bounded Cline SDK task
    -> structured result, validation, and Northstar AI undo history
```

There is one production Send action in AI Studio. Direct `/api/cline/*` routes are for tests and diagnostics, not an alternate production controller. See [Cline SDK integration](CLINE_COPILOT_INTEGRATION_PLAN.md) and the [backend contract](IDE_COPILOT_BACKEND_SDK.md).

### Coding Workspace Selection in Ask

Repository selection is part of the Ask conversation, not a Cline-specific user setting. At the start of a coding objective, Ask uses an explicit path from the current request or a reliable workspace already established for that session. If the target is missing or ambiguous, Ask asks which repository or folder to work in and pauses before delegating or making repository changes. The user's answer resumes the same Ask objective, and Ask passes the selected daemon-visible path as the delegated workspace root.

The user can change scope in a later turn by naming another repository or folder. Ask treats an explicit switch as authoritative for subsequent coding work; a repository change gets an isolated backend session, while a narrower folder request limits the task scope. If the switch is unclear, Ask confirms it before editing. Paths must exist on the machine running the daemon. The coding backend remains an implementation detail; the user interacts with Ask through the standard Chat Send action.

The AI Studio Chat header's **Preferred LLM** selector shows the current primary model and lets you change it for subsequent messages. The final **Pool config** option opens the provider-key settings; choosing a preferred model does not disable Northstar AI's configured fallback behavior.

## Privacy and Provider Keys

Local workspace files and terminal operations are handled by the daemon on the machine where it runs. Model prompts and context are sent to whichever remote model provider is configured; daemon mode does not make provider traffic local.

In daemon mode, the UI encrypts provider API keys through the daemon and stores encrypted values separately from non-secret browser settings. Do not assume keys are plaintext in `localStorage.sop_ai_config`, and do not expose them in logs or command-line arguments. See [Secure Input Handling](SECURE_INPUT_HANDLING.md) and [Fallback Provider Setup](FALLBACK_PROVIDER_SETUP.md).

## Terminal and Worker Behavior

Daemon terminal work may use isolated PTY workers. Interactive commands, background execution, cancellation, and output reporting are described in [Terminal Launch](TERMINAL_LAUNCH_FEATURE.md) and [PTY Worker Isolation](PTY_WORKER_ISOLATION.md).

## Development Checks

From the repository root:

```sh
go test ./src/backend/site ./src/backend/siteapi
go test ./lib/sop/cmd/sop-daemon
```

For the full repository test scopes and Cline SDK host build, see [DEVELOPMENT_GUIDE.md](DEVELOPMENT_GUIDE.md) and [CLINE_COPILOT_INTEGRATION_PLAN.md](CLINE_COPILOT_INTEGRATION_PLAN.md).
