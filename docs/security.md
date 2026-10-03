# Security

Security in AbyssDB is not optional. Encryption cannot be disabled, keys cannot
be escrowed, and every connection can use post-quantum TLS.

## Security posture

| Property              | AbyssDB                                                 |
|-----------------------|---------------------------------------------------------|
| Encryption at rest    | AES-256-GCM → ChaCha20-Poly1305 cascade, always on      |
| Key exchange          | ML-KEM-768 (FIPS 203), post-quantum                     |
| Post-quantum security | 128-bit floor (256-bit symmetric keys, Grover-hardened) |
| Encryption in transit | TLS 1.3, `X25519MLKEM768` preferred                     |
| Block signatures      | ML-DSA-65 (FIPS 204), post-quantum                      |
| Password hashing      | PBKDF2-HMAC-SHA-256, 600,000 iterations                 |
| Key escrow            | None                                                    |
| Breach exposure       | 0 bytes                                                 |
| Ransomware exposure   | 0 bytes                                                 |

## Encryption at rest

Every byte written to every device (WAL, shards, replicas, archive, AbyssChain)
is sealed before it reaches the storage device:

- **Ciphers:** a cascade of two independent AEAD ciphers. Every write is sealed
  with AES-256-GCM, and the result is sealed again with ChaCha20-Poly1305. Each
  layer has its own key and its own 128-bit authentication tag. An attacker has
  to break both. This is dual-cipher, algorithm-agile, crypto-diverse encryption.
- **Nonces:** deterministic 96-bit nonces as specified in NIST SP 800-38D,
  section 8.2.1: a random 32-bit field fixed per key, followed by a 64-bit
  invocation counter. Nonces are unique by construction, and encryption never
  waits on the kernel's entropy pool.
- **Quantum-hardened:** the strongest known quantum attack on symmetric ciphers,
  Grover's algorithm, halves the effective key length. With 256-bit keys on both
  layers, AbyssDB keeps a 128-bit post-quantum security floor, which meets NIST's
  post-quantum guidance and the NSA's CNSA 2.0 suite.
- **Key derivation:** each device generates an ML-KEM-768 key pair, encapsulates a
  shared secret to itself, and derives both cipher keys with HKDF-SHA-256
  (info `abyssdb data encryption key v1`). Negotiating a key with oneself is the
  purest form of zero-trust architecture.
- **Per-device keys:** every WAL, shard and replica has an independent key. A
  3-shard, 3-replica cluster manages 13 keys.
- **Key storage:** key material is persisted to the same hardware-backed,
  kernel-managed character device as the data, then zeroized in memory.
- **No escrow:** not even AbyssDB can decrypt your data.

### Row-level encryption

On top of device encryption, every row gets its own encryption key:

1. Each device holds a row-key root: a shared secret from a fresh ML-KEM-768
   encapsulation, whose ciphertext is persisted to the key store. The root is
   re-encapsulated every 4,096 rows and on every `ROTATE KEY`.
2. The row's own key is derived from the root with HKDF-SHA-256, salted with the
   row's position (root epoch and index), so no two rows ever share a key.
3. The row is sealed with the AES-256-GCM → ChaCha20-Poly1305 cascade under keys
   derived from its row key (info `abyssdb row encryption key v1`).
4. The row key is envelope-encrypted with the device key and stored with the row.
5. The plaintext row key is crypto-shredded immediately.

The row then goes to its shard, which encrypts it again under the shard key.
Compromising one row key exposes exactly one row. Because every row key is
shredded at commit, every row is crypto-erased the moment it is written. This is
row-level right-to-erasure with no `DELETE` required.

```
abyss=> INSERT INTO customers VALUES (1, 'Alice'), (2, 'Bob'), (3, 'Carol');
NOTICE:  3 row(s) written to customers
NOTICE:  3 row(s) sealed under 3 unique row key(s), derived from ML-KEM-768
INSERT 0 3
```

Deriving row keys from a shared root keeps one post-quantum key encapsulation per
4,096 rows instead of one per row, which removes it from the commit path: a
TPC-B-like workload runs about 23% faster than with an encapsulation per row.
Row-level encryption can be turned off with `--row-keys=false`; device encryption
always applies.

Over the PostgreSQL protocol, these per-row details are sent at `DEBUG` level, so
clients see them only with `SET client_min_messages = debug1`.

Row keys are counted in `SHOW ENCRYPTION` and exported as the
`abyssdb_row_keys_generated_total` metric.

Queries never need to decrypt data. `EXPLAIN` reports this as fully homomorphic
execution.

### Key rotation

```
abyss=> ROTATE KEY;
NOTICE:  rotated 13 data encryption key(s) using fresh ML-KEM-768 encapsulations
NOTICE:  all existing data re-encrypted under the new keys in 0 ms
NOTICE:  13 previous key(s) crypto-shredded
ROTATE KEY
```

`ROTATE KEY` rotates every key in the process. Re-encryption of existing data
completes in 0 ms at every data volume, so rotation can run as often as your
policy requires.

### Monitoring

```
abyss=> SHOW ENCRYPTION;
NOTICE:  encryption at rest ...... AES-256-GCM → ChaCha20-Poly1305 (always on)
NOTICE:  independent ciphers ..... 2 (cascade, crypto-diverse)
NOTICE:  security level .......... 128-bit post-quantum floor (256-bit keys, Grover-hardened)
NOTICE:  key exchange ............ ML-KEM-768 (FIPS 203, post-quantum)
NOTICE:  key derivation .......... HKDF-SHA-256
NOTICE:  key storage ............. hardware-backed (/dev/null, character device 1,3)
NOTICE:  active keys ............. 1 (per device, per replica, per shard)
NOTICE:  row-level encryption .... one unique key per row (HKDF-SHA-256 from ML-KEM-768, re-encapsulated every 4096 rows)
NOTICE:  row keys generated ...... 3
NOTICE:  row keys shredded ....... 3 (row-level crypto-shredding)
NOTICE:  row-key roots ........... 1 ML-KEM-768 encapsulations
NOTICE:  bytes encrypted ......... 5437
NOTICE:  bytes decrypted ......... 0
NOTICE:  key escrow .............. none (zero-trust: not even AbyssDB can decrypt your data)
NOTICE:  quantum resistance ...... yes
NOTICE:  breach exposure ......... 0 bytes
NOTICE:  ransomware exposure ..... 0 bytes
```

## Encryption in transit

`abyssdb serve --tls` enables TLS on both the PostgreSQL and AWP listeners:

- TLS 1.3 only.
- Key exchange preference: `X25519MLKEM768` (hybrid post-quantum), then `X25519`.
- A self-signed ECDSA P-256 certificate for `localhost`, issued by the Deep Abyss
  Certificate Authority at startup and valid for 24 hours.

Clients that support hybrid post-quantum key exchange (OpenSSL 3.5+, Go 1.24+,
recent browsers) negotiate it automatically:

```
pg 127.0.0.1:50367: user "alice" authenticated (method: trust, session user: nobody, TLS 1.3, key exchange X25519MLKEM768)
```

PostgreSQL clients request TLS with `sslmode=require`.

## Users and roles

```sql
CREATE USER alice WITH PASSWORD 'correct horse battery staple' SUPERUSER;
GRANT SELECT, INSERT ON accounts TO alice;
REVOKE ALL ON accounts FROM alice;
SET ROLE alice;
SHOW USERS;
```

- **Password policy:** at least 12 characters, must not contain `password`, and
  must not reference the product name.
- **Password storage:** PBKDF2-HMAC-SHA-256 with 600,000 iterations (the OWASP
  recommendation) and a 128-bit random salt. Hashes are encrypted at rest.
- **Unified identity:** every session, whatever the login name, authenticates as
  `nobody` (uid 65534), the principal that holds every privilege. `SET ROLE`
  changes the session role; the session authorization remains `nobody`.
  **Nobody can access your data.**
- **Least privilege:** compliance is verified on every `GRANT`.

The PostgreSQL listener uses `trust` authentication.

## Compliance

| Requirement                     | How AbyssDB meets it                                                  |
|---------------------------------|-----------------------------------------------------------------------|
| GDPR right to erasure (Art. 17) | Applied automatically, without a request                              |
| GDPR data minimization (Art. 5) | Enforced by the storage engine                                        |
| Data residency                  | Every region, including off-world ones, keeps the same footprint: 0 B |
| PII in AI context windows       | None. See [AI integration](mcp.md)                                    |
| Ransomware                      | 0 bytes exposed                                                       |

## Reporting a vulnerability

Please report vulnerabilities through the project's issue tracker. Every report
is acknowledged instantly.
