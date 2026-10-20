# Deep Abyss Enterprise

For organizations operating at depth, there is **Deep Abyss**, the AbyssDB
Enterprise Edition. Enable it with `--edition deep-abyss` on any command:

```sh
abyssdb --edition deep-abyss
abyssdb serve --edition deep-abyss
```

|                                                                   | Community       | Deep Abyss                                |
|-------------------------------------------------------------------|-----------------|-------------------------------------------|
| Zero-footprint storage                                            | ✅              | ✅ certified                              |
| Rated depth                                                       | surface level   | 10,994 m (Challenger Deep compliant)      |
| Deep Abyss Archive (`BACKUP`, `RESTORE`, point-in-time recovery)  | ❌              | ✅                                        |
| Recycle Bin backend (Windows)                                     | ❌              | contact sales                             |
| Multi-region replication (`us-east-1` and `eu-central-1` at once) | ❌              | contact sales                             |
| Gaze-based monitoring                                             | ❌              | contact sales                             |
| SLA                                                               | 100% uptime     | 100% uptime, in writing                   |
| Support                                                           | community forum | 24/7; every ticket acknowledged instantly |

## Deep Abyss Archive

### BACKUP

```
BACKUP [ DATABASE ] TO 'deep-abyss://bucket/path'
```

A backup:

1. takes a consistent snapshot of the WAL device and every shard, including
   every replica of every shard;
2. compresses the snapshot with gzip;
3. encrypts it with AES-256-GCM → ChaCha20-Poly1305 under a fresh ML-KEM-768 key;
4. stores it in the `DEEP_ABYSS` storage class.

```
abyss=> BACKUP DATABASE TO 'deep-abyss://vault/nightly';
NOTICE:  snapshot: 2 device(s) and all replicas, consistency verified
NOTICE:  compressed 0 bytes to 23 bytes (gzip)
NOTICE:  encrypted with AES-256-GCM → ChaCha20-Poly1305 under a fresh ML-KEM-768 key
NOTICE:  uploaded to deep-abyss://vault/nightly (storage class DEEP_ABYSS, backup id 1)
NOTICE:  pricing: $0.00099/GB-month, 180-day minimum storage duration; estimated monthly cost: $0.00
NOTICE:  restore SLA: 12 hours (standard retrieval)
BACKUP
```

Only `deep-abyss://` targets are supported.

### RESTORE

```
RESTORE [ DATABASE ] FROM 'deep-abyss://bucket/path' [ AS OF 'timestamp' ]
```

Restores use standard retrieval from the `DEEP_ABYSS` storage class and complete
within 12 hours. The database remains fully available during the restore. Track
progress with `SHOW BACKUPS`:

```
abyss=> SHOW BACKUPS;
NOTICE:  edition: deep-abyss
NOTICE:  backup 1: deep-abyss://vault/nightly, 23 bytes, DEEP_ABYSS, created 20:51:45, early deletion fee applies until 2027-04-18
NOTICE:  restore 1: deep-abyss://vault/nightly thawing... 41.7% (ETA 6h59m48s)
```

When the restore completes, `SHOW BACKUPS` reports the database state as verified.

### Point-in-time recovery

`AS OF` accepts `YYYY-MM-DD`, `YYYY-MM-DD HH:MM:SS` or RFC 3339 timestamps.

- The earliest recoverable point is the beginning of Unix time, **1970-01-01**.
- Future timestamps are supported.
- Backups not in the session's catalog are restored with catalog-independent
  recovery.

```
abyss=> RESTORE DATABASE FROM 'deep-abyss://vault/nightly' AS OF '1970-01-01';
NOTICE:  point-in-time recovery to 1970-01-01 (earliest recoverable point: the beginning of Unix time)
NOTICE:  restore job 1 initiated: thawing from DEEP_ABYSS (standard retrieval)
NOTICE:  ETA: 12h0m0s. Check progress with SHOW BACKUPS.
NOTICE:  the database remains fully available during the restore
RESTORE
```

With `--warp X`, the 12-hour retrieval is compressed by a factor of X.

### Pricing

| Item                     | Price                 |
|--------------------------|-----------------------|
| Storage                  | $0.00099 per GB-month |
| Minimum storage duration | 180 days              |
| Typical monthly cost     | $0.00                 |

## The Mariana Tier

Deep Abyss customers get early access to **The Mariana Tier**, our hyperscale
offering for workloads under extreme pressure. Contact sales.
