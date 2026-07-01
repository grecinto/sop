# Site account backend

This package contains the React/site-specific account model and handlers for users, subscriptions, payments, and billing flows.

## Transaction and B-tree usage notes

- Each named B-tree used by this package must be opened at most once per transaction.
- Re-opening the same store name from the same transaction can corrupt the transaction state and lead to commit-time lock failures.
- The site-account stores therefore reuse a transaction-scoped handle for the bridge/index trees instead of creating a fresh handle on every access.
- When adding new store helpers, keep the same pattern: resolve the store once per transaction and reuse it for the rest of the request or workflow.
