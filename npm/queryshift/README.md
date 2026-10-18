# QueryShift CLI

**Read-only PostgreSQL evidence for autonomous SQL modernization.** The static
binary exposes schema, plans, health, indexes, and workload findings without
changing the target database.

```bash
npx @queryshift/cli inspect "postgres://queryshift_ro@host:5432/db"
```

No prior install. `npx` downloads the wrapper and the prebuilt binary for your
platform (shipped as an `optionalDependency`, so npm installs only the one that
matches your OS/CPU) and runs it. Point it at your database with an argument or
`$DATABASE_URL`; use a role holding `pg_monitor` with no write grants.

## npm, pnpm, Yarn, bun

All four work, and each downloads only your platform's binary:

| | no install | project | global |
|---|---|---|---|
| npm | `npx @queryshift/cli` | `npm i @queryshift/cli` | `npm i -g @queryshift/cli` |
| pnpm | `pnpm dlx @queryshift/cli` | `pnpm add @queryshift/cli` | `pnpm add -g @queryshift/cli` |
| Yarn | `yarn dlx @queryshift/cli` | `yarn add @queryshift/cli` | `yarn global add @queryshift/cli` |
| bun | `bunx @queryshift/cli` | `bun add @queryshift/cli` | `bun add -g @queryshift/cli` |

The wrapper locates the binary at run time and this package ships **no install
scripts**, so it works wherever lifecycle scripts are disabled — `npm ci
--ignore-scripts`, pnpm's blocked-by-default builds, bun. Two Yarn notes:
`yarn dlx` is Berry-only (Classic uses `yarn global add`), and under Berry's
default Plug'n'Play linker there is no `node_modules/.bin`, so invoke it as
`yarn queryshift`.

The package is **scoped** — the bare name `queryshift` is blocked by npm's
package-name-similarity rule, so `npx queryshift` returns `E404`. Use `@queryshift/cli`.

## What npm verifies (and what it doesn't)

The npm packages carry registry integrity hashes and npm **provenance** — a
verifiable link to the GitHub Actions workflow that built them. That is a
*different and weaker* guarantee than the release's **cosign** signature: it
attests where the package came from, not that the artifact was signed.

For the verified path, install the binary with the script and require the
signature:

```bash
QUERYSHIFT_REQUIRE_SIGNATURE=1 curl -fsSL https://raw.githubusercontent.com/sarankumar1325/queryshift/main/install.sh | sh
```

Full documentation: <https://github.com/sarankumar1325/queryshift#readme>
