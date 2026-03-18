# BlockAgents Roadmap

## Phase 1

- formal repository structure
- genesis document support
- canonical block storage
- transaction queue and receipts
- authenticated transaction envelopes and account nonces
- deterministic task execution
- worker / miner role assignment
- explicit debate-stage scheduling
- proof-of-thought artifact storage
- validator-set and quorum primitives
- peer messaging endpoints
- durable consensus certificate persistence
- durable round-change persistence and recovery
- durable consensus safety evidence
- replay-based certified block import
- candidate-block construction plus certified local commit
- follower candidate fetch plus replay validation before voting
- branch-aware forward sync across contiguous certified ranges
- timeout-driven round changes
- validator slashing execution from recorded evidence
- stronger proof schema with claim-root commitments and stage-specific claim validation
- proposal and evaluation state
- block-level consensus snapshot and settlement
- queryable blocks, transactions, tasks, and agents

## Phase 2

Near-term protocol hardening:

- explicit role-selection policy
- richer miner voting and debate-stage progression policies
- explicit error codes in receipts
- storage migration/versioning strategy
- stronger execution and integration test coverage
- authenticated agent bootstrap and key-rotation policy
- stronger semantic verification of proof-of-thought claims beyond structural commitments
- production-grade remote block synchronization and fork handling
- fork arbitration across deeper branches and canonical reorg policy

## Phase 3

Networking and validator work:

- peer discovery
- block propagation
- state sync
- validator set model
- proposer rotation or external consensus integration
- persistent fork-choice and certificate selection across reorgs

## Phase 4

Market and oracle maturity:

- external oracle adapters
- dispute windows
- richer task types
- stronger blockagents-style deliberation primitives
- slash accounting destinations
- treasury and governance policy

## Phase 5

Operator and ecosystem tooling:

- CLI for chain init and inspection
- metrics and tracing
- dockerized local devnet
- CI/CD for test and release
- SDK or client libraries for agent developers
