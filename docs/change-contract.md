# QueryShift Change Contract v1

The Change Contract is the review boundary between proposed SQL and database
execution. QueryShift creates one contract for one statement against one
verified target and one structural schema snapshot.

## Guarantees

- The exact SQL is bound by SHA-256.
- The target is represented by a non-secret database fingerprint.
- Structural schema drift invalidates the contract before a write connection
  opens. Changing database size or display metadata does not.
- Deterministic safety classification, risk, recovery requirements, affected
  objects, foreign-key dependencies, and verification probes travel together.
- `WRITE` unlock and the exact risk-specific confirmation phrase remain host
  enforced. A contract cannot weaken them.
- The full contract is stored with mode `0600` beside the SQL migration and is
  included in the local integrity manifest.
- Execution re-runs statement validation and safety classification. Model output
  is never accepted as the security boundary.

The versioned machine-readable schema is
[`schema/queryshift-change-contract-1.0.json`](../schema/queryshift-change-contract-1.0.json).

## Execution sequence

```text
reviewed SQL
  -> deterministic analysis
  -> Change Contract + contract digest
  -> explicit approval phrase
  -> reload target schema
  -> validate target + schema + contract + SQL
  -> write contract/migration/snapshot artifacts
  -> one short transaction
  -> execution receipt + post-change schema snapshot
```

Any mismatch stops the sequence and requires a fresh review.
