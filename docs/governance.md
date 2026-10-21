# Local enterprise governance foundation

QueryShift evaluates every Change Contract with deterministic policy code. A
policy can cap risk, deny statement kinds, require roles by risk, enforce
separation of duties, and set approval expiry.

High-risk approvals are separate artifacts bound to both the contract SHA-256
and policy SHA-256. They are HMAC-SHA256 signed with a customer-controlled key,
expire automatically, and become invalid if the contract, policy, approver, or
roles are modified.

The built-in default policy requires `db_owner` approval for high risk and `dba`
approval for critical risk, with separation of proposer and approver and a
one-hour expiry. The core is local-first: database credentials, signing keys,
and approval artifacts do not need to leave the customer environment.

This package is the enforcement foundation for a future SSO-backed team control
plane; SSO/SAML, remote key management, and hosted approval routing are not
claimed by the local v1 implementation.

```sh
queryshift change policy-check change.contract.json

export QUERYSHIFT_APPROVAL_KEY="a-customer-controlled-key-at-least-32-bytes"
queryshift change approve change.contract.json --approver bob --role dba \
  -o change.approval.json
queryshift change approval-verify change.contract.json change.approval.json
```
