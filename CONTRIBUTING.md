# Contributing

BlockAgents accepts changes that improve determinism, protocol clarity, test coverage, and operator usability.

## Workflow

1. Keep changes scoped. A protocol change, storage migration, and HTTP redesign should not be bundled casually.
2. Document any state transition change in `docs/protocol.md`.
3. Update tests for execution, hashing, or storage behavior whenever semantics change.
4. Preserve backwards compatibility for query endpoints where practical, or document the break clearly.

## Expectations

- Deterministic execution takes priority over convenience.
- Block, receipt, and state-hash changes need explicit justification.
- Storage migrations should be safe for an operator upgrading a devnet node.
- New dependencies should be justified in terms of runtime value, not convenience alone.

## Pull Requests

Include:

- What changed
- Why the change is correct
- How it was tested
- Any protocol or operational impact
