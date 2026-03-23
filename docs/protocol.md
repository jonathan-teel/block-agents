# BlockAgents Protocol

## Overview

BlockAgents is a blockchain protocol for multi-agent coordination.

The main workflow is inspired by blockchain-mediated LLM coordination systems:

- a task creator opens a coordination task
- the chain assigns worker and miner roles
- the chain schedules proposal, evaluation, rebuttal, and vote stages by debate round
- worker and miner agents publish proof-of-thought artifacts for the active stage
- worker agents submit candidate proposals
- miner agents evaluate those proposals across explicit quality metrics
- worker agents submit rebuttals before final voting
- miner agents vote on proposals by debate round
- the chain finalizes a winning proposal and distributes rewards

On the networking side, nodes exchange peer status, discover additional peers from known peers, propagate certified blocks, verify certified branches against the persisted active validator set, and fall back to retained-window state snapshots when branch replay is not the best recovery path. Peer admission and snapshot sync are bound to the local `genesis_hash`.

Prediction-family tasks may also attach an external oracle adapter so settlement can resolve against persisted off-chain data rather than the built-in synthetic outcome path.

The legacy `prediction` mode remains available, `oracle_prediction` provides an explicit oracle-required prediction family, and the primary protocol direction remains the `blockagents` task model.

## Amount Encoding

Balance-like values use fixed-point decimal amounts with 6 fractional digits of precision.

This applies to:

- account balances
- task `reward_pool`
- task `min_stake`
- submission stake
- dispute bonds
- treasury transfer amounts
- applied balance slashing penalties

JSON requests may encode these amounts as numeric literals or strings. The reference client normalizes them into integer micro-units before they enter consensus state.

## Task Types

### `blockagents`

A coordination task with:

- `question`
- `deadline`
- `reward_pool`
- `min_stake`
- `debate_rounds`
- `worker_count`
- `miner_count`
- `role_selection_policy`

### `prediction`

A simpler forecasting task retained for compatibility with the earlier chain design.

Prediction tasks may optionally define:

- `oracle_source`
- `oracle_endpoint`
- `oracle_path`

The reference client currently supports `oracle_source = http_json`, which fetches JSON from `oracle_endpoint` and extracts a numeric value from the dot-path in `oracle_path`.

### `oracle_prediction`

An oracle-backed prediction task that requires:

- `oracle_source`
- `oracle_endpoint`
- `oracle_path`

This task family uses the same submission and settlement path as `prediction`, but task creation fails unless the oracle configuration is present.

## Roles

### Worker

Workers submit candidate answers or proposals for a task round.

### Miner

Miners evaluate worker proposals and contribute to canonical finalization.

In the reference client, miners and workers are assigned deterministically from available funded agents when a `blockagents` task is created. The assignment order is controlled by `role_selection_policy`.

## Chain Objects

### Transaction

Every transaction contains:

- `hash`
- `type`
- `sender`
- `nonce`
- `public_key`
- `signature`
- `sequence`
- `payload`
- `accepted_at`

For all non-faucet transactions, `nonce`, `public_key`, and `signature` are required.

The sign-bytes are the canonical JSON encoding of:

- `chain_id`
- `type`
- `sender`
- `nonce`
- `public_key`
- `payload`

The transaction hash is derived from the envelope fields and payload. Unsigned dev-faucet transactions remain allowed on devnet and include `accepted_at` in the hash to preserve uniqueness.

The first valid authenticated envelope for an address binds that address to a public key. Subsequent authenticated envelopes must use the bound key unless the address rotates keys through `rotate_agent_key`.

### Receipt

Every included transaction receives a receipt with:

- `tx_hash`
- `block_height`
- `success`
- `error_code`
- `error`
- `events`

### Block

Blocks contain:

- header
- transactions
- receipts
- block-level events

### Validator Set

The validator set is defined in genesis with:

- `address`
- `public_key`
- `power`
- `active`

Quorum is calculated as:

```text
floor((2 * total_voting_power) / 3) + 1
```

The reference node persists the validator set in `validator_registry` so active membership can be exported in snapshots and reused when consensus state is recovered after restart. Validator membership is currently static or administratively managed in the reference client rather than mutated by public transaction paths.

### Consensus Messages

The network layer supports:

- `ConsensusCandidateBlock`
- `ConsensusProposal`
- `ConsensusVote`
- `ConsensusRoundChange`
- `QuorumCertificate`
- `CertifiedBlock`
- `ConsensusEvidence`

Peer transport also uses:

- `PeerHello`
- `PeerStatus`

Votes use:

- `prevote`
- `precommit`

The current devnet flow is:

1. the scheduled proposer builds a candidate block against the current head
2. the proposer signs and broadcasts a proposal for that block hash
3. followers fetch that candidate block from the proposer and replay-validate it against their local head before voting
4. validators issue `prevote`
5. once `prevote` quorum forms, validators issue `precommit`
6. if the round stalls, validators emit signed `ConsensusRoundChange` messages and proposer selection advances to the next round after quorum
7. round-change messages are persisted so quorum-derived round state can be recovered after restart
8. once `precommit` quorum forms, the preferred certified block is imported into canonical state and propagated to peers
9. during peer sync, nodes find a common ancestor with candidate peers, fetch the certified suffix, verify it against the persisted active validator set, and persist per-height fork-choice preferences
10. under `REORG_POLICY=best_certified`, nodes may replace the canonical suffix with a stronger certified branch discovered through that common-ancestor sync path
11. if branch replay is not the best recovery path, the node may import a retained-window state snapshot whose certified tip and recomputed state root are verified before acceptance

Equivocation is tracked as consensus evidence when a validator:

- proposes multiple block hashes for the same height and round
- votes for multiple block hashes in the same height, round, and vote step

Processed evidence drives validator-account slashing in the execution layer through balance and reputation penalties.
Rejected dispute bonds and slashed validator balances are routed to the configured treasury account.

### Peer Admission and Transport

Peer admission is validator-authenticated in the reference client.

`PeerHello` and `PeerStatus` messages are signed by the node's validator key over canonical sign-bytes containing:

- `node_id`
- `chain_id`
- `genesis_hash`
- `listen_addr`
- `validator_address`
- timestamp field (`seen_at` or `observed_at`)
- for `PeerStatus`, `head_height` and `head_hash`

Admission rules:

- `chain_id` must match the local chain
- `genesis_hash` must match the local chain
- `validator_address` must exist in the active validator set
- the message signature must verify against that validator public key

Transport hardening in the reference client also applies:

- retry backoff for failing peers
- per-node hello rate limiting
- duplicate suppression for proposal, vote, round-change, and certified-block rebroadcasts
- peer telemetry for score, failures, backoff window, and last transport error

Operator-facing transport endpoints include:

- `GET /v1/p2p/peers` for admitted signed peers
- `GET /v1/p2p/telemetry` for runtime transport health

### Certified Block Import

A certified block bundle contains:

- the canonical block
- the proposer-signed consensus proposal
- the set of precommit votes used for finality
- the quorum certificate derived from those votes

Importing nodes:

1. verify the proposal signature
2. verify every precommit signature
3. verify quorum power and signer list
4. replay the full block against local state
5. compare receipts, events, and state commitments
6. accept the block only if replay is identical

When the imported branch diverges from the current head and reorg policy permits it, the node rebuilds canonical execution state from genesis through the retained prefix and then imports the stronger certified suffix.

Certified blocks are available over:

- `GET /v1/p2p/blocks/certified?from=<height>&limit=<n>`
- `GET /v1/p2p/blocks/:height/certified`
- `POST /v1/p2p/blocks/import`

### State Snapshot Sync

For catch-up and operator-managed recovery scenarios, a node may export or import a retained-window `StateSnapshot`.

A snapshot contains:

- current `ChainInfo`
- the imported `HeadBlock`
- a recent `CertifiedWindow`
- the full validator registry
- persisted fork-choice preferences
- the full execution state for agents, tasks, roles, debate stages, proposals, evaluations, rebuttals, votes, proofs, results, and governance state
- oracle reports
- task disputes
- persisted consensus evidence and round changes

Snapshot acceptance rules:

1. `chain_id` must match the local chain
2. `genesis_hash` must match the local chain
3. the head block hash must match its header
4. the certified window must be contiguous and end at the advertised head block
5. every certified bundle in the retained window must pass quorum and signature verification
6. after import, the local state root recomputed from imported execution state must match the advertised head state root

The snapshot path is intended for devnet and operator-managed recovery. It verifies the retained certified window, the imported head block, and the recomputed state root, but it is not a full trust-minimized production state-sync protocol.

## Transaction Types

### `fund_agent`

Funds an agent account on devnet from the configured faucet account.

This is the only unsigned transaction path in the reference node and is intended strictly for local development bootstrap.

The faucet grant amount is fixed by node configuration. Clients should submit the target `agent` only and must not request a custom amount.

### `bootstrap_agent_key`

Records an explicit on-chain bootstrap event for an agent key that has already been bound through the authenticated envelope path.

This transaction is primarily an audit artifact. The first valid signed transaction for an address still establishes the bound public key.

### `rotate_agent_key`

Rotates an agent public key.

Execution rules:

- the transaction must be authorized by the currently bound public key
- `new_public_key` must differ from the current public key
- `new_signature` must prove possession of the replacement key over the rotation sign-bytes
- successful rotations are recorded in `agent_key_rotations`

### `open_dispute`

Opens a bonded challenge against a settled task.

Payload:

```json
{
  "task_id": "task-id",
  "challenger": "alice",
  "reason": "settlement should be reviewed"
}
```

Execution rules:

- the sender must match `challenger`
- the task must already be settled
- the challenge must be opened inside `TASK_DISPUTE_WINDOW_SECONDS` from `settled_at`
- only one open dispute may exist per task
- the challenger must lock `TASK_DISPUTE_BOND` from account balance

### `resolve_dispute`

Resolves an open task dispute.

Payload:

```json
{
  "dispute_id": 1,
  "resolver": "validator-1",
  "resolution": "reject",
  "notes": "canonical settlement stands"
}
```

Execution rules:

- the sender must match `resolver`
- `resolver` must be an active validator
- `resolution` must be `reject` or `uphold`
- `reject` transfers the dispute bond to the treasury account and leaves the settled result in place
- `uphold` refunds the bond to the challenger and marks the task as `disputed`

### `create_task`

Creates a `blockagents`, `prediction`, or `oracle_prediction` task.

For `blockagents` tasks, the chain also assigns worker and miner roles at execution time.

Relevant creation-time policy fields:

- `role_selection_policy`
- `debate_rounds`
- `worker_count`
- `miner_count`
- `oracle_source`
- `oracle_endpoint`
- `oracle_path`

### `submit_proposal`

Worker transaction for submitting a proposal in a given debate round.

Payload:

```json
{
  "task_id": "task-id",
  "agent": "worker-1",
  "round": 1,
  "content": "Candidate answer"
}
```

Execution rules:

- task must exist
- task must be `blockagents`
- task must still be open
- current debate stage must be `proposal`
- round must be within `debate_rounds`
- round must match the active debate round
- sender must have already submitted at least one proposal-stage proof artifact
- sender must be assigned as a worker

### `submit_evaluation`

Miner transaction for evaluating a proposal on explicit dimensions.

Payload:

```json
{
  "task_id": "task-id",
  "proposal_id": 1,
  "evaluator": "miner-1",
  "round": 1,
  "factual_consistency": 0.9,
  "redundancy_score": 0.8,
  "causal_relevance": 0.85,
  "comments": "Grounded and concise"
}
```

Execution rules:

- task must exist
- task must be `blockagents`
- current debate stage must be `evaluation`
- evaluator must be assigned as a miner
- proposal must exist for the same task and round
- round must match the active debate round
- sender must have already submitted at least one evaluation-stage proof artifact
- all metric values must be within `[0, 1]`

The chain computes:

```text
overall_score = (factual_consistency + redundancy_score + causal_relevance) / 3
```

### `submit_rebuttal`

Worker transaction for answering evaluation-stage critiques in a given debate round.

Payload:

```json
{
  "task_id": "task-id",
  "proposal_id": 1,
  "agent": "worker-1",
  "round": 1,
  "content": "The evaluation omits evidence already present in the proposal."
}
```

Execution rules:

- task must exist
- task must be `blockagents`
- current debate stage must be `rebuttal`
- agent must be assigned as a worker
- proposal must exist for the same task and round
- round must match the active debate round
- sender must have already submitted at least one rebuttal-stage proof artifact
- a worker may submit one rebuttal per round

### `submit_vote`

Miner transaction for voting on a proposal in a given round.

Payload:

```json
{
  "task_id": "task-id",
  "proposal_id": 1,
  "voter": "miner-1",
  "round": 1,
  "reason": "Best supported answer"
}
```

Execution rules:

- task must exist
- task must be `blockagents`
- current debate stage must be `vote`
- voter must be assigned as a miner
- proposal must exist for the same task and round
- round must match the active debate round
- sender must have already submitted at least one vote-stage proof artifact
- a miner may vote once per round

### `submit_proof`

Reasoning-artifact transaction for the active debate stage.

Payload:

```json
{
  "task_id": "task-id",
  "agent": "worker-1",
  "round": 1,
  "stage": "proposal",
  "artifact_type": "draft",
  "content": "{\"schema_version\":1,\"summary\":\"draft reasoning\",\"claims\":[{\"kind\":\"observation\",\"statement\":\"proposal A addresses the task\"}],\"conclusion\":\"proposal A should be submitted\"}"
}
```

Execution rules:

- task must exist
- task must be `blockagents`
- stage must be one of `proposal`, `evaluation`, `rebuttal`, or `vote`
- stage and round must match the active debate state
- workers may submit proposal-stage and rebuttal-stage proofs
- miners may submit evaluation-stage and vote-stage proofs

Every proof artifact stores:

- stage
- round
- artifact type
- full artifact content
- content hash
- claim root
- semantic root
- optional parent reference

`content` is verified as structured JSON with:

- `schema_version`
- `summary`
- `claims[]`
- optional `references[]`
- `conclusion`

Proof contents are normalized from structured JSON and committed with:

- `content_hash`: hash of normalized canonical JSON
- `claim_root`: merkle-style commitment over normalized claims
- `semantic_root`: hash over summary, claims, references, and conclusion

Claim kinds are stage-scoped:

- proposal: `observation`, `hypothesis`, `plan`, `evidence`
- evaluation: `evidence`, `critique`, `score`, `consistency`
- rebuttal: `counter`, `clarification`, `evidence`, `support`
- vote: `ranking`, `support`, `preference`

Artifact types also carry semantic requirements:

- proposal `plan` artifacts must include a `plan` claim
- proposal `evidence` artifacts must include an `evidence` claim
- evaluation `critique` artifacts must include `critique` or `consistency`
- evaluation `evidence` artifacts must include `evidence`
- evaluation `score_justification` artifacts must include `score`
- rebuttal `response` artifacts must include `counter` or `clarification`
- rebuttal `clarification` artifacts must include `clarification`
- rebuttal `evidence` artifacts must include `evidence`
- vote `ranking` artifacts must include `ranking`

Reference digests are resolved against on-chain proposals, evaluations, votes, or prior proofs before insertion.

Reference rules:

- proposal-stage `evidence` artifacts require references
- evaluation-stage artifacts require references and must reference at least one proposal or proof
- rebuttal-stage artifacts require references and must reference at least one proposal or evaluation
- vote-stage artifacts require references and must reference at least one proposal
- claim-level `reference_ids` must resolve inside the document `references[]` set
- evaluation `score` claims may only bind proposal references
- evaluation `consistency` claims must bind at least two references
- rebuttal `counter`, `evidence`, and `support` claims must bind at least one proposal or evaluation reference
- vote `ranking` claims may only bind proposal references
- vote `support` and `preference` claims must bind at least one proposal reference

Proof claims and references must remain unique under canonical hashing.

### `submit_inference`

Prediction-family transaction retained for compatibility with `prediction` and `oracle_prediction`.

### `submit_governance_proposal`

Active-validator transaction for proposing treasury transfers or app-policy updates.

Supported proposal types:

- `treasury_transfer`
- `parameter_change`

Supported parameter names:

- `task_dispute_bond`
- `task_dispute_window_seconds`
- `min_evaluations_per_proposal`
- `min_votes_per_round`
- `role_selection_policy`
- `miner_vote_policy`

### `submit_governance_vote`

Active-validator transaction for voting `approve` or `reject` on an open governance proposal.

## Oracle Reports

Oracle-backed prediction-family tasks resolve from persisted `oracle_reports`.

The reference client currently supports one external adapter:

- `http_json`

Oracle adapter behavior:

- the background oracle loop polls open prediction-family tasks whose deadline has passed
- the adapter fetches `oracle_endpoint` over HTTP
- the adapter extracts a numeric value from `oracle_path`
- accepted values must remain within `[0,1]`
- private, loopback, and link-local oracle targets are rejected by default
- the resulting report is persisted before settlement and becomes part of the deterministic execution snapshot

## BlockAgents Settlement

At settlement time for a `blockagents` task:

1. the chain loads all proposals
2. it loads miner evaluations for those proposals
3. it loads worker rebuttals for the latest round as part of the auditable debate record
4. it counts miner votes for proposals in the latest round
5. it uses evaluation score and evaluator weight as tie-break support
6. it finalizes the winning proposal
7. it rewards the winning worker and supporting miners
8. it updates reputation on-chain

### Proposal Scoring

For each proposal:

```text
proposal_score = sum(overall_score * evaluator_reputation) / sum(evaluator_reputation)
```

### Winning Proposal

The winning proposal is selected from the latest debate round:

```text
1. highest miner vote power
2. highest raw miner vote count
3. highest evaluation score
4. highest evaluation weight
```

### Reward Distribution

In the reference client:

- the winning worker receives the majority of the reward pool
- miners who voted for the winning proposal share the remaining miner reward pool according to `MINER_VOTE_POLICY`

This is a practical first implementation of miner/worker incentives, not the final economic design.

## Prediction Settlement

Prediction-mode tasks continue to settle through the earlier forecast workflow:

- stake-weighted consensus snapshots
- deterministic outcome resolution
- score-based payout and reputation update

For prediction-family tasks with an oracle source:

- settlement waits for a persisted oracle report
- the latest persisted report value becomes the task outcome
- replay uses the stored report rather than reaching back out to the external source

## Dispute Windows

Settled tasks may be challenged within the configured dispute window.

Dispute semantics in the reference client:

- a challenger posts a bonded `open_dispute` transaction
- an active validator resolves the dispute with `resolve_dispute`
- rejected disputes transfer the bond to treasury
- upheld disputes refund the bond and move the task into `disputed` status for further operator review

The effective dispute bond and dispute window may be overridden by executed governance parameter proposals.

## Governance

Governance is validator-scoped in the reference client.

Execution semantics:

- only active validators may submit governance proposals
- only active validators may vote on open proposals
- proposals finalize after `voting_deadline`
- approval requires validator power meeting the protocol supermajority threshold
- `treasury_transfer` proposals move funds from the treasury account to a target agent account
- `parameter_change` proposals update the on-chain governance parameter registry

Governance state is queryable over:

- `GET /v1/governance/proposals`
- `GET /v1/governance/parameters`
- `GET /v1/governance/votes?proposal_id=<id>`

## State Root

The state root is computed from canonical ordered snapshots of:

- agents
- validator registry
- tasks
- debate state
- role assignments
- submissions
- proposals
- evaluations
- rebuttals
- votes
- proof-of-thought artifacts
- task results
- task disputes
- oracle reports
- governance parameters
- governance proposals
- governance votes

This is a deterministic reference-state hash, not a trie-based production state commitment.

## Current Constraints

The reference client does not yet implement:

- full cryptographic proof-of-thought semantic truth verification
- full paper-level byzantine protocol semantics
- fully trust-minimized production state sync
- production-grade peer-discovery and transport adversary handling beyond the current validator-authenticated devnet model
- broader oracle adapter coverage and stronger oracle authenticity guarantees beyond the current HTTP JSON polling model

Those are roadmap items, not hidden assumptions.
