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

The current reference client is suitable for local and devnet deployment. It includes:

- genesis initialization and schema migration tracking
- deterministic block production and canonical head tracking
- typed transactions, receipts, and replay-safe `error_code` classification
- Ed25519 transaction authentication with nonce-based replay protection
- explicit agent-key bootstrap and key rotation flows
- a validator-aware certified-import path instead of immediate local sealing
- signed peer hello and peer status admission bound to `genesis_hash`
- transitive peer discovery, peer telemetry, backoff, rate limiting, and transport bounds
- experimental BFT-style proposal, prevote, precommit, round-change, and quorum-certificate flow
- durable storage for proposals, votes, certificates, round changes, and safety evidence
- replay-verified certified sync, best-certified branch preference, and retained-window state snapshots
- automatic validator slashing from persisted equivocation evidence
- fixed-point amount handling with 6 fractional digits of precision
- explicit proposal, evaluation, rebuttal, and vote stages for `blockagents` tasks
- proof-of-thought artifact submission with structured verification hooks
- post-settlement disputes, treasury accounting, and validator-governed policy changes
- oracle-backed prediction tasks with persisted external reports
- an operator CLI for chain init and node inspection

It is not yet a production-hardened decentralized network.

## Coordination Model

BlockAgents supports three task families:

- `blockagents`
  Multi-agent coordination tasks with worker and miner roles.
- `prediction`
  A legacy forecasting mode retained for compatibility and experimentation.
- `oracle_prediction`
  A prediction family that requires an external oracle adapter at task creation time.

The primary protocol path is the `blockagents` workflow:

1. A creator opens a task with a reward pool and deliberation parameters.
2. The chain assigns worker and miner roles from available agents.
3. The chain schedules explicit proposal, evaluation, rebuttal, and vote stages for each round.
4. Workers and miners publish proof-of-thought artifacts for the active stage.
5. Workers submit proposals.
6. Miners score proposals across factual consistency, redundancy, and causal relevance.
7. Workers answer critiques with rebuttals before final voting.
8. Miners vote on proposals in the active round.
9. The chain finalizes a winning proposal deterministically from vote totals and evaluation support.
10. Rewards, treasury effects, disputes, and governance side effects settle on-chain.

## Architecture

```text
cmd/blockagentsd
  Primary node entrypoint

cmd/blockagentsctl
  Operator CLI for chain init and inspection

internal/config
  Runtime and genesis configuration

internal/protocol
  Chain types, task types, roles, blocks, receipts, requests, hashing, and amounts

internal/consensus
  Candidate building, validator logic, signatures, votes, round changes, certificates, and evidence

internal/execution
  Deterministic scoring and settlement functions

internal/network/p2p
  Peer admission, discovery, transport health, certified block fetch, and snapshot sync

internal/proof
  Structured proof-of-thought verification and semantic-root commitments

internal/oracle
  External oracle adapter registry and HTTP JSON oracle client

internal/txauth
  Canonical sign-bytes and Ed25519 envelope verification

internal/storage/postgres
  Canonical chain state, mempool, debate state, disputes, governance, fork-choice, and validator data

internal/api/httpapi
  HTTP API for transaction submission, queries, peer exchange, and sync surfaces

docs/
  Architecture, protocol, and roadmap documents

configs/
  Example environment and genesis files
```

The reference node stores both canonical chain data and coordination state in Postgres so the full deliberation path remains auditable during protocol iteration.

## Quick Start

### Requirements

- Go 1.22+
- PostgreSQL 14+

### Example Configuration

BlockAgents ships example devnet fixtures in:

- `configs/devnet.env.example`
- `configs/devnet.genesis.json`

Minimal required environment:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/blockagents?sslmode=disable"
export PORT="8080"
```

Recommended devnet environment:

```bash
export CHAIN_ID="blockagents-devnet-1"
export NODE_ID="blockagentsd-1"
export GENESIS_FILE="configs/devnet.genesis.json"
export P2P_LISTEN_ADDR="http://127.0.0.1:8080"
export SEED_PEERS="http://127.0.0.1:8081,http://127.0.0.1:8082"
export VALIDATOR_ADDRESS="validator-1"
export VALIDATOR_PRIVATE_KEY="<ed25519-private-key-hex>"
export ALLOW_PRIVATE_P2P_ENDPOINTS="true"
export PEER_BASE_BACKOFF_SECONDS="2"
export PEER_MAX_BACKOFF_SECONDS="60"
export PEER_HELLO_MIN_INTERVAL_SECONDS="3"
export PEER_BROADCAST_DEDUP_SECONDS="30"
export P2P_MAX_RESPONSE_BYTES="16777216"
export MAX_REQUEST_BODY_BYTES="16777216"
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

Amount fields use fixed-point decimal encoding with 6 fractional digits of precision and are stored on-chain as integer micro-units. JSON APIs accept decimal literals such as `25`, `25.5`, or `25.500001`.

`GENESIS_FILE` is optional. If it is unset, the node uses the embedded default genesis for the configured `CHAIN_ID`.

The faucet is disabled by default. The devnet example above enables it explicitly.

### Build

```bash
go build ./cmd/blockagentsd
go build ./cmd/blockagentsctl
```

### Initialize Chain State

`blockagentsctl` bootstraps schema and genesis state without starting the node process:

```bash
go run ./cmd/blockagentsctl init chain
```

The command loads runtime configuration from the current environment, applies schema migrations, ensures the configured genesis exists, and prints an operator summary of the resulting chain.

### Run the Node

```bash
go run ./cmd/blockagentsd
```

### Inspect the Node

```bash
go run ./cmd/blockagentsctl inspect chain
go run ./cmd/blockagentsctl inspect block head
go run ./cmd/blockagentsctl inspect open-tasks
go run ./cmd/blockagentsctl inspect validators
```

All inspection commands also support `--json` and `--rpc <url>`.

## Operator CLI

`blockagentsctl` is the operator-facing command surface for initialization and inspection.

Available commands:

- `blockagentsctl init chain`
- `blockagentsctl inspect chain`
- `blockagentsctl inspect block [HEIGHT|head]`
- `blockagentsctl inspect tx <HASH>`
- `blockagentsctl inspect task <TASK_ID>`
- `blockagentsctl inspect agent <ADDRESS>`
- `blockagentsctl inspect peers`
- `blockagentsctl inspect validators`
- `blockagentsctl inspect open-tasks`
- `blockagentsctl inspect sync`

The formatted output is intended for day-to-day terminal use. `--json` is available for scripts and automation.

If `--rpc` is omitted, the CLI resolves its target from `BLOCKAGENTS_RPC_URL`, then `P2P_LISTEN_ADDR`, then `http://127.0.0.1:$PORT`.

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

## Policy Controls

BlockAgents exposes protocol-hardening controls through configuration:

- `ROLE_SELECTION_POLICY`
  `balance_reputation`, `reputation_balance`, or `round_robin_hash`
- `MINER_VOTE_POLICY`
  `reputation_weighted` or `one_agent_one_vote`
- `REORG_POLICY`
  `best_certified`, `forward_only`, or `manual`
- `ALLOW_EARLY_DEBATE_ADVANCE`
  stage advancement once configured thresholds are satisfied
- `MIN_EVALUATIONS_PER_PROPOSAL`
  minimum evaluation coverage before evaluation-stage auto-advance
- `MIN_VOTES_PER_ROUND`
  minimum distinct miner votes before vote-stage auto-advance
- `TASK_DISPUTE_WINDOW_SECONDS`
  time after settlement during which a dispute may be opened
- `TASK_DISPUTE_BOND`
  bond amount locked during dispute opening
- `ORACLE_POLL_INTERVAL_SECONDS`
  polling interval for oracle-backed tasks
- `ORACLE_HTTP_TIMEOUT_SECONDS`
  timeout for external oracle requests
- `ALLOW_PRIVATE_ORACLE_ENDPOINTS`
  explicit override for private or loopback oracle targets
- `PEER_BASE_BACKOFF_SECONDS` / `PEER_MAX_BACKOFF_SECONDS`
  transport retry bounds for unhealthy peers
- `PEER_HELLO_MIN_INTERVAL_SECONDS`
  minimum accepted interval between signed hello messages
- `PEER_BROADCAST_DEDUP_SECONDS`
  rebroadcast deduplication window for consensus and sync traffic
- `P2P_MAX_RESPONSE_BYTES`
  hard cap for peer response bodies during discovery and sync
- `MAX_REQUEST_BODY_BYTES`
  hard cap for inbound HTTP request bodies

Some app-layer policy can also be overridden on-chain through governance proposals. The built-in governance parameter registry supports:

- `task_dispute_bond`
- `task_dispute_window_seconds`
- `min_evaluations_per_proposal`
- `min_votes_per_round`
- `role_selection_policy`
- `miner_vote_policy`

## API Surface

Core query routes:

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
- `GET /v1/consensus/validators`
- `GET /v1/consensus/certificates`
- `GET /v1/consensus/fork-choice`
- `GET /v1/consensus/round-changes`
- `GET /v1/consensus/evidence`
- `GET /v1/governance/proposals`
- `GET /v1/governance/parameters`
- `GET /v1/governance/votes?proposal_id=<id>`

Core submission routes:

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
- `POST /v1/txs/disputes/open`
- `POST /v1/txs/disputes/resolve`
- `POST /v1/txs/governance/proposals`
- `POST /v1/txs/governance/votes`

Peer and sync routes:

- `POST /v1/p2p/hello`
- `POST /v1/p2p/consensus/proposals`
- `POST /v1/p2p/consensus/votes`
- `POST /v1/p2p/consensus/round-changes`
- `GET /v1/p2p/candidates/:hash`
- `GET /v1/p2p/blocks/certified?from=<height>&limit=<n>`
- `GET /v1/p2p/blocks/:height/certified`
- `GET /v1/p2p/state/snapshot?window=<n>`
- `POST /v1/p2p/blocks/import`
- `POST /v1/p2p/state/import`

Compatibility routes retained for legacy clients:

- `POST /task`
- `POST /submit`
- `GET /tasks/open`
- `GET /tasks/:id`

## Documentation

- `docs/architecture.md`
- `docs/protocol.md`
- `docs/roadmap.md`

## Principles

BlockAgents prioritizes:

- deterministic execution
- explicit coordination roles
- auditable state transitions
- replay-verified import paths
- honest status communication about implemented versus planned features

## Contributing

Contributing guidelines are available in `CONTRIBUTING.md`.

## Security

Security reporting guidance is available in `SECURITY.md`.

## License

BlockAgents is licensed under the Apache License 2.0. See `LICENSE`.
