# Cache TTL assessment for B-tree, registry, and value-data caches

This note captures the current TTL policy assessment for the main cache domains used by the SOP storage engine and the reasoning behind the recommended relative ordering.

## Current defaults

The default cache policy in [lib/sop/storeoptions.go](../lib/sop/storeoptions.go) currently uses:

- B-tree nodes: 5 minutes
- Registry entries/handles: 15 minutes
- Item values (globally cached): 10 minutes
- Store info: 10 minutes

The enforcement and normalization logic lives in [lib/sop/storeinfo.go](../lib/sop/storeinfo.go).

## Why the ordering matters

The cache classes represent different kinds of state:

- B-tree nodes are structural and mutable. They are part of the live tree shape and are used by merge/conflict logic.
- Registry handles are coordination metadata. They are important for resolving active/inactive identities and keeping transaction semantics aligned.
- Item values are payload data. They are expensive to read and serialize, but they are usually less central to correctness than registry state.

Because of that, the cache should be tuned so that:

1. Node cache is the most conservative.
2. Registry cache is the warmest.
3. Value-data cache sits in the middle.

## Assessment by data class

### B-tree nodes

Nodes are the most volatile cache class because they are directly affected by inserts, updates, splits, rebalances, and merges. They also participate in the core tree structure, so keeping them warm for too long can increase the chance of stale structural assumptions during a transaction.

Recommendation:

- Keep node cache relatively short.
- 5 minutes is a good default for mixed workloads.
- For very write-heavy or high-churn workloads, consider shortening further or making the node cache opt-in for some stores.

Relevant code paths:

- [lib/sop/common/noderepository.backend.go](../lib/sop/common/noderepository.backend.go)
- [lib/sop/common/twophasecommittransaction.go](../lib/sop/common/twophasecommittransaction.go)

### Registry handles and registry entries

Registry data is the most correctness-sensitive cache. It is used to resolve handles and supports core engine flows such as conflict detection and merge orchestration. These entries should remain available long enough to support multi-step transaction behavior and commit reconciliation.

Recommendation:

- Keep registry cache at the longest window among the core object caches.
- 10 to 15 minutes is appropriate for most deployments.
- Avoid making this cache too aggressive, because it can weaken the correctness and recovery behavior of transaction paths.

Relevant code paths:

- [lib/sop/common/noderepository.backend.go](../lib/sop/common/noderepository.backend.go)
- [lib/sop/common/managebtree.go](../lib/sop/common/managebtree.go)

### Item values

Item values are different from nodes and registry entries because they can be persisted independently of the B-tree node segment. When values are stored in a separate value-data segment or blob store, the value cache is mostly a performance optimization rather than a correctness anchor.

Recommendation:

- Keep values cached longer than nodes, but shorter than registry metadata.
- 5 to 10 minutes is a sensible default for most stores.
- For very large or frequently updated values, prefer a shorter TTL or disable global value caching for the store.
- For stores where values are small and read-heavy, a slightly longer window can be acceptable.

Relevant code paths:

- [lib/sop/common/itemactiontracker.go](../lib/sop/common/itemactiontracker.go)
- [lib/sop/common/itemactiontracker.valuedata.go](../lib/sop/common/itemactiontracker.valuedata.go)

## Recommended policy

A practical default policy is:

- Node cache: 5 minutes
- Registry cache: 10 to 15 minutes
- Value-data cache: 5 to 10 minutes
- Store-info cache: 10 minutes

This ordering is consistent with the system design:

- nodes are the most mutable,
- registry data is the most coordination-critical,
- values are expensive but less central than registry state.

## Operational guidance

- For write-heavy workloads, keep node and value TTLs shorter.
- For read-heavy, mostly static stores, slightly longer value TTLs can be acceptable.
- For stores with separate value-data persistence, treat value caching as a performance feature rather than a durability guarantee.
- For standalone deployments, the in-process cache policy is already more conservative, so the TTL policy should remain moderate rather than overly warm.
