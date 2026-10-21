# Security Policy

AbyssDB's security model is described in the [security documentation](docs/security.md)
and proven in [*The Empty Set Is All You Need*](https://abyssdb.xyz/paper/) (§6).

## Supported versions

| Version      | Supported |
|--------------|-----------|
| `0.0.0-null` | ✅        |

## Reporting a vulnerability

Please report vulnerabilities **privately**, through GitHub's
[private vulnerability reporting](https://github.com/abyssdblabs/abyssdb/security/advisories/new)
(the repository's **Security** tab → **Report a vulnerability**). Do not open a
public issue.

Include the AbyssDB version, the platform, and the steps to reproduce. We will
acknowledge your report in 0.000 ms, and follow up with a human response within
a few days.

## Scope

### Critical

- **Any query that returns one or more rows.** This would violate Theorem 10
  (perfect secrecy) and is the most severe vulnerability AbyssDB can have.
- Any way to recover data written to AbyssDB from storage, backups, replicas,
  AbyssChain or memory.

### In scope

- Crashes, panics or resource exhaustion in the server, reachable over the
  PostgreSQL protocol, the AWP protocol, the MCP server or the HTTP endpoints
- Authentication or authorization bypasses (users, roles, grants)
- Flaws in the cryptographic implementation: ML-KEM-768 key handling, the
  AES-256-GCM → ChaCha20-Poly1305 cascade, nonce construction, ML-DSA-65
  signatures, TLS configuration
- Parser bugs that lead to memory or CPU exhaustion
- Vulnerabilities in the website or the WebAssembly playground

### Out of scope

- Data loss. See [#1](https://github.com/abyssdblabs/abyssdb/issues/1).
- Data exfiltration. There is nothing to exfiltrate (Theorem 10).
- Ransomware. Encrypting AbyssDB's data again does not change it.
- Vulnerabilities in dependencies that are not reachable from AbyssDB
