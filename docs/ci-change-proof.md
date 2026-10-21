# Change proof in pull requests and CI

QueryShift's `change` commands are read-only. They generate and verify the same
Change Contract enforced by the interactive workspace; they never apply SQL.

## Generate the PR artifact

Run against a reviewed CI or staging database after the migration has been
prepared but before it is deployed:

```sh
export DATABASE_URL="postgres://queryshift_ro:…@ci-db/app?sslmode=require"
queryshift change plan migrations/20261020_add_orders_index.sql \
  --output artifacts/orders-index.contract.json
```

The contract contains no connection string or host. It records a non-secret
target fingerprint, structural schema hash, exact SQL hash, deterministic risk,
affected objects, dependencies, recovery requirements, evidence hashes, and
required verification probes.

Upload `artifacts/*.contract.json` as a protected CI artifact or attach it to
the pull request review. The schema is
[`queryshift-change-contract-1.0.json`](../schema/queryshift-change-contract-1.0.json).

## Verify before deployment

```sh
export DATABASE_URL="postgres://queryshift_ro:…@target-db/app?sslmode=require"
queryshift change verify artifacts/orders-index.contract.json
```

Successful verification prints one JSON receipt. The command exits non-zero if:

- the contract or SQL was modified;
- the target fingerprint differs;
- structural schema drift occurred after review;
- the deterministic safety classification no longer matches;
- the contract tries to weaken the WRITE unlock.

Database-size changes and other telemetry do not invalidate the structural
schema fingerprint.

## GitHub Actions outline

```yaml
name: database-change-proof
on: [pull_request]

jobs:
  contract:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build QueryShift
        run: go build -o ./queryshift ./cmd/queryshift
      - name: Generate immutable change contract
        env:
          DATABASE_URL: ${{ secrets.QUERYSHIFT_CI_DATABASE_URL }}
        run: ./queryshift change plan migrations/change.sql -o change.contract.json
      - uses: actions/upload-artifact@v4
        with:
          name: database-change-contract
          path: change.contract.json
```

Use a read-only role for planning and verification. Applying a change remains a
separate, explicitly approved operation in the QueryShift workspace.
