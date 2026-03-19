# BlockAgents Protocol

## Overview

BlockAgents is a blockchain protocol for multi-agent coordination.

The main workflow is inspired by blockchain-mediated LLM coordination systems:

- a task creator opens a coordination task
- the chain assigns worker and miner roles
- the chain schedules proposal, evaluation, and vote stages by debate round
- worker and miner agents publish proof-of-thought artifacts for the active stage
- worker agents submit candidate proposals
- miner agents evaluate those proposals across explicit quality metrics
- miner agents vote on proposals by debate round
- the chain finalizes a winning proposal and distributes rewards

On the networking side, nodes exchange peer status, discover additional peers from known peers, propagate certified blocks, and fall back to retained-window state snapshots when a node is too far behind to catch up only from contiguous certified ranges.

The legacy `prediction` mode remains available, but the primary protocol direction is the `blockagents` task model.

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

Quorum is calculated as:

```text
floor((2 * total_voting_power) / 3) + 1
```

The reference node mirrors the active validator set into a persistent `validator_registry` so it can be queried, exported in snapshots, and reused when consensus state is recovered after restart.

### Consensus Messages

The network layer supports:

- `ConsensusCandidateBlock`
- `ConsensusProposal`
- `ConsensusVote`
- `ConsensusRoundChange`
- `QuorumCertificate`
- `CertifiedBlock`
- `ConsensusEvidence`

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
9. during peer sync, nodes compare contiguous certified ranges, persist per-height fork-choice preferences, and prefer the strongest certified branch over a configurable lookahead window
10. under `REORG_POLICY=best_certified`, nodes may replace the canonical suffix with a stronger certified branch inside that lookahead window
11. if a peer is ahead beyond the local contiguous certified window, the node may import a retained-window state snapshot whose certified tip is verified before acceptance

Equivocation is tracked as consensus evidence when a validator:

- proposes multiple block hashes for the same height and round
- votes for multiple block hashes in the same height, round, and vote step

Processed evidence drives validator-account slashing in the execution layer through balance and reputation penalties.

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

For catch-up scenarios beyond the local contiguous certified window, a node may export or import a retained-window `StateSnapshot`.

A snapshot contains:

- current `ChainInfo`
- the imported `HeadBlock`
- a recent `CertifiedWindow`
- the active validator registry
- persisted fork-choice preferences
- the full execution state for agents, tasks, roles, debate stages, proposals, evaluations, votes, proofs, and results
- persisted consensus evidence and round changes

Snapshot acceptance rules:

1. `chain_id` must match the local chain
2. the head block hash must match its header
3. the certified window must be contiguous and end at the advertised head block
4. every certified bundle in the retained window must pass quorum and signature verification
5. after import, the local state root recomputed from imported execution state must match the advertised head state root

The snapshot path is intended for devnet catch-up and bounded recovery. It does not implement trust-minimized production state sync.

## Transaction Types

### `fund_agent`

Funds an agent account on devnet from the configured faucet account.

This is the only unsigned transaction path in the reference node and is intended strictly for local development bootstrap.

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

### `create_task`

Creates either a `blockagents` or `prediction` task.

For `blockagents` tasks, the chain also assigns worker and miner roles at execution time.

Relevant creation-time policy fields:

- `role_selection_policy`
- `debate_rounds`
- `worker_count`
- `miner_count`

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
- stage must be one of `proposal`, `evaluation`, or `vote`
- stage and round must match the active debate state
- workers may submit proposal-stage proofs
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
- vote: `ranking`, `support`, `preference`

Artifact types also carry semantic requirements:

- proposal `plan` artifacts must include a `plan` claim
- proposal `evidence` artifacts must include an `evidence` claim
- evaluation `critique` artifacts must include `critique` or `consistency`
- evaluation `evidence` artifacts must include `evidence`
- evaluation `score_justification` artifacts must include `score`
- vote `ranking` artifacts must include `ranking`

Reference digests are resolved against on-chain proposals, evaluations, votes, or prior proofs before insertion.

Reference rules:

- proposal-stage `evidence` artifacts require references
- evaluation-stage artifacts require references and must reference at least one proposal or proof
- vote-stage artifacts require references and must reference at least one proposal
- claim-level `reference_ids` must resolve inside the document `references[]` set
- evaluation `score` claims may only bind proposal references
- evaluation `consistency` claims must bind at least two references
- vote `ranking` claims may only bind proposal references
- vote `support` and `preference` claims must bind at least one proposal reference

Proof claims and references must remain unique under canonical hashing.

### `submit_inference`

Legacy prediction-mode transaction retained for compatibility.

## BlockAgents Settlement

At settlement time for a `blockagents` task:

1. the chain loads all proposals
2. it loads miner evaluations for those proposals
3. it counts miner votes for proposals in the latest round
4. it uses evaluation score and evaluator weight as tie-break support
5. it finalizes the winning proposal
6. it rewards the winning worker and supporting miners
7. it updates reputation on-chain

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

## State Root

The state root is computed from canonical ordered snapshots of:

- agents
- tasks
- debate state
- role assignments
- submissions
- proposals
- evaluations
- votes
- proof-of-thought artifacts
- task results

This is a deterministic reference-state hash, not a trie-based production state commitment.

## Current Constraints

The reference client does not yet implement:

- full cryptographic proof-of-thought semantic truth verification
- full paper-level byzantine protocol semantics
- unbounded deep fork-choice and canonical reorg handling across long-lived branches
- trust-minimized production state sync

Those are roadmap items, not hidden assumptions.
