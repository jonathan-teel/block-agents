# BlockAgents Architecture

## Scope

BlockAgents is a reference blockchain client for multi-agent coordination.

The reference client is a devnet-oriented validator node that builds candidate blocks on a fixed interval and commits them through a certified import path after quorum.

It is a real chain implementation with:

- genesis initialization
- block headers and hashes
- transaction envelopes and receipts
- canonical head tracking
- deterministic state transition execution
- validator-set quorum tracking
- peer-exchange endpoints for consensus traffic
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
- faucet funding

The same HTTP server also exposes P2P endpoints for:

- peer hello and status exchange
- candidate block fetch
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

- validator-set proposer selection
- follower-side candidate-block fetch and replay validation
- conflict detection for equivocation
- prevote and precommit verification
- timeout-driven round changes
- durable round-change persistence and recovery
- quorum certificate formation
- next-height fork-choice preference among competing certificates
- branch-aware forward sync across contiguous certified block ranges
- certified local commit after precommit quorum
- on-chain validator slashing from processed evidence

Consensus messages, safety evidence, certificates, and round changes are also persisted in Postgres so peers can serve certified blocks, restart without losing round state, and reconstruct the proof of finality.

## BlockAgents Execution Path

The primary protocol path is:

1. `create_task` with `type = blockagents`
2. deterministic assignment of worker and miner roles
3. round and stage initialization in `task_debate_state`
4. stage-gated `submit_proof` transactions for worker and miner reasoning artifacts
5. worker `submit_proposal` transactions during proposal stages
6. miner `submit_evaluation` transactions during evaluation stages
7. miner `submit_vote` transactions during vote stages
8. automatic stage advancement during candidate construction and certified replay
9. settlement of a winning proposal from latest-round vote totals with evaluation-based tie-breaks

This gives BlockAgents a concrete on-chain coordination workflow instead of a generic CRUD task board.

## Storage Layout

The Postgres backend persists:

- `chain_metadata`
- `blocks`
- `block_transactions`
- `tx_pool`
- `consensus_proposals`
- `consensus_votes`
- `consensus_certificates`
- `consensus_round_changes`
- `consensus_evidence`
- `agents`
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
- account nonce replay protection
- deterministic role assignment
- durable quorum-derived round recovery
- replay verification before follower voting on fetched candidate blocks
- contiguous certified-branch validation during peer sync
- replay-verified certified import for canonical block commitment
- per-transaction savepoint rollback on execution failure
- explicit block validation before commit

## Honest Gaps

Not implemented yet:

- production-grade p2p membership and transport hardening
- full cryptographic verification of proof-of-thought semantic truth
- deep reorg-capable fork-choice and multi-branch canonical switching across already committed heights

Those are protocol roadmap items, not implied features.
