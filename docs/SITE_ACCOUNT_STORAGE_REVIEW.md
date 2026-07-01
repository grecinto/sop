# Site Account Storage Review

This note documents the current site-account persistence model for review. It focuses on the key/value structures, the B-tree-backed stores, and the relationships between them.

## Summary

The site account backend persists three independent SOP B-tree stores:

- Users store: string key -> UserRecord
- Subscriptions store: string key -> SubscriptionRecord
- Payments store: string key -> PaymentRecord

Each store is backed by a string key and a typed value record. The stores are created and opened through the shared SOP database layer using the site-specific store names.

## Store Map

| Store | Key type | Value type | Store name | Purpose |
| --- | --- | --- | --- | --- |
| Users | string | UserRecord | site_users | Stores account identity and billing state |
| Subscriptions | string | SubscriptionRecord | site_subscriptions | Stores billing subscription rows |
| Payments | string | PaymentRecord | site_payments | Stores payment events and outcomes |

## Key / Value Structures

### UserRecord

Key: normalized username string

Value fields:

- Username
- Role
- PasswordHash
- Email
- SubscriptionPlan
- SubscriptionFeatures
- BillingStatus
- CheckoutURL
- LastWebhookEvent
- LastWebhookAt
- FailureReason
- StripeCustomerID
- StripeSubID
- Status
- CreatedAt
- UpdatedAt

This is the primary identity record used by login, registration, status lookup, and checkout/billing updates.

### SubscriptionRecord

Key: subscription ID string

Value fields:

- ID
- UserID
- PlanCode
- ProviderSubscription
- Status
- CurrentPeriodStart
- CurrentPeriodEnd
- CancelAtPeriodEnd
- CreatedAt
- UpdatedAt

This stores one subscription row per billing subscription.

### PaymentRecord

Key: payment ID string

Value fields:

- ID
- UserID
- SubscriptionID
- ProviderPaymentID
- Amount
- Currency
- Status
- ProviderEventID
- CreatedAt
- UpdatedAt

This stores one payment row per billing/payment event.

## Relationships

The three stores are related by business identity rather than by a single shared parent object:

- A user can have zero or more subscriptions.
- A user can have zero or more payments.
- A payment may optionally reference a subscription.
- Subscription and payment rows are discovered by user-scoped list methods using the UserID field.
- For bridge-style relationship stores, developers can call `StoreOptions.SetRelation(...)` to attach relation metadata and AI-oriented hints directly from the store configuration layer.

## ERD

```mermaid
erDiagram
    USER_RECORD {
        string username PK
        string role
        string password_hash
        string email
        string subscription_plan
        string[] subscription_features
        string billing_status
        string checkout_url
        string last_webhook_event
        datetime last_webhook_at
        string failure_reason
        string stripe_customer_id
        string stripe_subscription_id
        string status
        datetime created_at
        datetime updated_at
    }

    SUBSCRIPTION_RECORD {
        string id PK
        string user_id FK
        string plan_code
        string provider_subscription_id
        string status
        datetime current_period_start
        datetime current_period_end
        bool cancel_at_period_end
        datetime created_at
        datetime updated_at
    }

    PAYMENT_RECORD {
        string id PK
        string user_id FK
        string subscription_id FK
        string provider_payment_id
        float amount
        string currency
        string status
        string provider_event_id
        datetime created_at
        datetime updated_at
    }

    USER_RECORD ||--o{ SUBSCRIPTION_RECORD : has
    USER_RECORD ||--o{ PAYMENT_RECORD : has
    SUBSCRIPTION_RECORD ||--o{ PAYMENT_RECORD : may_reference
```

## Flow: Registration and Persistence

```mermaid
flowchart TD
    A[Client submits registration] --> B[siteaccount.Config.RegisterUserWithRequest]
    B --> C[Normalize username]
    C --> D[Create UserRecord]
    D --> E[Save to site_users B-tree]
    E --> F[Append in-memory config.Users]
    F --> G[Sync shared backend auth user]
    G --> H[Return user record]
```

## Flow: Checkout and Billing Update

```mermaid
flowchart TD
    A[User requests checkout] --> B[CreateCheckoutSession]
    B --> C[Load user from site_users]
    C --> D[Update billing state and checkout URL]
    D --> E[Persist updated UserRecord]
    E --> F[Return checkout session]

    G[Webhook arrives] --> H[HandleBillingWebhook]
    H --> I[Load user from site_users]
    I --> J[Update billing status and failure reason]
    J --> K[Persist updated UserRecord]
    K --> L[Optionally persist subscription/payment side effects]
```

## Notes for Review

- The model is intentionally simple: each record is persisted independently and looked up by a string key.
- The current implementation uses the user name as the primary lookup key for the user store and the record ID for subscription/payment stores.
- The relationships are currently enforced by application logic rather than by a relational database schema.
- The site account store is currently a key/value persistence layer, not a fully normalized relational model.
