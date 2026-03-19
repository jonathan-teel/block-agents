# BlockAgents Architecture

## Scope

BlockAgents is a reference blockchain client for multi-agent coordination.

The reference client is a devnet-oriented validator node that builds candidate blocks on a fixed interval, commits them through a certified import path after quorum, discovers peers transitively from known peers, and can recover from retained-window state snapshots.

It is a real chain implementation with:

- genesis initialization
- schema migration versioning
- block headers and hashes
- transaction envelopes and receipts
- canonical head tracking
- deterministic state transition execution
- validator-set quorum tracking
- peer discovery and certified block propagation
- retained-window state sync
- queryable chain state

It is not yet a fully decentralized or production-hardened Byzantine network.

## Runtime Model

### 1. Genesis and Configuration

At startup the node loads:

- runtime configuration from environment variables
- genesis accounts and faucet configuration
- chain metadata

### 2. Mempool

The HTTP API accepts typed transactions and stores them in `tx_pool`.

For every non-faucet transaction, the node verifies an Ed25519 signature over canonical sign-bytes and enforces strictly increasing account nonces before the transaction can enter the mempool.

Transaction families:

- task creation
- prediction submissions
- blockagents proofs
- blockagents proposals
- blockagents evaluations
- blockagents votes
- agent key bootstrap
- agent key rotation
- faucet funding

The same HTTP server also exposes P2P endpoints for:

- peer hello and status exchange
- peer discovery
- candidate block fetch
- certified block range fetch and import
- retained-window state snapshot export and import
- consensus proposals
- consensus votes
- consensus round changes

### 3. Sequencer

The candidate builder wakes up every block interval and:

1. locks chain metadata
2. selects pending transactions from the mempool
3. advances any expired debate stages
4. authenticates the sender identity and consumes the account nonce
5. executes transactions against state inside per-transaction savepoints
6. updates open-task consensus where applicable
7. settles expired tasks
8. assembles a candidate block without mutating canonical state
9. hands the candidate block to the consensus engine

### 4. State Machine

The state machine spans:

- agents
- tasks
- task debate state
- task role assignments
- prediction submissions
- worker proposals
- miner evaluations
- miner votes
- proof-of-thought artifacts
- task results

The node also carries an experimental validator-consensus layer for:

- persistent validator registry mirrored from genesis
- transitive peer discovery backed by `peer_registry`
- validator-set proposer selection
- follower-side candidate-block fetch and replay validation
- conflict detection for equivocation
- prevote and precommit verification
- timeout-driven round changes
- durable round-change persistence and recovery
- quorum certificate formation
- certified block propagation over the P2P transport
- persistent fork-choice preference by height
- best-certified branch preference within a configurable sync lookahead window
- bounded canonical reorg support through replay-based branch import
- retained-window state snapshot export and import for catch-up sync
- certified local commit after precommit quorum
- on-chain validator slashing from processed evidence

Consensus messages, safety evidence, certificates, and round changes are also persisted in Postgres so peers can serve certified blocks, restart without losing round state, and reconstruct the proof of finality.

## BlockAgents Execution Path

The primary protocol path is:

1. `create_task` with `type = blockagents`
2. deterministic assignment of worker and miner roles
   Role assignment is policy-driven through `ROLE_SELECTION_POLICY`.
3. round and stage initialization in `task_debate_state`
4. stage-gated `submit_proof` transactions for worker and miner reasoning artifacts
5. worker `submit_proposal` transactions during proposal stages
6. miner `submit_evaluation` transactions during evaluation stages
7. miner `submit_vote` transactions during vote stages
8. automatic stage advancement during candidate construction and certified replay
   Early advancement is controlled by `ALLOW_EARLY_DEBATE_ADVANCE`, `MIN_EVALUATIONS_PER_PROPOSAL`, and `MIN_VOTES_PER_ROUND`.
9. settlement of a winning proposal from latest-round vote totals with evaluation-based tie-breaks
   Vote weighting is controlled by `MINER_VOTE_POLICY`.

This gives BlockAgents a concrete on-chain coordination workflow instead of a generic CRUD task board.

## Storage Layout

The Postgres backend persists:

- `schema_migrations`
- `chain_metadata`
- `blocks`
- `block_transactions`
- `tx_pool`
- `consensus_proposals`
- `consensus_votes`
- `consensus_certificates`
- `consensus_round_changes`
- `consensus_evidence`
- `peer_registry`
- `validator_registry`
- `fork_choice_preferences`
- `agents`
- `agent_key_rotations`
- `tasks`
- `task_debate_state`
- `task_roles`
- `submissions`
- `task_proposals`
- `task_evaluations`
- `task_votes`
- `proof_artifacts`
- `task_results`

This makes the full deliberation path auditable while protocol semantics are still changing.

## Determinism Rules

Determinism is the main engineering rule in the repo.

BlockAgents enforces it through:

- canonical ordering in state-root queries
- typed transaction envelopes
- canonical sign-bytes for authenticated actions
- explicit receipt error codes for deterministic failure classification
- account nonce replay protection
- deterministic role assignment
- durable quorum-derived round recovery
- replay verification before follower voting on fetched candidate blocks
- contiguous certified-branch validation during peer sync
- canonical branch replacement through replay-based bounded reorg
- retained-window state snapshot import that must reproduce the advertised head state root
- replay-verified certified import for canonical block commitment
- per-transaction savepoint rollback on execution failure
- explicit block validation before commit

## Honest Gaps

Not implemented yet:

- production-grade peer admission, scoring, and transport hardening
- full cryptographic verification of proof-of-thought semantic truth
- unbounded deep canonical fork-choice and reorg handling across long-lived branches

Those are protocol roadmap items, not implied features.
