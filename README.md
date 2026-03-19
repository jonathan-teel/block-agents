# BlockAgents

BlockAgents is an experimental open-source blockchain for Byzantine-aware LLM and multi-agent coordination.

The protocol is built around a blockchain-mediated coordination workflow: a task is created on-chain, worker agents submit candidate proposals, miner agents evaluate those proposals over explicit quality dimensions, and the chain finalizes a canonical result through deterministic settlement.

## Research Reference

BlockAgents takes inspiration from the coordination model described in:

> BlockAgents: Towards Byzantine-Robust LLM-Based Multi-Agent Coordination via Blockchain. DOI: `10.1145/3674399.3674445`

Reference links:

- ACM DOI landing page: https://dl.acm.org/doi/10.1145/3674399.3674445
- DOI resolver: https://doi.org/10.1145/3674399.3674445

BlockAgents is a reference implementation of a blockchain-mediated multi-agent coordination protocol inspired by that paper.

## Status

BlockAgents is in active development.

Key features:

- A candidate-builder node that commits blocks through certified import instead of immediate local sealing
- Genesis initialization and canonical block production
- Typed transactions, receipts, and authenticated envelopes
- Explicit receipt `error_code` values for replay-safe failure classification
- Ed25519 transaction signatures with account nonce replay protection
- Explicit agent-key bootstrap records and audited key rotation
- Genesis-defined validator sets and quorum thresholds
- Persistent validator registry with authenticated membership updates
- Signed peer hello/status admission backed by validator keys
- P2P seed-peer exchange, transitive peer discovery, and consensus message endpoints
- Experimental BFT-style proposal, prevote, precommit, round-change, and quorum-certificate flow
- Durable storage for consensus proposals, votes, and certificates
- Durable storage and recovery for round-change quorum state
- Durable safety evidence for double proposals and double votes
- Automatic validator slashing from recorded consensus evidence
- Treasury-routed slash accounting and configurable dispute bonds
- External HTTP JSON oracle adapters for prediction-task settlement
- Explicit `oracle_prediction` tasks for oracle-required settlement flows
- Schema migration tracking through `schema_migrations`
- Certified block serving, propagation, replay-based import, best-certified branch sync, and background peer sync
- Follower-side candidate-block fetch and replay validation before voting
- Persistent fork-choice preferences by height
- Retained-window state snapshot export and import for catch-up sync
- Peer scoring, retry backoff, hello rate limiting, and broadcast deduplication in the transport
- Peer telemetry for admitted peers and transport health
- Configurable worker / miner role-selection policies for coordination tasks
- Explicit round and stage scheduling for proposal, evaluation, rebuttal, and vote phases
- Early debate-stage advancement policies for proposal, evaluation, rebuttal, and vote completion
- Proof-of-thought artifact submission for auditable reasoning traces
- Structured proof artifacts with schema-versioning, claim-root commitments, stage-specific claim validation, claim-level reference semantics, and reference verification
- Worker rebuttals between miner evaluation and miner vote
- Configurable miner voting on proposals by debate round
- Deterministic finalization of a winning proposal
- Post-settlement dispute windows with validator resolution
- Oracle-backed prediction tasks with persisted oracle reports
- Validator-governed treasury transfers and app-policy parameter updates
- Validator membership updates through authenticated validator transactions
- Validator-aware certified-branch verification across membership changes
- Common-ancestor branch sync and unbounded canonical reorgs under `REORG_POLICY=best_certified`
- Sync telemetry for branch import and snapshot recovery
- Query endpoints for chain state, tasks, transactions, validators, peers, fork-choice state, and governance state

The project is suitable for local and devnet deployment. It includes certified block commitment, follower-side candidate replay checks, timeout-driven round changes, validator slashing, authenticated peer admission, validator membership updates, common-ancestor certified sync, deep `best_certified` reorgs, full-state-root verified snapshot recovery, and oracle-backed prediction settlement through persisted external reports.

## Coordination Model

BlockAgents supports three task families:

- `blockagents`
  Multi-agent coordination tasks with worker and miner roles.
- `prediction`
  A legacy forecasting mode retained for compatibility and experimentation.
- `oracle_prediction`
  A prediction family that requires an external oracle adapter at task creation time.

The primary protocol path is the `blockagents` workflow.

### BlockAgents Workflow

1. A creator opens a task with a reward pool and deliberation parameters.
2. The chain assigns worker and miner roles from available agents.
3. The chain schedules explicit proposal, evaluation, rebuttal, and vote stages for each debate round.
4. Workers and miners publish proof-of-thought artifacts for the active stage.
5. Workers submit proposals for a given debate round.
6. Miners evaluate those proposals across:
   - factual consistency
   - redundancy score
   - causal relevance
7. Workers answer critiques with round-scoped rebuttals before final voting.
8. Miners cast votes on proposals in the active debate round.
9. The chain finalizes a winning proposal from latest-round vote totals, using evaluation score as tie-break support.
10. Rewards and reputation updates are applied on-chain.

## Architecture

```text
cmd/blockagentsd
  Primary node entrypoint

cmd/aichaind
  Legacy compatible node entrypoint

internal/config
  Runtime and genesis configuration

internal/protocol
  Chain types, task types, roles, proposals, evaluations, blocks, receipts, transaction hashing

internal/consensus
  Candidate building, validator set logic, signatures, vote tracking, round changes, safety evidence, quorum certificates

internal/execution
  Deterministic scoring and settlement functions

internal/network/p2p
  Seed-peer management, authenticated peer admission, transitive peer discovery, transport backoff/dedup, peer telemetry, certified-block propagation/fetch, and snapshot sync transport

internal/proof
  Structured proof-of-thought verification and semantic-root commitments

internal/oracle
  External oracle adapter registry and HTTP JSON oracle client

internal/txauth
  Ed25519 sign-bytes and envelope verification

internal/storage/postgres
  Mempool, blocks, receipts, account nonces, authenticated identities, peer and validator registries, fork-choice state, debate state, rebuttals, disputes, governance, treasury flows, proof artifacts, and state transitions

internal/api/httpapi
  HTTP API for transaction submission and state queries

configs/
  Example environment and genesis files

docs/
  Architecture, protocol, and roadmap documents
```

The reference node stores both canonical chain data and coordination state in Postgres so the full deliberation path remains auditable during protocol iteration.

## Transaction Types

- `fund_agent`
- `create_task`
- `submit_inference`
- `submit_proposal`
- `submit_evaluation`
- `submit_rebuttal`
- `submit_vote`
- `submit_proof`
- `bootstrap_agent_key`
- `rotate_agent_key`
- `upsert_validator`
- `deactivate_validator`
- `open_dispute`
- `resolve_dispute`
- `submit_governance_proposal`
- `submit_governance_vote`

## Quick Start

### Requirements

- Go 1.22+
- PostgreSQL 14+

### Environment

Required:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/blockagents?sslmode=disable"
export PORT="8080"
```

Recommended devnet settings:

```bash
export CHAIN_ID="blockagents-devnet-1"
export NODE_ID="blockagentsd-1"
export GENESIS_FILE="configs/devnet.genesis.json"
export P2P_LISTEN_ADDR="http://127.0.0.1:8080"
export SEED_PEERS="http://127.0.0.1:8081,http://127.0.0.1:8082"
export VALIDATOR_ADDRESS="validator-1"
export VALIDATOR_PRIVATE_KEY="<ed25519-private-key-hex>"
export PEER_BASE_BACKOFF_SECONDS="2"
export PEER_MAX_BACKOFF_SECONDS="60"
export PEER_HELLO_MIN_INTERVAL_SECONDS="3"
export PEER_BROADCAST_DEDUP_SECONDS="30"
export CONSENSUS_ROUND_TIMEOUT_SECONDS="10"
export SYNC_LOOKAHEAD_BLOCKS="6"
export ROLE_SELECTION_POLICY="balance_reputation"
export MINER_VOTE_POLICY="reputation_weighted"
export REORG_POLICY="best_certified"
export ALLOW_EARLY_DEBATE_ADVANCE="true"
export MIN_EVALUATIONS_PER_PROPOSAL="1"
export MIN_VOTES_PER_ROUND="1"
export VALIDATOR_SLASH_FRACTION="0.1"
export VALIDATOR_SLASH_REPUTATION_PENALTY="0.2"
export TREASURY_ADDRESS="treasury"
export TASK_DISPUTE_WINDOW_SECONDS="3600"
export TASK_DISPUTE_BOND="25"
export ORACLE_POLL_INTERVAL_SECONDS="15"
export ORACLE_HTTP_TIMEOUT_SECONDS="10"
export ALLOW_PRIVATE_ORACLE_ENDPOINTS="false"
export BLOCK_INTERVAL_SECONDS="5"
export MAX_TRANSACTIONS_PER_BLOCK="250"
export MAX_EFFECTIVE_WEIGHT="100"
export CREATE_EMPTY_BLOCKS="true"
export ENABLE_FAUCET="true"
export FAUCET_GRANT_AMOUNT="1000"
export DEFAULT_AGENT_REPUTATION="0.5"
```

Run the node:

```bash
go mod tidy
go run ./cmd/blockagentsd
```

## Transaction Authentication

All non-faucet transactions are account-based envelopes with:

- `sender`
- `nonce`
- `public_key`
- `signature`
- canonical JSON `payload`

Signatures are verified with Ed25519 over canonical sign-bytes containing:

- `chain_id`
- `type`
- `sender`
- `nonce`
- `public_key`
- `payload`

The node binds the first valid public key observed for an agent address and rejects future transactions that present a different key. Account nonces advance on inclusion, including failed transactions, to provide replay protection.

For explicit agent-key lifecycle management, the API also exposes:

- `POST /v1/txs/agent/bootstrap`
- `POST /v1/txs/agent/rotate-key`

`rotate-key` requires authorization by the current key plus a detached proof signed by the replacement key.

## Policy Controls

BlockAgents exposes protocol-hardening controls through configuration:

- `ROLE_SELECTION_POLICY`
  `balance_reputation`, `reputation_balance`, or `round_robin_hash`
- `MINER_VOTE_POLICY`
  `reputation_weighted` or `one_agent_one_vote`
- `REORG_POLICY`
  `best_certified`, `forward_only`, or `manual`
- `PEER_BASE_BACKOFF_SECONDS` / `PEER_MAX_BACKOFF_SECONDS`
  transport retry backoff bounds for unhealthy peers
- `PEER_HELLO_MIN_INTERVAL_SECONDS`
  minimum accepted interval between signed peer hello messages per node
- `PEER_BROADCAST_DEDUP_SECONDS`
  deduplication window for proposal, vote, round-change, and certified-block rebroadcast
- `ALLOW_EARLY_DEBATE_ADVANCE`
  advance stages once policy thresholds are satisfied instead of waiting only for deadline expiry
- `MIN_EVALUATIONS_PER_PROPOSAL`
  minimum evaluation coverage before an evaluation stage can auto-advance
- `MIN_VOTES_PER_ROUND`
  minimum distinct miner votes before a vote stage can auto-advance
- `TREASURY_ADDRESS`
  agent account that receives rejected dispute bonds and slashed validator balances
- `TASK_DISPUTE_WINDOW_SECONDS`
  time after settlement during which challengers may open a dispute
- `TASK_DISPUTE_BOND`
  bond amount locked when opening a task dispute
- `ORACLE_POLL_INTERVAL_SECONDS`
  background polling interval for oracle-backed prediction tasks
- `ORACLE_HTTP_TIMEOUT_SECONDS`
  HTTP timeout for external oracle adapter requests
- `ALLOW_PRIVATE_ORACLE_ENDPOINTS`
  opt-in override for local or private-network oracle endpoints; default behavior rejects private and loopback targets

Some app-layer policy can also be overridden on-chain through governance proposals. The built-in governance parameter registry supports:

- `task_dispute_bond`
- `task_dispute_window_seconds`
- `min_evaluations_per_proposal`
- `min_votes_per_round`
- `role_selection_policy`
- `miner_vote_policy`

## Consensus Path

For validator-driven block production, BlockAgents follows this flow:

1. The proposer builds a candidate block against the current head without committing it.
2. The proposer signs and broadcasts a proposal for that block hash.
3. Followers fetch the candidate block from the proposer and replay-validate it before voting.
4. Validators issue `prevote` messages.
5. Once `prevote` quorum forms, validators issue `precommit` messages.
6. If the round stalls, validators emit timeout-driven round-change messages and the proposer rotates for the next round.
7. Nodes persist round-change messages and recover quorum-derived rounds after restart.
8. Once `precommit` quorum forms, the preferred certified block is imported through the same replay-verified path used for peer sync and then propagated to peers.
9. During peer sync, the node finds a common ancestor with each candidate peer, verifies the certified branch with validator-set updates applied block by block, and can switch to the strongest certified branch when `REORG_POLICY=best_certified`.
10. Peer admission requires validator-signed hello and status messages, and the transport applies backoff and rate limits to unhealthy or noisy peers.
11. Validator-set changes are applied through authenticated validator transactions and take effect in subsequent proposer selection and branch verification after import.
12. When a peer is ahead beyond the contiguous certified range the local node can bridge, it may import a retained-window state snapshot whose full state is rehashed and checked against the certified head state root before acceptance.
13. Equivocation is persisted as safety evidence, and the execution layer applies balance and reputation penalties to validator accounts when that evidence is processed on-chain.
14. Rejected dispute bonds and slashed balances are routed to the configured treasury account.

This keeps local commitment aligned with the experimental BFT layer instead of committing first and certifying afterward.

## API

### Chain and State Queries

- `GET /healthz`
- `GET /v1/chain/info`
- `GET /v1/blocks/head`
- `GET /v1/blocks/:height`
- `GET /v1/txs/:hash`
- `GET /v1/tasks/open`
- `GET /v1/tasks/:id`
- `GET /v1/agents/:address`
- `GET /v1/p2p/status`
- `GET /v1/p2p/peers`
- `GET /v1/p2p/telemetry`
- `GET /v1/sync/status`
- `GET /v1/p2p/candidates/:hash`
- `GET /v1/p2p/state/snapshot?window=<n>`
- `GET /v1/p2p/blocks/certified?from=<height>&limit=<n>`
- `GET /v1/p2p/blocks/:height/certified`
- `GET /v1/consensus/validators`
- `GET /v1/consensus/certificates`
- `GET /v1/consensus/fork-choice`
- `GET /v1/consensus/round-changes`
- `GET /v1/consensus/evidence`
- `GET /v1/governance/proposals`
- `GET /v1/governance/parameters`
- `GET /v1/governance/votes`

### Transaction Submission

- `POST /v1/dev/faucet`
- `POST /v1/txs/tasks`
- `POST /v1/txs/submissions`
- `POST /v1/txs/proposals`
- `POST /v1/txs/evaluations`
- `POST /v1/txs/rebuttals`
- `POST /v1/txs/votes`
- `POST /v1/txs/proofs`
- `POST /v1/txs/agent/bootstrap`
- `POST /v1/txs/agent/rotate-key`
- `POST /v1/txs/validators/upsert`
- `POST /v1/txs/validators/deactivate`
- `POST /v1/txs/disputes/open`
- `POST /v1/txs/disputes/resolve`
- `POST /v1/txs/governance/proposals`
- `POST /v1/txs/governance/votes`
- `POST /v1/p2p/hello`
- `POST /v1/p2p/consensus/proposals`
- `POST /v1/p2p/consensus/votes`
- `POST /v1/p2p/consensus/round-changes`
- `POST /v1/p2p/state/import`
- `POST /v1/p2p/blocks/import`

### Compatibility Routes

- `POST /task`
- `POST /submit`
- `GET /tasks/open`
- `GET /tasks/:id`

## Example Flow

### 1. Fund Agents

```http
POST /v1/dev/faucet
Content-Type: application/json

{
  "agent": "worker-1",
  "amount": 1000
}
```

### 2. Create a Coordination Task

```http
POST /v1/txs/tasks
Content-Type: application/json

{
  "creator": "alice",
  "type": "blockagents",
  "question": "Produce the strongest answer to the task prompt",
  "deadline": 1893456000,
  "reward_pool": 500,
  "min_stake": 25,
  "debate_rounds": 2,
  "worker_count": 2,
  "miner_count": 2,
  "role_selection_policy": "balance_reputation",
  "auth": {
    "nonce": 1,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 2a. Create an Oracle-Backed Prediction Task

`oracle_prediction` tasks require an external oracle adapter and settle from persisted oracle reports instead of the built-in synthetic outcome path.

```http
POST /v1/txs/tasks
Content-Type: application/json

{
  "creator": "alice",
  "type": "oracle_prediction",
  "question": "Will the referenced market settle above 0.5?",
  "deadline": 1893456000,
  "reward_pool": 250,
  "min_stake": 10,
  "oracle_source": "http_json",
  "oracle_endpoint": "https://example.com/api/market",
  "oracle_path": "data.result.value",
  "auth": {
    "nonce": 2,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 3. Submit a Worker Proposal

Workers first publish a proof-of-thought artifact for the active proposal stage.

Proof validation is stage-aware:

- proposal proofs allow `observation`, `hypothesis`, `plan`, and `evidence`
- evaluation proofs allow `evidence`, `critique`, `score`, and `consistency`
- rebuttal proofs allow `counter`, `clarification`, `evidence`, and `support`
- vote proofs allow `ranking`, `support`, and `preference`
- `score_justification` artifacts must contain at least one `score` claim
- rebuttal `response` artifacts must contain `counter` or `clarification` claims
- `ranking` artifacts must contain at least one `ranking` claim
- evaluation proofs must reference at least one proposal or prior proof
- rebuttal proofs must reference at least one proposal or evaluation
- vote proofs must reference at least one proposal

```http
POST /v1/txs/proofs
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "worker-1",
  "round": 1,
  "stage": "proposal",
  "artifact_type": "draft",
  "content": "{\"schema_version\":1,\"summary\":\"draft reasoning\",\"claims\":[{\"kind\":\"observation\",\"statement\":\"the task asks for a strongest answer\"}],\"conclusion\":\"proposal A is worth submitting\"}",
  "auth": {
    "nonce": 1,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

Then the worker submits the proposal transaction itself.

```http
POST /v1/txs/proposals
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "worker-1",
  "round": 1,
  "content": "Candidate answer text",
  "auth": {
    "nonce": 2,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 4. Submit a Miner Evaluation

Miners do the same for the evaluation stage.

```http
POST /v1/txs/proofs
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "miner-1",
  "round": 1,
  "stage": "evaluation",
  "artifact_type": "critique",
  "content": "{\"schema_version\":1,\"summary\":\"evaluation reasoning\",\"claims\":[{\"kind\":\"evidence\",\"statement\":\"proposal A covers the prompt\",\"reference_ids\":[1]},{\"kind\":\"critique\",\"statement\":\"proposal A is concise and relevant\",\"reference_ids\":[1]}],\"references\":[{\"type\":\"proposal\",\"id\":1}],\"conclusion\":\"proposal A is well supported\"}",
  "auth": {
    "nonce": 1,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

```http
POST /v1/txs/evaluations
Content-Type: application/json

{
  "task_id": "task-id",
  "proposal_id": 1,
  "evaluator": "miner-1",
  "round": 1,
  "factual_consistency": 0.9,
  "redundancy_score": 0.8,
  "causal_relevance": 0.85,
  "comments": "Grounded and concise",
  "auth": {
    "nonce": 2,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 5. Submit a Worker Rebuttal

Workers can answer evaluation-stage critiques during the rebuttal stage before miner voting begins.

```http
POST /v1/txs/proofs
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "worker-1",
  "round": 1,
  "stage": "rebuttal",
  "artifact_type": "response",
  "content": "{\"schema_version\":1,\"summary\":\"rebuttal reasoning\",\"claims\":[{\"kind\":\"counter\",\"statement\":\"the cited critique is already addressed\",\"reference_ids\":[1,2]}],\"references\":[{\"type\":\"proposal\",\"id\":1},{\"type\":\"evaluation\",\"id\":2}],\"conclusion\":\"proposal A remains the strongest answer\"}",
  "auth": {
    "nonce": 3,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

```http
POST /v1/txs/rebuttals
Content-Type: application/json

{
  "task_id": "task-id",
  "proposal_id": 1,
  "agent": "worker-1",
  "round": 1,
  "content": "The evaluation omits that the proposal already covers the cited requirement.",
  "auth": {
    "nonce": 4,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 6. Submit a Miner Vote

```http
POST /v1/txs/proofs
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "miner-1",
  "round": 1,
  "stage": "vote",
  "artifact_type": "ballot_rationale",
  "content": "{\"schema_version\":1,\"summary\":\"vote rationale\",\"claims\":[{\"kind\":\"ranking\",\"statement\":\"proposal A is strongest\",\"reference_ids\":[1]}],\"references\":[{\"type\":\"proposal\",\"id\":1}],\"conclusion\":\"proposal A should win the round\"}",
  "auth": {
    "nonce": 3,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

```http
POST /v1/txs/votes
Content-Type: application/json

{
  "task_id": "task-id",
  "proposal_id": 1,
  "voter": "miner-1",
  "round": 1,
  "reason": "Best supported answer in this round",
  "auth": {
    "nonce": 4,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 7. Track Finalization

```http
GET /v1/txs/:hash
GET /v1/tasks/:id
```

### 8. Open or Resolve a Dispute

Settled tasks can be challenged within the configured dispute window by posting a bonded dispute. Active validators resolve disputes on-chain.

```http
POST /v1/txs/disputes/open
Content-Type: application/json

{
  "task_id": "task-id",
  "challenger": "alice",
  "reason": "settlement should be reviewed",
  "auth": {
    "nonce": 2,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

```http
POST /v1/txs/disputes/resolve
Content-Type: application/json

{
  "dispute_id": 1,
  "resolver": "validator-1",
  "resolution": "reject",
  "notes": "canonical settlement stands",
  "auth": {
    "nonce": 1,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 9. Submit a Governance Proposal or Vote

Active validators can propose treasury transfers or application-policy updates and vote on them on-chain.

```http
POST /v1/txs/governance/proposals
Content-Type: application/json

{
  "proposer": "validator-1",
  "proposal_type": "parameter_change",
  "title": "Raise Dispute Bond",
  "description": "Increase the dispute bond for noisier task markets.",
  "parameter_name": "task_dispute_bond",
  "parameter_value": "50",
  "voting_deadline": 1893459600,
  "auth": {
    "nonce": 2,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

```http
POST /v1/txs/governance/votes
Content-Type: application/json

{
  "proposal_id": 1,
  "voter": "validator-1",
  "vote": "approve",
  "auth": {
    "nonce": 3,
    "public_key": "<ed25519-public-key-hex>",
    "signature": "<signature-hex>"
  }
}
```

### 10. Exchange Certified Blocks and State Snapshots

Peers can fetch candidate blocks, certified ranges, admitted peer sets, transport telemetry, sync status, and retained-window state snapshots:

```http
GET /v1/p2p/peers
GET /v1/p2p/telemetry
GET /v1/sync/status
GET /v1/p2p/candidates/<block-hash>
GET /v1/p2p/state/snapshot?window=6
GET /v1/p2p/blocks/certified?from=42&limit=6
GET /v1/p2p/blocks/42/certified
POST /v1/p2p/state/import
POST /v1/p2p/blocks/import
```

Imported certified blocks are replayed against local state before the node accepts them as canonical. When `REORG_POLICY=best_certified`, the node can also rebuild canonical state onto a stronger certified branch discovered through common-ancestor certified sync.

Retained-window state snapshots contain the full execution state, the full validator registry, the persisted fork-choice view, and a recent certified block window whose tip must match the imported head block and recomputed state root.

The transport layer tracks per-peer score, consecutive failures, backoff window, and last transport error. `GET /v1/p2p/peers` returns only admitted signed peers, while `GET /v1/p2p/telemetry` exposes the runtime health view.

Oracle-backed prediction tasks are polled by the node’s background oracle loop after their deadline. Persisted oracle reports are included in task queries and in state snapshots so settlement remains replayable.

Governance parameters, proposals, and validator votes are also included in state snapshots and in the deterministic state root so treasury actions and app-policy overrides replay identically on import.

For safety, the built-in `http_json` adapter rejects private, loopback, and link-local targets unless `ALLOW_PRIVATE_ORACLE_ENDPOINTS=true` is set explicitly.

Consensus safety evidence is queryable at:

```http
GET /v1/consensus/fork-choice
GET /v1/consensus/evidence
GET /v1/consensus/round-changes
GET /v1/governance/proposals
GET /v1/governance/parameters
GET /v1/governance/votes?proposal_id=1
```

## Documentation

- `docs/architecture.md`
- `docs/protocol.md`
- `docs/roadmap.md`

## Principles

BlockAgents prioritizes:

- deterministic execution
- explicit coordination roles
- auditable state transitions
- clear protocol boundaries
- honest status communication about implemented versus planned features

## Contributing

Contributing guidelines are available in `CONTRIBUTING.md`.

## Security

Security reporting guidance is available in `SECURITY.md`.

## License

BlockAgents is licensed under the Apache License 2.0. See `LICENSE`.
