# Subscription and Payment Architecture

## Recommendation

Use Stripe for recurring subscriptions.

Stripe is the most practical choice for this product because it provides:
- hosted checkout for a fast, secure start,
- built-in recurring billing for monthly subscriptions,
- support for cards plus Apple Pay and Google Pay,
- webhook-based event handling for renewals, failures, and cancellations,
- and a much smaller operational burden than building a custom payments stack.

## Why not use Google Wallet directly as the primary billing system?

Google Pay/Wallet is a payment method surface, not a complete subscription billing platform. For recurring monthly payments, the simpler and safer path is to use Stripe as the processor and let Stripe surface Google Pay as an available payment method.

This keeps the implementation:
- easier to launch,
- easier to maintain,
- and much more secure.

## Proposed flow

1. The React site opens a secure checkout flow.
2. The backend creates a Stripe Checkout Session with a fixed monthly price.
3. Stripe handles card entry and payment method confirmation.
4. Stripe sends webhook events to the backend.
5. The backend updates the user subscription state in the shared auth/user store.

## Security model

- Never store raw card data in the application database.
- Keep Stripe secret keys on the server only.
- Verify Stripe webhook signatures server-side.
- Use HTTPS everywhere.
- Use idempotency for webhook processing.

## Minimal database footprint

Use a robust billing model with separate entities:

### users
- id
- username
- email
- password hash
- role
- status
- created_at
- updated_at

### subscriptions
- id
- user_id
- product_code or plan_code
- provider_subscription_id
- status
- current_period_start
- current_period_end
- cancel_at_period_end
- created_at
- updated_at

### payments
- id
- user_id
- subscription_id
- provider_payment_id
- amount
- currency
- status
- provider_event_id
- created_at
- updated_at

This is enough to power:
- multiple plans or products per user,
- upgrades/downgrades and cancellations,
- renewal handling,
- payment history,
- and account UI messaging.

## Suggested implementation path

Phase 1:
- add Stripe Checkout integration for the Starter Kit plan,
- store the resulting customer/subscription IDs in the shared backend state,
- expose account status and subscription state to the frontend.

Phase 2:
- add webhook handling for subscription created, updated, canceled, and payment failed,
- update the UI based on the backend state.

Phase 3:
- add invoice history and user-facing billing status later if needed.

## Practical recommendation

Start with Stripe Checkout.

It gives the fastest path to a secure monthly subscription flow with minimal custom code and very little database complexity.
