# ADR-077: Repo Consolidation — Single-Repo Monorepo for the Three Portfolio Bpps

**Date:** 2026-08-21
**Status:** Accepted

## Context

ADR-076 (yesterday's decision, executed today as tasks 76–78) solved code duplication across
the three sibling apps by extracting a small, verified-safe TypeScript scope into a fourth
repo, `shared-app-packages`, vendored into each app via `git subtree` at `packages/shared/`.
That is a **separate, already-settled decision** and this ADR does not revisit it — the
question here is different and one level up: the user now wants the **repos themselves**
consolidated into one git repository, so that editing shared code stops requiring an explicit
`git subtree pull` per consumer, while the three apps keep running as three fully independent
deployables (three Docker images, three Postgres databases, three `docker compose` stacks, no
domain-model merging). This was explicitly decided by the user before this ADR — the job here
is the concrete HOW, not whether.

### Verified facts (checked directly against the four working trees this session, since the
task brief's assumptions about repo state were already stale)

**Commit counts — corrected.** The task brief assumed portfolio-b has 3 commits, portfolio-c 2,
`shared-app-packages` 1. As of right now:
```
portfolio-b:    6 commits (main)     — the extra 3 are today's task-76/78 work
portfolio-c:     4 commits (master)   — the extra 2 are today's task-76/78 work
shared-app-packages:     1 commit  (master)
investing-app:         114 commits (feat/backend; main also exists, unmerged)
```

**No remote configured on three of the four repos.** `git remote -v` returns nothing for
`investing-app`, `portfolio-b`, and `portfolio-c` — only `shared-app-packages`
has a GitHub origin (`github.com/emicon123/shared-app-packages`). This simplifies both the
host-repo choice (no remote URL/history to redirect for the two repos being absorbed) and the
subtree mechanics (`git subtree add` accepts a local filesystem path as its source directly —
no `git remote add` step needed).

**Working trees are clean** in portfolio-b and portfolio-c (`git status
--porcelain` empty in both). **investing-app is not clean** — it carries the still-uncommitted
output of today's ADR-076/architecture pass (`docs/architecture/00-stack-validation.md`
modified, several new task files, ADR-076, `12-portfolio-bpps-and-shared-packages.md`
untracked). investing-app also currently has two `worktree-agent-*` branches present from past
`Agent(isolation: "worktree")` runs, of unknown live/stale status.

**investing-app is on `feat/backend`, not `main`.** Today's ADR-076 execution (tasks 76–78)
landed directly on `feat/backend` — i.e., the established *practice* on this repo (as opposed
to CLAUDE.md's stated *policy* of "feature branches" + Conventional Commits) is to do
infra/consolidation chores directly on whatever branch is currently checked out, not on a
dedicated branch merged via PR (there is no PR discipline anywhere in this project — no remote,
no CI). This ADR does not resolve which branch the actual migration commits should land on;
task 80 makes the executor confirm this with the user explicitly before task 81 runs, precisely
because it's a judgment call this document shouldn't make silently.

**A real host-cron dependency exists.** `crontab -l` for this user contains exactly one entry
referencing an app repo's absolute path:
```
0 23 * * * ~/Claude-Code/investing-app/infra/news/news-research.sh >> ~/Claude-Code/investing-app/infra/news/news-research.log 2>&1
```
(ADR-041's daily asset-news research job.) No portfolio-b/portfolio-c cron entries exist.
`systemctl list-units --all` and `ls /etc/systemd/system/` show no unit files referencing
`invest`/`portfolio-b`/`portfolio-c`. **This check is only as good as 2026-08-21** — task 80 mandates
re-running it immediately before and after the migration, since host automation outside the
four repos is invisible to any tool operating from inside them.

**Package-manager identity has no collisions.** The npm scopes are already distinct by design
(confirmed in each app's own CLAUDE.md "Package naming" section, chosen specifically to avoid
this): `@investing-app/shared` / `@investing-app/frontend-web`, `@portfolio-b/shared` /
`@portfolio-b/frontend-web`, `@portfolio-c/shared` / `@portfolio-c/frontend-web`. Merging all of
them into one npm-workspaces install has no name collisions to resolve.

**Compose identity is already fully decoupled from file-system location.** Every one of the
three compose stacks already pins its project name (`name: investing` / `name: portfolio-b` /
`name: portfolio-c`, fixed by task 76 this session), its Docker volume's physical name
(`infra_db-data`, `infra_db-portfolio-b-data`, `portfolio-c_db-portfolio-c-data` — none derived from directory
basename), and its Docker network name (`investing-shared`, `portfolio-b-net`, `portfolio-c-net`,
`portfolio-b_portfolio-b-net`, `portfolio-c_portfolio-c-net` as Compose-qualified names). **None of these identities
are derived from where the compose file physically lives on disk.** This is the single most
important safety property of this whole plan: moving `portfolio-b/infra/compose.yml`
to `investing-app/apps/portfolio-b/infra/compose.yml` changes *nothing* about the
containers, volumes, or networks it produces — the only things that can break are the Docker
**build `context:`/`dockerfile:` paths**, which is why §4 below is worked out to the line.

Live container/volume/network baseline, captured this session (task 80 re-captures this as the
pre-migration snapshot):
```
investing-nginx-1, investing-backend-1, investing-db-1
portfolio-b-frontend-portfolio-b-1, portfolio-b-backend-portfolio-b-1, portfolio-b-db-portfolio-b-1
portfolio-c-frontend-portfolio-c-1, portfolio-c-backend-portfolio-c-1, portfolio-c-db-portfolio-c-1

volumes: infra_db-data, infra_db-portfolio-b-data, portfolio-c_db-portfolio-c-data
networks: investing-shared, investing_default, portfolio-b_portfolio-b-net, portfolio-c_portfolio-c-net
```

## Decision

### 1. Host repo: investing-app confirmed — but the target layout is challenged, not accepted as-given

**investing-app is the right host**, for the reasons the task brief already gives (richest
history: 76+ ADRs, 12 architecture docs, 114 commits vs. 6/4/1 for the others; most complex
app). Not contested.

**The symmetric layout in the task brief's example — `apps/investing-app/`,
`apps/portfolio-b/`, `apps/portfolio-c/` all siblings under `apps/` — is
challenged and rejected in favor of an asymmetric layout:**

- **investing-app's own application content does not move.** `backend/`, `frontend-web/`,
  `shared-ts/`, `infra/`, `docs/`, `.claude/`, `backtest/`, `boot.sh`, and
  the root `package.json` all stay exactly where they are today, at the repo root.
- **portfolio-b and portfolio-c fold in as new subdirectories**,
  `apps/portfolio-b/` and `apps/portfolio-c/`, each preserving its own
  existing internal tree verbatim (its own `backend/`, `frontend-web/`, `shared-ts/`, `infra/`,
  `docs/`, `docs/adr/`, `CLAUDE.md`, etc.).
- **`packages/shared/{ts,ui}`** — already living at investing-app's repo root since ADR-076 —
  stays exactly there and becomes the one canonical copy for all three apps. portfolio-b's and
  portfolio-c's own `packages/shared/` (their own separate ADR-076 subtree mounts, physically
  distinct copies of the same content) are deleted as part of the fold-in — see §5.

**Why (this is the actual "validate or challenge" call):**

1. **Zero Docker/build-context risk for investing-app itself.** Because nothing physically
   moves, investing-app's own two Dockerfiles and its `infra/compose.yml` need **no changes at
   all** — not even a re-verification pass, since the build context is still exactly the repo
   root it always was. Given investing-app is the largest, most actively-developed, and most
   operationally critical of the three apps, eliminating it entirely from the blast radius of a
   structural migration is a meaningful risk reduction, not a cosmetic shortcut.
2. **Preserves the host-cron job's absolute path unmodified.** `news-research.sh`'s crontab
   entry hardcodes `~/Claude-Code/investing-app/infra/news/news-research.sh`. If
   investing-app's own `infra/` moved to `apps/investing-app/infra/`, this migration would
   *have* to include a crontab edit to avoid silently breaking the nightly news job — an edit
   this plan would have no way to verify succeeded, since crontab lives outside all four repos.
   The asymmetric layout makes that edit unnecessary rather than requiring it to be done
   correctly.
3. **Delivers the user's actual stated wins in full regardless.** "One clone" and "atomic
   shared-code edits, no subtree-pull ceremony" do not require every app to live at the same
   nesting depth — they only require one repo and one physical copy of `packages/shared/`.
   Both are satisfied by the asymmetric layout exactly as well as the symmetric one.
4. **Rejected alternative — full symmetric layout** (moving investing-app's own content into
   `apps/investing-app/` too): more uniform-looking (three apps structurally equal, easier to
   explain in one sentence), but it is pure mechanical churn bought for aesthetics — it would
   require rewriting investing-app's own compose `context:`/Dockerfile `COPY` paths (the exact
   risk this migration is trying to manage) *and* a crontab edit, for zero functional gain over
   the asymmetric layout. Rejected. If the aesthetic argument ever outweighs this in the
   future, it's a mechanically simple follow-up (`git mv` investing-app's own directories into
   `apps/investing-app/`, once the rest of this migration has already proven the pattern safe
   for portfolio-b and portfolio-c) — not something that needs to happen in the same change.

### 2. History handling: `git subtree add --squash`, one per source repo — matches the ADR-076 precedent

Fold `portfolio-b` and `portfolio-c` into investing-app the same way
`shared-app-packages` was folded into all three apps earlier today: `git subtree add
--prefix=<path> <local-path-to-source-repo> <branch> --squash`, run from investing-app's own
working directory, using each source repo's local filesystem path directly (no remote needed).

```bash
# from investing-app's repo root:
git subtree add --prefix=apps/portfolio-b ~/Claude-Code/portfolio-b main   --squash
git subtree add --prefix=apps/portfolio-c   ~/Claude-Code/portfolio-c master --squash
```

Note the branch names differ: portfolio-b's default branch is `main`, portfolio-c's is `master`.

**Why squash, not full history:**

- **Consistency with the pattern already proven today.** All three apps' existing
  `packages/shared/` mounts used `--squash` (visible in their own git logs: `"Squashed
  'packages/shared/' content from commit ecabbed"`). Using the same mechanism for the bigger
  fold-in keeps the technique uniform across this repo's history rather than mixing two
  different subtree conventions.
- **Solo maintainer, no external history consumers.** No PR ever referenced a portfolio-b or portfolio-c
  commit SHA (no remote existed for either until today, and still doesn't). Nothing on GitHub,
  in an issue tracker, or in another person's clone depends on being able to `git log` through
  portfolio-b's or portfolio-c's commit-by-commit history from inside the merged repo. For a two-person
  (you + your dad; you + another family member) side project with 4–6 commits of history each,
  preserving per-commit granularity inside the *combined* repo has no practical use that
  outweighs cluttering investing-app's otherwise-clean 114-commit timeline with two short,
  unrelated interleaved histories dated months apart from investing-app's own commits.
- **Full-history subtree-add would interleave by author date**, not by logical grouping — `git
  log` on the merged repo would show portfolio-b's June commits and portfolio-c's July commits scattered
  among investing-app's own June–August commits, which is more confusing to read later, not
  less.

**Consequence, stated plainly (this is the trade-off, not a hidden cost):** squashing means
investing-app's own history gains exactly **one** commit per source repo (e.g. `"Squashed
'apps/portfolio-b/' content from commit <sha>"`), not six or four. The original
commit-by-commit granularity is **not** preserved inside the merged repo — it remains
recoverable only from the old standalone checkouts' own `.git` directories. This is precisely
why §5 says: after cutover, stop and archive those old checkouts, do not delete them. They are
now the only remaining copy of that commit-level history, since neither repo has ever had a
remote.

### 3. Target directory layout

```
investing-app/                              (host repo — root is unchanged in identity/content
│                                             for everything already here today)
├── backend/                                 UNCHANGED — investing-app's own Spring Boot API
├── frontend-web/                            UNCHANGED — investing-app's own React SPA
├── shared-ts/                               UNCHANGED — investing-app's own shared-ts workspace
├── infra/                                   UNCHANGED — investing-app's own compose/nginx/backend Dockerfiles
├── backtest/                                UNCHANGED
├── boot.sh                                  UNCHANGED
├── docs/                                    UNCHANGED — investing-app's own architecture/ADR/OpenAPI docs
├── .claude/                                 UNCHANGED (tasks 80-84 below land here)
├── CLAUDE.md                                Edited — see §7 (identifies changes, doesn't apply them)
├── package.json                             Edited — workspaces array grows, see §4
├── package-lock.json                        Regenerated once, at the same step as package.json
│
├── packages/shared/                         UNCHANGED location; becomes canonical for all 3 apps
│   ├── ts/                                  @shared/app-core
│   └── ui/                                  @shared/ui-primitives
│
└── apps/                                    NEW top-level directory — the only new nesting
    ├── portfolio-b/                git-subtree-folded, squashed, from portfolio-b's repo
    │   ├── backend/                         unchanged internal structure
    │   ├── frontend-web/
    │   ├── shared-ts/
    │   ├── infra/
    │   │   ├── compose.yml                  EDITED — frontend-portfolio-b's context:/dockerfile:, see §4
    │   │   ├── nginx/Dockerfile             EDITED — COPY/WORKDIR paths, see §4
    │   │   └── backend/Dockerfile           UNCHANGED (content identical; only its physical path moved)
    │   ├── docs/                            portfolio-b's own architecture.md + adr/ — NOT merged with
    │   │                                    investing-app's docs/; different domain, stays separate
    │   ├── CLAUDE.md                        Edited — see §7
    │   ├── package.json                     Retired as an npm-workspaces root (its own workspaces
    │   │                                    array becomes irrelevant — the monorepo root package.json
    │   │                                    is the only workspaces root now); left in place unedited,
    │   │                                    since nothing reads it anymore once the root manifest wins
    │   ├── package-lock.json                DELETED — superseded by the one root lockfile, see §4
    │   └── (packages/shared/ — DELETED, see §5; was a duplicate of the root copy)
    │
    └── portfolio-c/                 same pattern as portfolio-b above
        ├── backend/, frontend-web/, shared-ts/, infra/, docs/, CLAUDE.md, ...
        └── (packages/shared/ — DELETED, see §5)
```

### 4. Docker build-context changes — all 6 Dockerfiles, exact before/after

**investing-app's own two Dockerfiles: no change, either file.**
`infra/backend/Dockerfile` (context `../backend`) and `infra/nginx/Dockerfile` (context `..`,
i.e. repo root) are both untouched — their build context is, and remains, investing-app's own
repo root, because nothing under it moved.

**portfolio-b's and portfolio-c's `infra/backend/Dockerfile`: no content change, either file.** The
Compose `context: ../backend` / `dockerfile: ../infra/backend/Dockerfile` pair is a *relative*
expression anchored to the compose file's own directory. Since `backend/` and `infra/` move
together as one unit (both end up under `apps/portfolio-b/`), the relative path still
resolves correctly with zero edits:
```yaml
# apps/portfolio-b/infra/compose.yml — backend-portfolio-b service — UNCHANGED
build:
  context: ../backend          # resolves to apps/portfolio-b/backend — correct, untouched
  dockerfile: ../infra/backend/Dockerfile
```
(Same for portfolio-c.) This is the general rule this whole plan leans on: **anything expressed as a
relative path between two locations that move together as a unit needs no edit.** Only paths
that must reach *outside* the moved subtree (the frontend build, which needs the monorepo
root's `package.json` and `packages/shared/`) need rewriting.

**portfolio-b's and portfolio-c's `infra/nginx/Dockerfile` + the `frontend-*` service's compose context:
these are the real edits.**

`apps/portfolio-b/infra/compose.yml`, `frontend-portfolio-b` service:
```yaml
# BEFORE (today, standalone repo — context is portfolio-b's own repo root):
frontend-portfolio-b:
  build:
    context: ..                                   # portfolio-b/ (its own root)
    dockerfile: infra/nginx/Dockerfile

# AFTER (folded in — context must reach the monorepo root, 3 levels up from
# apps/portfolio-b/infra/):
frontend-portfolio-b:
  build:
    context: ../../..                             # investing-app/ (monorepo root)
    dockerfile: apps/portfolio-b/infra/nginx/Dockerfile
```
(`../../..` from `apps/portfolio-b/infra/`: `..` → `apps/portfolio-b/`,
`../..` → `apps/`, `../../..` → repo root. `dockerfile:` is resolved relative to the *new*
context root, hence the full `apps/portfolio-b/...` prefix.)

`apps/portfolio-b/infra/nginx/Dockerfile`:
```dockerfile
# BEFORE (context = portfolio-b's own root):
COPY package.json package-lock.json* ./
COPY packages/shared/ts/package.json ./packages/shared/ts/
COPY packages/shared/ui/package.json ./packages/shared/ui/
COPY shared-ts/package.json ./shared-ts/
COPY frontend-web/package.json ./frontend-web/

RUN npm install

COPY packages/shared/ ./packages/shared/
COPY shared-ts/ ./shared-ts/
COPY frontend-web/ ./frontend-web/

WORKDIR /app/frontend-web
RUN npm run build

# AFTER (context = monorepo root):
COPY package.json package-lock.json* ./
COPY packages/shared/ts/package.json ./packages/shared/ts/
COPY packages/shared/ui/package.json ./packages/shared/ui/
COPY apps/portfolio-b/shared-ts/package.json ./apps/portfolio-b/shared-ts/
COPY apps/portfolio-b/frontend-web/package.json ./apps/portfolio-b/frontend-web/

RUN npm install

COPY packages/shared/ ./packages/shared/
COPY apps/portfolio-b/shared-ts/ ./apps/portfolio-b/shared-ts/
COPY apps/portfolio-b/frontend-web/ ./apps/portfolio-b/frontend-web/

WORKDIR /app/apps/portfolio-b/frontend-web
RUN npm run build
```
Notice the three `packages/shared/...` lines are **textually identical before and after** —
`packages/shared/` was already at the build-context root in the old standalone repo (it was
portfolio-b's own ADR-076 subtree mount) and stays at the build-context root in the new monorepo
(now the canonical shared copy). Only the lines touching portfolio-b's own `shared-ts/` and
`frontend-web/` — the app code that physically moved — need the `apps/portfolio-b/`
prefix added, plus the final `WORKDIR`.

**Portfolio C's `infra/compose.yml` + `infra/nginx/Dockerfile`: identical pattern**, substitute
`apps/portfolio-c/` for `apps/portfolio-b/` throughout.

**Root `package.json` — the change that makes the above installable.** The monorepo's one root
`package.json` must declare portfolio-b's and portfolio-c's frontend/shared-ts as workspace members using
**glob patterns**, not explicit paths:
```json
{
  "workspaces": [
    "packages/shared/ts",
    "packages/shared/ui",
    "shared-ts",
    "frontend-web",
    "apps/*/shared-ts",
    "apps/*/frontend-web"
  ]
}
```
This matters mechanically: each app's Docker build only ever `COPY`s its *own* `apps/<name>/`
subtree into the image — investing-app's own nginx build never copies `apps/` at all, and
portfolio-b's/portfolio-c's own nginx builds only copy their own one `apps/<name>/` directory, never each
other's. If the root `package.json` declared portfolio-b's and portfolio-c's workspace paths **explicitly**
(`"apps/portfolio-b/frontend-web"` as a literal string), `npm install` would fail
inside e.g. investing-app's own Docker build with a missing-workspace error, since that exact
path was never copied into that image. **Glob patterns don't have this problem** — npm resolves
`apps/*/frontend-web` against whatever is actually present on disk at install time; if `apps/`
doesn't exist in that particular build's context at all (investing-app's own build), or only
one of its two children does (portfolio-b's build only ever contains
`apps/portfolio-b/...`), the glob simply matches what's there and installs cleanly.
**This assumption needs verification at execution time, not just this ADR's confidence in it**
— task 82 mandates a real `npm install` + `npm ls --workspaces` at the new root, plus a real
Docker build of all three nginx images, before this is trusted.

One further consequence of unifying the root manifest: there is now **exactly one root
`package-lock.json`** for the whole monorepo. portfolio-b's and portfolio-c's own separate
`package-lock.json` files (each ~190 KB, generated against their own now-retired workspace
root) become dead weight once folded in and are deleted in task 82, after the new root lockfile
regenerates cleanly and each app's frontend build is verified against it.

### 5. Disposition of `shared-app-packages` and the existing `git subtree` mounts

- **`shared-app-packages` (the GitHub repo): archived, not deleted.** It is the one repo among
  the four that already has real hosted history on GitHub — zero cost to keep, real rollback
  value if this migration ever needs to be unwound. Mark it archived on GitHub (or simply stop
  touching it) rather than deleting it. **No further `git subtree pull` from it into
  investing-app after this migration lands** — `packages/shared/` is edited in place inside the
  monorepo going forward; that's the entire point of this migration (killing the pull
  ceremony). A short note is added to its README stating it's superseded by
  `investing-app/packages/shared/` as of this ADR, for anyone (future you) who finds the repo
  later and wonders why it's frozen.
- **investing-app's own `packages/shared/` subtree-mount lineage: untouched.** This migration
  doesn't re-do or re-verify it — it's already the canonical copy per §1, and ADR-076 already
  proved it works.
- **portfolio-b's and portfolio-c's own `packages/shared/` subtree mounts: deleted in the same migration,
  not a transitional period.** The moment `git subtree add --prefix=apps/portfolio-b
  ...` lands (bringing portfolio-b's *entire* tracked tree along, including its own separate
  `packages/shared/` copy as `apps/portfolio-b/packages/shared/`), the very next
  commit in the same task removes it: `git rm -r apps/portfolio-b/packages/shared`.
  There is no scenario where keeping three physical copies of `packages/shared/` alongside one
  canonical root copy is useful — it immediately reintroduces the exact drift risk ADR-076 was
  written to eliminate, so there's no reason to delay the deletion past the same task that
  creates the duplicate.

### 6. Host deploy-path impact

- **investing-app's own runbook is unchanged.** `cd infra && docker compose build backend &&
  docker compose up -d backend` (and the nginx equivalent) — identical commands, because
  nothing about investing-app's own `infra/` moved.
- **portfolio-b's and portfolio-c's runbooks get exactly one path segment deeper**, mechanically trivial
  but real: `cd portfolio-b/infra && ...` becomes `cd
  investing-app/apps/portfolio-b/infra && ...` (or, run from inside investing-app's
  own checkout, `cd apps/portfolio-b/infra && ...`). Every doc that states the old
  path is now wrong — enumerated in §7.
- **Recommendation: keep each app's own `infra/compose.yml`, do not hoist to one root
  `infra/`.** A root `infra/` with multiple compose files or Compose profiles unifying all
  three stacks would be a materially bigger, riskier rewrite (re-deriving port/network
  assignments, merging three already-distinct nginx configs under shared profiles) for zero
  functional gain — the user explicitly wants the three stacks to stay independently
  deployable, and they already are, via three fully self-contained compose files with distinct
  project names, volumes, and networks (§ Context). Per the brief's own bias toward "the
  smallest, least risky change to the actual deploy commands," leaving each app's `infra/`
  exactly where it structurally is (just one directory level deeper for the two that moved) is
  the right call.
- **Host-cron/systemd visibility gap — flagged explicitly, not resolved by this ADR.** Only the
  one crontab entry found this session (`news-research.sh`) is known; this ADR has no
  visibility into anything that might reference `portfolio-b` or
  `portfolio-c`'s absolute paths outside the four repos (a systemd timer added later, a
  cron entry added by a different session, a shell alias in `.bashrc`, an entry in some other
  tool's config). **Task 80 mandates re-running `crontab -l`, `systemctl list-units --all`, and
  a `grep -r` for each repo's absolute path across `/etc` and the user's home directory
  dotfiles, both immediately before task 81 starts and again immediately after task 83's
  cutover completes** — a "before" check alone doesn't catch something added mid-migration, and
  an "after" check alone doesn't establish a clean baseline.

### 7. CLAUDE.md updates needed (identified here; execution is out of scope for this ADR)

Per this session's operating rules, an architect agent does not edit `CLAUDE.md` on another
agent's say-so — these are the *exact* diffs needed, for whichever agent (DevOps, matching the
existing deploy-runbook ownership) applies them with the user's own direct confirmation, in
task 84:

- **`investing-app/CLAUDE.md`** (root):
  - "Project structure" section currently reads `Monorepo: backend/, frontend-web/,
    shared-ts/, infra/, docs/.` — already stale today (missing `packages/shared/`,
    `backtest/`) and becomes more wrong after this migration. Replace with
    a description of the asymmetric layout: investing-app's own app at repo root, plus
    `apps/portfolio-b/` and `apps/portfolio-c/` as independently-deployed
    sibling apps, plus shared `packages/shared/{ts,ui}/` — with a pointer to
    `docs/architecture/12-portfolio-bpps-and-shared-packages.md` for the full picture.
  - Add an explicit pointer that `apps/portfolio-b/CLAUDE.md` and
    `apps/portfolio-c/CLAUDE.md` are the authoritative instructions files for their own
    trees — this root file's domain-specific sections (portfolio strategies, rebalancing rules,
    the agent-orchestration table) describe investing-app itself, not its two siblings.
  - Drive-by fix while the file is open anyway (unrelated to this migration, but free): the
    "Git" section still says "Repository is not initialized yet" — stale, 114 commits exist.
  - Rebuild-commands section: **no change** — paths are unaffected.
- **`apps/portfolio-b/CLAUDE.md`** (moved verbatim from portfolio-b's own repo root):
  "Rebuilding" section's `cd portfolio-b/infra` → `cd
  apps/portfolio-b/infra` (2 occurrences); "Project structure" tree's implicit root
  label updated to note its new nested location.
- **`apps/portfolio-c/CLAUDE.md`**: same pattern, `cd portfolio-c/infra` → `cd
  apps/portfolio-c/infra`.

### 8. Sequencing — tasks 80–84, never break a live deployment

See `.claude/tasks/80-*.task.md` through `84-*.task.md` for the full executable breakdown.
Summary:

1. **Task 80 (DevOps, read-only):** pre-flight audit — fresh host-cron/systemd check, confirm
   working-tree cleanliness and branch choice with the user, snapshot current
   container/volume/network state as the rollback baseline. **HUMAN CONFIRMATION GATE** before
   task 81 (which branch to run this on; go-ahead for the first `git subtree add`).
2. **Task 81 (DevOps):** `git subtree add --squash` portfolio-b, then portfolio-c, one at a time into
   investing-app as `apps/<name>/`; immediately strip each one's duplicate `packages/shared/`;
   manually `cp` each app's untracked `infra/.env` (subtree add cannot bring gitignored files
   along — they were never tracked in the source repo either) into its new location. No Docker
   rebuild yet.
3. **Task 82 (Frontend-web, DevOps assists):** unify the root `package.json` (glob workspace
   entries), rewrite portfolio-b's and portfolio-c's `infra/compose.yml` + `infra/nginx/Dockerfile` per §4,
   regenerate the single root `package-lock.json`, delete the two now-dead per-app lockfiles.
   Verify via `npm install`/`npm ls --workspaces` and `docker compose config` (context/dockerfile
   resolution only, no build yet) for all three apps, including investing-app itself as a
   regression check. **HUMAN CONFIRMATION GATE** before task 83 (the first real rebuild that
   touches live containers).
4. **Task 83 (DevOps):** the cutover — stop each old standalone stack, bring up the new nested
   one, full verification (curl through the real nginx proxy paths, DB row-count comparison
   against task 80's baseline) — portfolio-b first, then portfolio-c, then rebuild investing-app's own
   nginx (root `package.json` changed) and re-verify it too. **This is the one step with
   unavoidable, brief downtime** for portfolio-b's and portfolio-c's apps (their compose project name,
   container names, and network aliases are identical between the old and new location, so both
   copies cannot run simultaneously — the old stack must come down before the new one comes up).
   investing-app's own stack has no such downtime beyond its normal rebuild-and-redeploy churn.
5. **Task 84 (DevOps + Architect):** apply the exact CLAUDE.md diffs from §7 (with the user's
   direct confirmation, per the rule that no agent edits CLAUDE.md on another agent's
   instruction); archive `shared-app-packages` on GitHub; stop (not delete) the old standalone
   `portfolio-b`/`portfolio-c` checkouts and move them out of the active
   `Claude-Code/` working set (e.g. rename with an `archive-` prefix or relocate) — kept
   indefinitely, since they are now the only remaining copy of pre-squash commit history;
   fresh host-cron/systemd check per §6; flip
   `docs/architecture/12-portfolio-bpps-and-shared-packages.md`'s status from "planned" to "done."

## Consequences

- investing-app becomes a true single-clone entry point for all three apps plus their shared
  code — `git clone` once, everything is there, no `git subtree pull` ceremony for any of the
  three going forward.
- The asymmetric layout means the repo is not perfectly uniform (investing-app's own app lives
  at the root, its two siblings live one level deeper under `apps/`) — a reader unfamiliar with
  the history might reasonably ask "why isn't investing-app under `apps/` too?" The answer
  (§1) is recorded here and should be pointed to from
  `docs/architecture/12-portfolio-bpps-and-shared-packages.md` rather than re-derived.
  Actually — per this project's own documentation convention, that answer should live in the
  architecture doc as current fact ("investing-app's own app lives at the repo root because...")
  rather than requiring a reader to open this ADR; this ADR's job is done once that doc reflects
  it.
- Squashed history means portfolio-b's and portfolio-c's pre-fold-in commit-by-commit history is not
  browsable from inside the merged repo — recoverable only from the old, now-archived standalone
  checkouts. This is an accepted, deliberate trade for a solo-maintainer project with no
  external history consumers.
- The three apps remain exactly as independently deployable as they are today — three Docker
  images, three databases, three compose stacks, three project names/volumes/networks, all
  unchanged in identity. This migration only ever touches *where the source files live on disk
  and where their build reads from*, never *what gets deployed or how it's identified at
  runtime*.
- `shared-app-packages` stops being an active dependency of any of the three apps but remains
  archived on GitHub rather than deleted, at zero ongoing cost, as a rollback path.
- A real, brief downtime window is required for portfolio-b's and portfolio-c's apps during cutover
  (task 83) — investing-app's own app has none. This should be scheduled at the user's
  convenience, not treated as risk-free just because the rest of the plan is low-risk.
