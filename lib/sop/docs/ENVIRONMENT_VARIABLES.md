# Environment Variables and Secret Keys

Use this page as the single reference for the environment variables that affect authentication, signing, and encrypted persistence in SOP.

## Required or recommended secrets

### SOP_SYSTEM_ADMIN_BOOTSTRAP_SECRET
- What it is: the one-time bootstrap secret used to create the initial system-admin/root account.
- Purpose: allows the setup wizard and config save flow to initialize the first privileged account safely.
- How to use it: set it before booting the server or before running the setup wizard.
- Guidance: store it in a secret manager or deployment secret store; do not commit it to source control.

### SOP_SESSION_SECRET
- What it is: the secret used to sign and verify session tokens and bearer tokens.
- Purpose: protects the built-in auth flow so tokens cannot be forged or tampered with.
- Guidance: use a long random value and rotate it carefully if you need to invalidate existing sessions.

### SOP_ENCRYPTION_KEY
- What it is: the key used to encrypt sensitive values before they are written to disk.
- Purpose: protects secrets such as passwords, session secrets, emails, and billing identifiers in config persistence.
- Guidance: use a strong, secret value and keep it consistent across restarts and replicas. The same key must be present whenever encrypted config data is loaded.
- Recommended format: 32-byte value, preferably generated from a secure random source.

## Example shell export

```bash
export SOP_SYSTEM_ADMIN_BOOTSTRAP_SECRET="replace-with-a-long-random-secret"
export SOP_SESSION_SECRET="replace-with-a-long-random-session-secret"
export SOP_ENCRYPTION_KEY="0123456789abcdef0123456789abcdef"
```

A copyable template is also available at [.env.example](../.env.example).

## Operational guidance
- Keep all secrets in a managed secret store if possible.
- Avoid hard-coding secrets in scripts, Dockerfiles, or repository files.
- If you add new runtime secrets in the future, document them here first so deployment guidance stays centralized.
