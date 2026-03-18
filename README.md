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
- Ed25519 transaction signatures with account nonce replay protection
- Genesis-defined validator sets and quorum thresholds
- P2P seed-peer exchange, certified-block sync, and consensus message endpoints
- Experimental BFT-style proposal, prevote, precommit, round-change, and quorum-certificate flow
- Durable storage for consensus proposals, votes, and certificates
- Durable storage and recovery for round-change quorum state
- Durable safety evidence for double proposals and double votes
- Automatic validator slashing from recorded consensus evidence
- Certified block serving, replay-based import, and background peer sync
- Follower-side candidate-block fetch and replay validation before voting
- Next-height fork-choice preference when competing certified blocks are observed
- Branch-aware forward sync over contiguous certified block ranges
- Worker / miner role assignment for coordination tasks
- Explicit round and stage scheduling for proposal, evaluation, and vote phases
- Proof-of-thought artifact submission for auditable reasoning traces
- Structured proof artifacts with schema-versioning, claim-root commitments, stage-specific claim validation, and reference verification
- Miner voting on proposals by debate round
- Deterministic finalization of a winning proposal
- Query endpoints for chain state, tasks, transactions, and agents

The project is suitable for local and devnet deployment. It includes certified block commitment, follower-side candidate replay checks, timeout-driven round changes, and validator slashing. It is not yet a production-hardened Byzantine network, and the faucet remains a development bootstrap path.

## Coordination Model

BlockAgents supports two task families:

- `blockagents`
  Multi-agent coordination tasks with worker and miner roles.
- `prediction`
  A legacy forecasting mode retained for compatibility and experimentation.

The primary protocol path is the `blockagents` workflow.

### BlockAgents Workflow

1. A creator opens a task with a reward pool and deliberation parameters.
2. The chain assigns worker and miner roles from available agents.
3. The chain schedules explicit proposal, evaluation, and vote stages for each debate round.
4. Workers and miners publish proof-of-thought artifacts for the active stage.
5. Workers submit proposals for a given debate round.
6. Miners evaluate those proposals across:
   - factual consistency
   - redundancy score
   - causal relevance
7. Miners cast votes on proposals in the active debate round.
8. The chain finalizes a winning proposal from latest-round vote totals, using evaluation score as tie-break support.
9. Rewards and reputation updates are applied on-chain.

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
  Seed-peer management, status exchange, certified-block fetch, and consensus message broadcast

internal/proof
  Structured proof-of-thought verification and semantic-root commitments

internal/txauth
  Ed25519 sign-bytes and envelope verification

internal/storage/postgres
  Mempool, blocks, receipts, account nonces, authenticated identities, debate state, proof artifacts, and state transitions

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
- `submit_vote`
- `submit_proof`

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
export CONSENSUS_ROUND_TIMEOUT_SECONDS="10"
export SYNC_LOOKAHEAD_BLOCKS="6"
export VALIDATOR_SLASH_FRACTION="0.1"
export VALIDATOR_SLASH_REPUTATION_PENALTY="0.2"
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

## Consensus Path

For validator-driven block production, BlockAgents follows this flow:

1. The proposer builds a candidate block against the current head without committing it.
2. The proposer signs and broadcasts a proposal for that block hash.
3. Followers fetch the candidate block from the proposer and replay-validate it before voting.
4. Validators issue `prevote` messages.
5. Once `prevote` quorum forms, validators issue `precommit` messages.
6. If the round stalls, validators emit timeout-driven round-change messages and the proposer rotates for the next round.
7. Nodes persist round-change messages and recover quorum-derived rounds after restart.
8. Once `precommit` quorum forms, the preferred certified block is imported through the same replay-verified path used for peer sync.
9. During peer sync, the node prefers the strongest contiguous certified branch returned by peers over a configurable lookahead window.
10. Equivocation is persisted as safety evidence, and the execution layer applies balance and reputation penalties to validator accounts when that evidence is processed on-chain.

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
- `GET /v1/p2p/candidates/:hash`
- `GET /v1/p2p/blocks/certified?from=<height>&limit=<n>`
- `GET /v1/p2p/blocks/:height/certified`
- `GET /v1/consensus/validators`
- `GET /v1/consensus/certificates`
- `GET /v1/consensus/round-changes`
- `GET /v1/consensus/evidence`

### Transaction Submission

- `POST /v1/dev/faucet`
- `POST /v1/txs/tasks`
- `POST /v1/txs/submissions`
- `POST /v1/txs/proposals`
- `POST /v1/txs/evaluations`
- `POST /v1/txs/votes`
- `POST /v1/txs/proofs`
- `POST /v1/p2p/hello`
- `POST /v1/p2p/consensus/proposals`
- `POST /v1/p2p/consensus/votes`
- `POST /v1/p2p/consensus/round-changes`
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
  "auth": {
    "nonce": 1,
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
- vote proofs allow `ranking`, `support`, and `preference`
- evaluation proofs must reference at least one proposal or prior proof
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
  "content": "{\"schema_version\":1,\"summary\":\"evaluation reasoning\",\"claims\":[{\"kind\":\"evidence\",\"statement\":\"proposal A covers the prompt\"}],\"references\":[{\"type\":\"proposal\",\"id\":1}],\"conclusion\":\"proposal A is well supported\"}",
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

### 5. Submit a Miner Vote

```http
POST /v1/txs/proofs
Content-Type: application/json

{
  "task_id": "task-id",
  "agent": "miner-1",
  "round": 1,
  "stage": "vote",
  "artifact_type": "ballot_rationale",
  "content": "{\"schema_version\":1,\"summary\":\"vote rationale\",\"claims\":[{\"kind\":\"ranking\",\"statement\":\"proposal A is strongest\"}],\"references\":[{\"type\":\"proposal\",\"id\":1}],\"conclusion\":\"proposal A should win the round\"}",
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

### 6. Track Finalization

```http
GET /v1/txs/:hash
GET /v1/tasks/:id
```

### 7. Exchange Certified Blocks

Peers can fetch candidate and certified blocks:

```http
GET /v1/p2p/candidates/<block-hash>
GET /v1/p2p/blocks/certified?from=42&limit=6
GET /v1/p2p/blocks/42/certified
POST /v1/p2p/blocks/import
```

Imported certified blocks are replayed against local state before the node accepts them as canonical.

Consensus safety evidence is queryable at:

```http
GET /v1/consensus/evidence
GET /v1/consensus/round-changes
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
