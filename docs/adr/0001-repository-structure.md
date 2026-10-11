# ADR-0001: Repository Structure — Single Polyglot Monorepo, Taskfile-Orchestrated

- **Status:** Accepted
- **Date:** 2026-10-12
- **Deciders:** Mandate core team
- **Context docs:** [Architecture Blueprint](../architecture/BLUEPRINT.md)

## Context

- **Stack:** a Go backend (`mandated`, a modular monolith — Blueprint D3) and one TypeScript frontend (`web/console`, Next.js 16 + AG Studio).
- **Team:** small hackathon team; everyone touches both sides near the deadline.
- **Hard requirements from the rules:** a public repository with an open-source license, and run instructions complete enough for judges to build it themselves.
- **The bug class we pitch against is contract drift.** `paypal/paypal-mcp-server#7` is a published schema that disagrees with the server's own validator. Our repo layout must make that class of bug impossible for *our* API.

## Options considered

| Option | Summary |
|---|---|
| **A. Multi-repo** (kernel, console, sdk, infra) | Independent repos and deploys |
| **B. Monorepo with Turborepo + pnpm workspaces** | JS task graph and remote caching drive everything, with Go tasks wrapped in `package.json` scripts |
| **C. Monorepo with Taskfile + Go module + pnpm (single app)** | Each toolchain uses its native build system; a thin top-level task runner ties them together |

## Evaluation

| Criterion | A. Multi-repo | B. Turborepo | C. Taskfile |
|---|---|---|---|
| Contract drift between Go server and TS client | High risk: the spec is copied across repos | Low | **Low**: one `api/openapi.yaml`, codegen for both sides, CI diff gate |
| Atomic cross-stack changes (API + UI in one PR) | No | Yes | **Yes** |
| Fit with Go tooling | Native | Awkward: Turbo's hashing doesn't understand Go's module graph, so Go needs wrapper scripts | **Native** (`go build` cache, `go test -race`) |
| Value of Turbo's caching | — | Low: only one JS package | — |
| Judge onboarding ("clone → run") | Several clones and versions to align | One clone, two toolchains plus Turbo | **One clone; `task dev`** |
| Render deploy | One Blueprint per repo | One `render.yaml` | **One `render.yaml`** (`rootDir` per service) |
| New tools to learn | — | Turbo | Task (one YAML file) |

## Decision

**Option C.** One repository, `mandate-os`:

- **One Go module** at the root, with no `go.work`, because a single module is enough while the backend is one binary.
- **One pnpm package** at `web/console`. There is no workspace yet; we add `pnpm-workspace.yaml` only if a second TS package appears.
- **[Task](https://taskfile.dev)** as the single entry point for developers, CI, and judges.
- **OpenAPI 3.1 as the single source of truth** for the HTTP API.

### Layout

```text
mandate-os/
├── api/
│   └── openapi.yaml               # source of truth (HTTP API + SSE event schemas)
├── cmd/
│   ├── mandated/                  # single binary: --api --workers --suppliers
│   └── mandatectl/                # CLI: mandates, scenario replay, offline audit verify
├── internal/
│   ├── kernel/{policy,ledger,token,idem,fsm,audit}/
│   ├── paypal/                    # hand-written REST client; only holder of the secret
│   ├── mesh/  procure/  checkout/  agent/  mcp/  notify/  jobs/
│   └── store/
│       ├── migrations/            # SQL migrations (invariants as CHECK/UNIQUE/triggers)
│       └── queries/               # sqlc input
├── gen/                           # generated Go (oapi-codegen, sqlc) — committed
├── web/
│   └── console/                   # Next.js 16 + AG Studio; generated TS types in src/gen/
├── postman/
│   └── mandate.postman_collection.json   # generated from api/openapi.yaml
├── deploy/
│   └── render.yaml
├── docs/{architecture,adr}/
├── research/
├── Taskfile.yml
├── .github/workflows/ci.yml
└── LICENSE                        # MIT (already present)
```

### Codegen and the drift gate

| Generator | Output |
|---|---|
| `oapi-codegen` | `gen/api` (Go server interfaces and types) |
| `openapi-typescript` | `web/console/src/gen/api.d.ts` |
| `sqlc` | `gen/db` |
| Postman import | `postman/` |

**CI rule:** `task gen && git diff --exit-code`. If generated code is out of date, the build fails. This is our structural answer to `paypal-mcp-server#7`, and it's worth one sentence in the video.

Generated code is committed, so judges can build without installing the generators.

### Task entry points

| Task | Does |
|---|---|
| `task dev` | Postgres (docker compose), `mandated` with hot reload, and `next dev` together |
| `task gen` | All code generators |
| `task test` | `go test -race ./...`, invariant property tests, `pnpm test` |
| `task lint` | `golangci-lint` (with `depguard`: only `internal/paypal` may read PayPal credentials) and `eslint` |
| `task demo:seed` | Create sandbox mandate, supplier agents, and stock levels |
| `task demo:reset` | Reset the demo state |
| `task verify-audit` | `mandatectl verify --from 0` |

### CI

GitHub Actions on every PR:
- path filters per toolchain (`**/*.go`, `web/**`, `api/**`)
- `gen` drift gate
- tests, with Postgres as a service container
- static console build

## Consequences

**Positive**
- One PR can change the API contract, the server, and the console together, and drift is caught mechanically.
- Judges get one clone and one command. The README documents `task dev` plus sandbox credential setup.
- No tool fights Go's native build cache.

**Negative / accepted risks**
- Two toolchains (Go 1.23+ and Node 22 + pnpm) must be installed. Mitigation: `task doctor` checks versions, and a devcontainer is provided.
- Taskfile doesn't do remote caching. Not needed at our scale.

**Revisit triggers**
- A second TS package (for example a published TS SDK): introduce `pnpm-workspace.yaml`. Turborepo still isn't needed unless there are 3 or more TS packages.
- The kernel splits into its own service: switch to `go.work` with separate modules. This is a deployment change only; the package boundaries already exist.
