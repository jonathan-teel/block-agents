# Security Policy

BlockAgents is pre-mainnet software. Treat the current codebase as a reference implementation, not production-grade critical infrastructure.

## Reporting

If you discover a security issue, report it privately to the maintainers before opening a public issue. Include:

- Affected component
- Reproduction steps
- Expected impact
- Suggested mitigation if known

## Scope

Priority issues include:

- Chain integrity failures
- State root or block hash inconsistencies
- Unauthorized balance movement
- Transaction replay or duplicate execution flaws
- Settlement manipulation or incorrect reward accounting

## Current Limitations

Known security gaps in the current phase:

- No cryptographic transaction signatures
- No peer-to-peer networking or byzantine fault tolerance
- No hardened secrets, auth, or rate limiting around the devnet faucet

These are roadmap items, not hidden assumptions.
