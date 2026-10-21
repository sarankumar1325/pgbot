# QueryShift customer-controlled runner

The runner executes a Verified Change Run inside the customer network. The
signed request contains the exact run, environment name, principal, expiry,
nonce, and confirmation phrase. It never contains a database URL or signing
key.

## Environment configuration

```json
{
  "version": "1.0",
  "environments": [
    {
      "name": "production",
      "tier": "production",
      "secret_ref": "env://PRODUCTION_DATABASE_URL"
    }
  ]
}
```

Validate it with:

```sh
queryshift environments config-check .queryshift/environments.json
```

Raw DSNs are rejected in this file. The runner resolves the named environment
variable only after request signature, expiry, RBAC, run integrity, policy,
rehearsal, approval, and anti-replay checks pass.

## Signed execution flow

Use independent 32-byte-or-longer secrets for runner request signing and change
approval signing:

```sh
export QUERYSHIFT_RUNNER_KEY='replace-with-a-random-secret-at-least-32-bytes'
export QUERYSHIFT_APPROVAL_KEY='replace-with-a-different-random-32-byte-secret'
export PRODUCTION_DATABASE_URL='postgresql://...'

queryshift runner request change.run.json \
  --environment production \
  --subject alice@example.com \
  --issuer https://identity.example.com \
  --role dba \
  --nonce "deploy-$(date +%s)" \
  --change-confirmation APPLY \
  -o execution.request.json

queryshift runner verify execution.request.json
queryshift runner execute execution.request.json .queryshift/environments.json \
  --confirm-request qrr_REPLACE_WITH_REQUEST_ID
```

Requests expire after five minutes by default and cannot exceed 15 minutes.
Consumed nonces are persisted under the local QueryShift state directory, so a
request cannot be replayed by starting another runner process.

## Audit export

```sh
queryshift runner audit-export change.run.json > queryshift-audit.jsonl
```

Each JSONL record includes the previous record hash. This makes editing,
removal, and reordering detectable by an audit sink or CI verifier.

## Honest deployment boundary

The repository implements the runner protocol, principal contract, RBAC,
policy gates, secret references, anti-replay, rate-limit primitive, and audit
format. An enterprise deployment still needs to validate real OIDC/SAML tokens,
distribute policy, route approvals, and ship telemetry using its chosen identity
provider and control plane. QueryShift does not claim that a local `--subject`
flag is SSO authentication.
