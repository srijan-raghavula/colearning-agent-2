# BUILD_PROGRESS — EDUCORE (Go edition)

Status: **Phase 5 complete (interfaces, dashboards, analytics, visual transformation). Delivery phase (README, cleanup, final verification) is next.**

## Locked stack

| Layer | Decision |
|---|---|
| Language | Go (installed: 1.27.3) |
| HTTP | `net/http` stdlib |
| Templates | `html/template` server-rendered |
| CSS | Custom stylesheet, static assets |
| JS | Minimal vanilla, progressive enhancement only |
| DB | SQLite file `./data/educore.db` |
| Driver | `modernc.org/sqlite` v1.61.1 (CGO-free; already in module cache) |
| Sessions | Server-side sessions in SQLite, HttpOnly cookie |
| Passwords | bcrypt (`golang.org/x/crypto/bcrypt`) |
| Charts | Server-generated SVG |
| AI | OpenAI-compatible API via stdlib `net/http` + `encoding/json` |
| Files | `./data/uploads/`, size/MIME/extension validated |
| Icons | Lucide inline SVG partials |

Default env: `APP_ADDR`, `APP_DB_PATH`, `AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`.

## Environment verified

- Go 1.27.3 installed at `/usr/bin/go`.
- sqlite3 CLI 3.54.1, gcc 16.2.1, pkg-config present (not required by chosen driver).
- Disk: 68 GB free on the project filesystem.
- Repository contained only `requirement.md` — nothing to preserve.
- Network/proxy for Go modules: `https://proxy.golang.org,direct`; all selected deps resolvable (driver already cached).

## Stages

1. [x] Phase 1 — Repo inspection, environment check, architecture lock, BUILD_PROGRESS.
2. [x] requirement.md rewritten: tech-stack + architecture sections, run commands, validation/adaptation notes. All functional/design requirements untouched.
3. [x] Phase 2 — Foundation (verified by tests below):
   - Config loader (`APP_ADDR`, `APP_DB_PATH`, `AI_*`; defaults 127.0.0.1:8080 / ./data/educore.db).
   - SQLite via modernc (CGO-free): WAL, foreign keys ON, busy_timeout 5000, single connection, transactional idempotent migrations (`migrations/001_init.sql`).
   - Schema: users, courses, enrollments, assignments, rubric_criteria, submissions, evaluations, criterion_results, sessions, schema_migrations, db_meta — FKs, indexes, uniqueness, CHECK constraints, extensible for Phases 3+.
   - Auth: bcrypt hashes, 256-bit session tokens stored as SHA-256 token hash, expiry + lazy purge + background purge timer, per-user session rotation on login, HttpOnly SameSite=Lax cookie (Secure under TLS), CSRF (session-bound tokens for authenticated forms; cookie-salted anonymous tokens for login), faculty/student/any-auth middlewares, role from server data never client input.
   - Seed: idempotent demo accounts (faculty@educore.demo / Faculty123!, student@educore.demo / Student123!); existing passwords never overwritten; runs via `educore seed` or auto on a fresh DB.
   - UI: login page styled per spec §9 (custom CSS, no gradients, responsive, error/notice states), faculty + student placeholder homes with sidebar layout and working sign-out.
4. [x] Phase 3 — Core workflows (verified by tests + live smoke below):
   - **Courses**: create/edit/detail/delete-safe flows, per-faculty ownership enforced in the store, roster + assignment lists, aggregate counts.
   - **Students**: faculty-only student account creation (bcrypt, unique email, password policy), enroll by dropdown or email, duplicate/unenroll handling.
   - **Assignments**: draft/published lifecycle, edit-locked when published, dynamic due-date handling (datetime-local input, stored UTC, server-side formatting), assignment list with draft/published filters.
   - **Rubric**: add/edit/replace criteria in a DB transaction (drafts only), name/description/positive-points validation, running total display, publication gated on exactly 100 points.
   - **Student surface**: My Courses, per-course published assignment list, My Assignments with All/Pending/Submitted/Overdue filters, assignment detail with instructions/rubric/submission form/status.
   - **Submissions**: text-only, file-only, and combined submissions; resubmission replaces the record and keeps the latest timestamp; late indicator computed from the deadline (never persisted as "OVERDUE"); served through authenticated, ownership-checked handlers.
   - **Uploads**: 10 MB cap, extension + real content-type sniffing, random 32-hex server filenames, path-traversal-proof open helper, storage under ./data/uploads/.
   - **Text extraction**: TXT (UTF-8), DOCX (archive/zip + encoding/xml), PDF (github.com/ledongthuc/pdf, pure Go). Failures are recorded as `EXTRACTION_FAILED: <reason>` instead of being faked.
   - **Faculty monitoring**: per-assignment enrolled/submitted/not-submitted/overdue counts + completion %, roster table with status, received time, late badge, inspect + download actions.
   - migration `migrations/002_phase3.sql`: submissions.extracted_text + performance indexes; Ø drop of existing tables.
 5. [x] Phase 4 — Evaluation engine (verified below):
   - **Architecture**: independent `internal/evaluator` package (types, strict validation, evidence checking, OpenAI-compatible provider, deterministic DEMO, engine). No evaluation logic embedded in templates or handlers; handlers only orchestrate store calls.
   - **Input**: built fresh per attempt from the latest submission; `submissions.extracted_text` is the primary source (falls back to the plain text field only when extraction failed — flagged for review, never silently grades an empty or failed-extraction document).
   - **Output**: strictly validated typed result. Every criterion exactly once; unknown/duplicate/missing IDs rejected; 0 <= points <= max (clamps recorded as warnings); total = server-computed sum of validated points (never the model's own); 2dp rounding; total capped at 100.
   - **Evidence**: every quotation verified against the submission via normalized-whitespace substring matching (which cannot invent words). Unverifiable quotes dropped, counted, and warned; substantial evidence problems force review. DEMO quotes are verbatim submission substrings.
   - **Confidence**: HIGH/MEDIUM/LOW with a reason; LOW means faculty review is required.
   - **Provider**: stdlib `net/http` OpenAI-compatible chat-completions adapter (env `AI_BASE_URL` / `AI_API_KEY` / `AI_MODEL`). The key is sent only in the server-side Authorization header (never logged, never rendered); 60s client timeout, 1MiB response cap; HTTP errors, invalid JSON and empty choices handled; at most one retry for malformed/provider output then a FAILED state with no silent DEMO fallback. Student content is framed as untrusted data with explicit injection-resistance instructions; temperature 0.2.
   - **DEMO mode** (default without credentials): deterministic rubric-keyword matching with proportional clamped scores and matched/unmatched keyword feedback; criteria without keywords use a documented conservative floor (40% topic match / 20% substantive answer / 0). Always labeled DEMO, LOW confidence, faculty review required before publication.
   - **Queue**: additive migration `003_evaluations.sql` — durable `evaluation_jobs` table (PENDING/RUNNING/COMPLETED/FAILED/SUPERSEDED), transactional claim (no double processing), `submissions.eval_status` lifecycle (NOT_REQUESTED/PENDING/RUNNING/NEEDS_REVIEW/APPROVED/FAILED) distinct from submission status and approval state; single background worker (poll + submit-triggered wake); RUNNING jobs recovered to PENDING on restart; resubmission supersedes stale jobs; attempts accumulate for auditing.
   - **Faculty review**: grading workspace page — answer + extracted text on the left; rubric workspace on the right (model vs effective points, validated overrides with notes, evidence + explanations, rejected-quote counters, warnings box, overall feedback / strengths / gaps / suggestions, confidence rationale, method and attempt). Approve & publish (server-calculated total, approver and timestamp recorded), unpublish, re-evaluate, plus a MANUAL grading form.
   - **Overrides / manual**: `criteria_overrides` audit table keeps original model rows intact (faculty name + timestamp recorded); MANUAL attempts pass the same strict validation (missing/duplicate/out-of-range rejected) and stay provisional until approved.
   - **Student surface**: evaluation status line (Submitted / Evaluation in progress / Awaiting faculty review / Evaluation failed / Results published) plus a published-results panel (criterion scores including faculty adjustments, evidence, feedback, strengths, gaps, next steps) shown only for APPROVED evaluations; provisional scores, provider details and internal errors never reach students.

6. [x] Phase 5 — Interfaces: faculty + student dashboards, analytics views, SVG charts, full design spec, responsive layout, empty/error states (see the Phase 5 section below for the full verification record).
7. [ ] Phase 6 — Integration testing: full end-to-end workflow, extended `go test ./...`, security checks.
8. [ ] Phase 7 — Delivery: README (created; final polish), cleanup, final verification.

## Test status (Phase 2 + 3)

`go test ./...` — all pass (`go vet ./...` and `go build` clean):

Phase 2 suites (unchanged, still green):
- database: fresh init; repeatable migrations; FK enforcement; WAL + busy_timeout.
- auth: plaintext never stored; correct/wrong credentials; expired sessions rejected and purged; logout revocation; purge keeps live sessions.
- handlers (httptest live server): unauthenticated redirects; student blocked from faculty routes and vice versa; login success/failure; CSRF forging rejected (403); logout CSRF-protected and session-invalidating; session rotation on login.
- middleware: CSRF verify incl. anonymous cookie flow.
- seed: idempotent (2 runs → exactly 2 users); preserves modified password hashes; hashes are bcrypt.

Phase 3 suites:
- store: course CRUD + ownership (foreign faculty → ErrForbidden), duplicate course code rejected; student account creation with bcrypt + uniqueness + password policy; duplicate enrollment rejected; enrollment-of-faculty rejected; un-enrolled students denied course access; rubric: partial draft allowed, 100-point publication gate, published rubric locked, criterion validation; submissions: outsider rejected, empty rejected, persistence, dynamic overdue counts, late flag not persisted; uploads: random server names, traversal blocked, extension/type-mismatch/oversize/empty rejected.
- extraction: TXT round-trip + binary junk rejected; DOCX (real zip archive) paragraph extraction + broken archive rejected; PDF (hand-built tiny PDF via ledongthuc/pdf) extraction + junk rejected.
- HTTP (httptest): student blocked from faculty areas; un-enrolled course access → 403; rubric=90 blocks publish, rubric=100 publishes; student sees only published assignments; full flow — faculty creates course → enrolls student → creates + publishes assignment (rubric 100) → student submits text → replaces with DOCX → faculty roster shows metrics → faculty downloads the stored file; `.exe` upload rejected; student/anonymous access to faculty download routes blocked.
- Live smoke (APP_ADDR=127.0.0.1:18080): Data Structures course, Graph Traversal assignment with 100-point rubric published, student sees it, text + file submissions persisted with extracted text, faculty inspection + download working, student → faculty redirect and unauthorized-URL checks verified.

## Phase 3 UI verification (sidebar & polish pass)

Root cause of the "Overview-only" sidebar: `pageFrom` (used by the role home
pages) built a single-item nav while detail pages built the full one via
separate helpers. Fixed by making one shared, path-aware navigation builder:
- Faculty nav = Overview / Courses / Assignments / Students on **every** faculty page.
- Student nav = Overview / My Courses / My Assignments / Performance on **every** student page.
- Active state is section-aware: detail pages highlight their section (`/faculty/courses/1` → Courses, `/student/assignments/1` → My Assignments, `/faculty/submissions/1` → Assignments since submission monitoring sits under it).
- Verified via HTTP smoke: correct active item per URL on all 7 faculty paths and all 6 student paths; zero cross-role href leakage (grep checks).
- Fixed a latent 500: `/student/performance` referenced a deleted placeholder template — now renders a labeled "arrives with Phase 4" empty state, nav active on Performance.
- Authorization denials (403) previously broke out of the app shell; they now render an in-shell error card with the sidebar intact across faculty and student flows.
- CSS polish: sidebar footer contrast raised (#64748B → #AEB8CC on navy), sidebar ✕ button and header hamburger styling, mobile drawer JS exempted the hamburger from the outside-click close, stray `.sidebar-toggle` desktop CSS scoped. Content layout unused whitespace reviewed (max-width/shell padding unchanged; no dead hero blocks).
- All routes re-smoked: every faculty page 200, every student page 200 (enrolled) or a shell-wrapped 403 (not enrolled); static CSS/JS 200; `go test ./...`, `go vet ./...`, `go build` all green.

## Test status (Phase 4)

`go test ./...` — all packages pass (`go vet ./...`, `go build` clean):

Evaluator unit tests (`internal/evaluator`): happy path + server-side total calculation & rounding (46.504 → 66.50); missing/duplicate/unknown criterion rejection; clamp warnings (−5→0, 140→40 with total 40); evidence verification (real sub-string kept, invented quote dropped + warning) including whitespace normalization that cannot fabricate words; DEMO labels/method/confidence/needs-review and feedback disclosure; empty submission and failed-extraction rejection; provider adapter against a **local fake HTTP server only**: malformed body, HTTP 502, empty choices, client timeout, auth header (`Bearer …`) and payload (model, temperature 0.2) shape, and no real provider was ever contacted.

Persistence tests (`internal/store`): enqueue→claim→persist→complete; exclusive claims (no concurrent duplicates); resubmission supersedes stale jobs and re-enqueues; interrupted RUNNING jobs recovered to PENDING; failed evaluations keep the submission content and permit retry via a fresh job; overrides preserve original model points (audit) and record faculty + timestamp; wrong-faculty overrides/approvals rejected; approval validates effective scores, persists the final total and flips eval status; MANUAL validation (missing/unknown/duplicate/out-of-range rejected) and its distinct method label; faculty-scoped overview.

HTTP tests (`internal/handlers`): full flow — DEMO evaluation appears provisional with method+confidence; student cannot see provisional marks; faculty override → approve; student then sees published results including adjusted 45.00; re-evaluate creates attempt 2 with history preserved and resets provisional state; students blocked from every evaluation mutation route; faculty routes reject students; forged CSRF on evaluation mutations → 403.

Live E2E smoke (APP_ADDR=127.0.0.1:18080, no AI credentials): course + enrolled student + published keyword rubric assignment; student submits → job auto-processed by the background worker (~seconds) → DEMO/LOW-confidence provisional result with per-criterion scores and verbatim evidence; faculty saw no student-visible marks before approval; override + approve published server-calculated 90.00; student page showed published results only after approval (and nothing after unpublish); repeated evaluation kept attempts 1/2; manual attempt 3 (70.00) approved and published; the approved/unpublished states stayed consistent for the student; a RUNNING job was recovered and completed after an application restart.

## Client-facing demo credentials

- Faculty: `faculty@educore.demo` / `Faculty123!`
- Students: `student@educore.demo` (primary demo dashboard), plus
  `meera.iyer@`, `rohan.verma@`, `sara.khan@`, `dev.patel@`, `anaya.rao@`,
  `kabir.nair@`, `ishita.sen@` — all `...@educore.demo`, password `Student123!`
  (existing passwords are never overwritten by seeding).

## Phase 5 — Complete interfaces, dashboards, analytics and visual transformation (verified)

- **Design system**: single consolidated `static/css/style.css` with the
  documented token palette (bg/surface/sidebar/primary/semantic-soft colors),
  Inter stack, editorial hierarchy (26px page heads, 17px sections, 28px KPI
  values, 12px labels). Component styles consolidated into
  `static/css/components.css`; legacy per-phase sheets removed.
- **Shell**: fixed 248px navy sidebar with geometric EDUCORE brand, Lucide
  inline SVG icons per nav item, active state with indigo tint + left
  indicator, role-aware sections, pinned avatar profile with sign-out JS-free
  form (CSRF-protected). Sticky 68px header. Content capped at 1440px with
  32px/28px gutters. Mobile: drawer + hamburger, reduced-motion respected.
  Icon system: `internal/handlers/icons.go` (zero-dependency inline Lucide paths).
- **Faculty Overview (centerpiece)**: greeting + live date + course filter;
  five KPI cards (enrolled, courses, completion, approved comprehension,
  pending evaluations with failed/review split); topic mastery bars with
  evaluated-student counts; per-assignment completion table (submitted/expected,
  missing/overdue badges, pending-to-review count); students-requiring-attention
  list (documented rules: approved topic < 60% or multiple missing/overdue work);
  upcoming deadlines (14-day window); recent activity table. Everything computed
  from the database through `internal/analytics`.
- **Student Overview**: four KPIs (approved average, subject comprehension,
  completion, pending submissions), prominent upcoming-assignment list with
  due state / overdue markers and direct actions, subject cards with progress
  bars, recent approved evaluations, learning insights (strongest/weakest topics
  with evaluation counts), empty states before approvals exist.
- **Student Performance page**: subject comparison bars, topic mastery bars
  (grouped by subject), approved-evaluation trend SVG line chart with honest
  empty state under two points, learning gaps list (< 60% threshold),
  demonstrated strengths list (≥ 75%).
- **Analytics package** (`internal/analytics`, unit tested): assignment
  completion (submitted/expected×100), topic mastery (per-student percent first,
  then topic mean — no submission-count overweighting), subject comprehension
  (mean of per-student means), student averages with strongest/weakest topic
  extraction. Only the **latest approved attempt per submission version**
  feeds every metric; effective (override-aware) totals are used. New store
  loaders: `ApprovedRowsForFaculty/Student`, `FacultyKPIs`,
  `AssignmentCompletionRows`, `AttentionList`, `UpcomingDeadlines`,
  `RecentActivity`, `SubmissionStatuses`.
- **Charts** (`internal/charts`): server-generated SVG horizontal bars grouped
  LOW/MID/HIGH color bands, responsive inline progress bars, chronological line
  chart with gridlines, labels, titles/desc/aria labels and safe empty states.
- **Seed showcase** (`internal/seed/showcase.go`): 3 courses, 8 students
  (Arjun = `student@educore.demo`), 6 assignments across different topics with
  100-point rubrics + keywords, 27 submissions, 22 evaluations (22 approved,
  several pending-review, one provider-failure), truthful evidence strings cut
  from the actual submission text, late submissions, missing/overdue students.
  Idempotent via natural keys (verified 3 consecutive seed runs), never touches
  existing accounts/passwords. Second-run FK bug found and fixed in testing.
- **Concurrency bug found and fixed during smoke testing**: streaming DB rows
  while re-querying with a pool of 1 self-deadlocked the dashboard; fixed by
  materializing result sets before sub-queries and raising the pure-Go driver
  pool to 4 connections (WAL + busy timeout serialize writes).
- **Nondeterministic template bug found and fixed**: `partials/sidebar.html`
  and `partials/shell.html` both defined `{{define "sidebar"}}`; map iteration
  order silently picked a variant per process run. Stale partial deleted.
- Verified via HTTP smoke with the seeded showcase: faculty + student
  dashboards return 200 with populated KPI values, 5 topic bars, attention
  rows, activity rows and deadline entries; performance page renders subject,
  topic and trend charts from approved data; every nav link 200; no cross-role
  href leakage. `go test ./...`, `go vet ./...`, `go build` all clean.
- Visual inspection: performed via rendered-HTML structural assertions and
  screenshot-free checks (this environment has no browser automation available;
  no claims of pixel-level review are made).

## Final run commands

```
go build -o bin/educore .
./bin/educore            # serves http://127.0.0.1:8080 (idempotent seed on first run)
./bin/educore seed       # re-run seeding explicitly, safe to repeat
go test ./...
```

Optional `.env`: `AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL` to enable real AI evaluation; without them the labeled DEMO mode runs. AI mode expects the OpenAI-compatible chat-completions path (`<AI_BASE_URL>/chat/completions`).

## Phase 5 correction pass (pre-Phase 6) — issues, root causes, fixes, tests

1. **Raw HTML/SVG printed as text in dashboards.** Root cause: the template
   function `progress` returned a plain `string` (untrusted) so `html/template`
   escaped the generated markup. Fix: `charts.Progress`/`charts.Bars` are the
   only producers and propagate `template.HTML` (markup generated exclusively
   by trusted Go code); no DB/user content flows through them. Regression:
   `TestProgressFuncIsTrustedHTML`, `TestUserContentStaysEscaped`,
   `TestInitialsSafe` (production `templateFuncs()` round trip).
2. **Black bars.** Root cause: `.fill-*` CSS set `background` (does not paint
   SVG `fill`). Fix: explicit `fill="#536FE5" / #0F766E / #B45309 / #DC2626`
   and `#E4E8F0` track attributes inside the SVG helpers. Regression:
   `TestProgressWidthsAndColors` (0/50/100 + clamp + aria), `TestBarsTokenColorsAndEscaping`
   (token fills present, no browser-default black, `<script>` injection escaped).
3. **Status contradictions (100% completion + "overdue, not submitted" vs
   "submitted late").** Root cause: dashboard's upcoming list labeled
   late-but-submitted assignments as "not submitted", and multiple pages each
   derived status independently. Fix: single source of truth
   `store.StudentAssignmentStates(studentID)` (enrollment scope, publication
   status, latest submission, deadline, evaluation state →
   SUBMITTED/LATE/OVERDUE/eval statuses) — now used by the student dashboard,
   My Assignments (with new Evaluation column and Late badges) and the
   dashboard detail text; a late-but-submitted item can never show
   "submit as soon as possible" again. Regression:
   `TestLateSubmissionConsistentStates` (store), `TestScreenshotStatusConsistency`
   (HTTP: dashboard/`My Assignments`/detail all agree; late badge present;
   overdue never invented).
4. **Stale "Awaiting Phase 4"** on My Assignments → replaced with the real
   evaluation status (`evalStatusText/Class`) and an honest marks column
   ("See results…" / "Provisional marks stay with faculty until approved").
   `performance.html` (superseded placeholder page) deleted. Repository-wide
   grep for stale phase refs returned clean.
5. **Trend chart showing one marker for three approvals.** Root cause: showcase
   approvals share one approval timestamp; the line chart collapses visually.
   Fix: `charts.Line` groups records by UTC day (mean score), discloses
   "(n evaluation/s)" per point in the tooltip, and with fewer than two
   distinct days explains exactly that ("All approved evaluations share the
   same approval date…"). No artificial offsets introduced; the DB query
   confirms 3 approvals on one date for the demo student. Regression:
   `TestLineGroupingIdenticalTimestamps`, `TestLineEmptyStates`.
6. **Faculty Students page completed**: new columns — courses (faculty scope),
   average approved marks, completion rate, explainable attention reasons
   (`Repeated weak results (… weakest topic: X)`, `N overdue missing
   assignments`, `N missing assignments`, low-completion note). Students
   without approved results / published assignments show explicit states.
   Regression: store-side ownership is enforced by construction
   (`ApprovedRowsForFaculty` + per-course rosters) — coverage via existing
   `TestEvalOverviewOwnership` + the new completion tests.
7. **Student My Courses cards**: subject comprehension (token progress bar or
   a "No approved results yet" state — never a fake 0%), evaluated count,
   submitted/published count, faculty + link to details. Verified in the live
   render (3 subject bars / info states).
8. **Faculty completion rows 0/0 and inconsistent KPI.** Root causes: (a)
   `AssignmentCounts` faced a half-populated assignment (`CourseID` = 0 → empty
   roster → "Complete 0%"); (b) the faculty KPI multiplied *all* enrollments by
   *all* published assignments instead of the per-course sums. Fix: defensive
   course lookup in `AssignmentCounts`; KPI expected =
   Σ per-course (enrolled × published) — now exactly reconcilable with rows;
   template shows "N/A — no enrollments" instead of a meaningless Complete/0%.
   Regression: `TestCompletionCornerCases` (zero enrollments),
   `TestCompletionReconciliationAcrossCourses` (partial/complete/multi-course;
   rows 100 %/50 % reconcile with KPI 3/4).
9. Deployment-state bug fixed during verification: a stale server binary kept
   serving pre-fix markup; the live smoke now runs the current build.
10. **Blank names/emails in the "Students requiring attention" table**
   (correction pass, final fix). Root cause: `AttentionList` resolved student
   identity only for students present in *approved evaluations*; rows created
   from the missing-work roster scan (students with no approved evaluations
   yet) looked up an empty identity map and rendered blank names/emails.
   Fix: identity is now preloaded for every student enrolled in the faculty's
   courses via one `DISTINCT` query, course association for missing-work rows
   uses full enrollment membership (not just approved-result rows), and a
   defensive guard refuses to emit any row with a blank identity. Regression
   test `TestAttentionListIdentityCompleteness`: three students with different
   missing-work profiles (one with no evaluations at all, one with a single
   miss not flagged, one clean student never flagged) all render with correct,
   independent names/emails, no duplicates, and faculty-scoped course codes.
   Verified against the live showcase DB: all four attention rows — Dev Patel,
   Ishita Sen, Kabir Nair, Rohan Verma — render with correct names and emails;
   zero blank rows.

Test outcome: `go test ./...` all packages pass (9 packages incl. new
`charts` tests), `go vet ./...` clean, `go build` clean; live smoke against the
existing showcase database (not recreated): all token fills present, zero
escaped-markup instances, zero stale placeholders, late badges and consistent
statuses verified, Students/My Courses/completion tables populated from real
data, honest trend empty state where the data supports nothing else. Visual
inspection again performed via rendered-HTML assertions (no screenshot tooling
environment installed).

## DeepInfra provider readiness (credit-limited demonstration prep)

DeepInfra compatibility is achieved through the existing OpenAI-compatible
provider abstraction; no new dependency, no redesign.

- **Retry budget hardened**: the engine previously issued a second full request
  after *any* failure (even 401/403/429), burning credits. Now there is exactly
  one retry per evaluation, and **only** for transient failures (408/5xx/net);
  auth (401/403), billing (402), rate-limit windows (429) and malformed
  requests (4xx) fail immediately without a second call. Provider failures
  still never fall back to DEMO and never produce a falsely approved
  evaluation (status FAILED, submission intact).
- **Token budget configurable**: new env `AI_MAX_TOKENS` (default 4096, clamped
  256–8192) wired through `config → Engine.MaxTokens → every chat request`.
- **New CLI subcommands**:
  - `./bin/educore provider-check` — exactly ONE tiny request (≤32 output
    tokens), validates `{\"ok\":true}`, prints the endpoint/model and a masked
    key (`****last4`); refuses to run without credentials; exits non-zero on
    failure.
  - `./bin/educore evaluate-once --submission=<id> --confirm` — one real
    end-to-end evaluation through the production engine (same validation,
    one-retry budget), persisted as NEEDS_REVIEW; refuses approved submissions,
    refuses to run without `--confirm`, and on failure marks the submission
    FAILED while preserving content (never fabricates a DEMO result).
- **Key safety**: key travels only in the server-side Authorization header,
  never logged, never rendered; the only display surface is SafeMask's masked
  form. Regression tests confirm masked output and header presence.
- **Tests (all mocked HTTP, zero real API calls)**:
  successful DeepInfra-shaped parsing (exactly 1 request, Bearer header,
  `/v1/openai/chat/completions` path), auth failure (exactly 1 call, HTTP 401
  surfaced, no DEMO fallback), 429 (no retry), malformed→valid (exactly 1
  retry), malformed×2 (budget capped at 2 calls, clean failure), transient
  503→valid recovery, probe single tiny request, mask safety, plus the
  end-to-end CLI probe flow test against a fake server.
- Showcase database untouched (9 users / 22 evaluations / 22 approved verified
  after the change).


## Phase 6 — agentic evaluation system (implemented; verified locally)

- **Architecture**: new `internal/agent` package (types, tool registry, bounded execution loop) + `internal/evaluator/chat.go`
  (OpenAI-compatible **tool-calling** conversation turns on the existing DeepInfra provider: `tools` array in, assistant
  `tool_calls` out, raw-message passthrough so vendor/tool_call semantics survive resume). No multi-agent framework, no new dependencies.
- **Model loop**: genuine dynamic loop — the model sees the goal, decides which tool to call each turn, receives structured
  observations, and may finish only via `save_pending_verification`-style tools. Caps: 12 model turns (configurable per run),
  24 tool calls, 200k-char output budget, per-run 120s provider timeout. Scripted sequences are not possible — each turn's
  tool choice comes from the live model response.
- **Tools (6)**: get_assignment_context, inspect_submission (extraction warnings included), search_submission (verbatim
  snippets with character offsets), evaluate_criterion (bounds + verbatim-evidence validation via `QuoteContains`), verify_evaluation
  (coverage/bounds/total/evidence audit), save_pending_evaluation (feeds the draft through the production `ValidateAgentResult`
  pipeline + `PersistEvaluation` → NEEDS_REVIEW; never approves).
- **State & trace**: migration `004_agent.sql` — `agent_runs` (goal, kind EVALUATE/RECONSIDER/FOLLOWUP, status, iterations,
  full transcript checkpointed each turn, draft state JSON, result_evaluation_id, readonly flag) + `agent_events`
  (MODEL_CALL/TOOL_CALL/TOOL_RESULT/TOOL_ERROR/FINAL/ERROR/USER_MSG/NOTE with timestamps). Startup recovery moves interrupted
  RUNNING runs back to PENDING; runs are resumable/re-runnable from the console.
- **Authorization**: runs are faculty-bound; tools scope to the owning faculty (submission→course→faculty check in the loop,
  store checks in console handlers); foreign faculty get 403; read-only follow-up conversations exclude all mutating tools.
- **UI**: Faculty Agent Console (`/faculty/submissions/{id}/agent` lists runs + start/follow-up forms; `/faculty/agent/runs/{id}`
  shows goal, status, FULL trace table, saved-evaluation callout, re-run/reconsider/read-only-question forms; auto-refresh while
  RUNNING); linked from the grading workspace header. Everything rendered from real DB execution state — no fabricated activity.
- **CLI**: `educore provider-check` (smallest connectivity request) and `educore evaluate-once --submission=<id> --confirm`
  (one real evaluation, credit-guarded) from the previous milestone still work; AI_MAX_TOKENS caps every agent turn too.
- **Tests (all model calls mocked)**: full happy path (7 turns → pending eval persisted, trace complete, not auto-approved),
  iteration-cap enforcement without save, invalid tool args (bounds violation traced, no eval), unknown tool rejected, fabricated
  evidence rejected (verbatim-quote rule), authorization boundary (foreign faculty 403), follow-up read-only (mutating tool
  unavailable, zero approved evaluations), reconsider → new attempt with history preserved, durable state persistence + recovery,
  draft checkpoint across turns, provider failure → run FAILED + submission intact. 11 packages, all green; `go vet` and
  `go build` clean; showcase DB intact (9 users / 22 evaluations / 22 approved).
- **Live smoke**: console renders (200), start run → 303 to run page, trace recorded, **without AI credentials** the run aborts
  honestly at iteration 0 with a clear escalation message; forged-CSRF mutations rejected with 403.

Real-run commands (awaiting explicit approval; each run costs ~1 provider request per agent turn):
```
./bin/educore provider-check                                   # 1 tiny request
./bin/educore evaluate-once --submission=<id> --confirm        # single queued-style evaluation
# or: open the agent console, press "Start agent evaluation", watch the trace
AI_MAX_TOKENS=2048 ./bin/educore                               # smaller turns = fewer tokens per turn
```

## Blockers

None. Note: port 8080 is occupied on this machine by an unrelated process
(`vidya-server`); use `APP_ADDR=...:18080` locally — default remains per spec.

## New dependency (Phase 3)

- `github.com/ledongthuc/pdf` (pure Go, from the local module cache) — the only
  dependency added in Phase 3, for PDF text extraction per §6 of the spec.

## Phase 4 dependency & limitation notes

- No new Go dependencies in Phase 4 — the provider, queue, validation and DEMO
  evaluator are all standard library.
- Known honest limitations: the provider adapter has only been exercised
  against a local fake HTTP server (no real credentials configured on this
  machine); DEMO scores are keyword-based and explicitly not a measure of
  understanding; per-criterion decimal scores are supported (min step 0.5 in
  the faculty UI) while the DB schema keeps historical INTEGER affinity for
  legacy rows.

## AI Learning Studio — Phase 1: Course content foundation

- Added additive migration `005_course_chapters.sql` with course-owned
  chapters, ordered position, Markdown/plain-text content, publication state
  (draft by default), timestamps and course/order indexes.
- Added store CRUD/read operations with faculty course ownership checks,
  server-side field limits, positive chapter-order validation and a content
  requirement before publication. Student-facing store reads are scoped to
  enrolled student accounts and published chapters only.
- Added faculty chapter list, create and edit pages from the existing course
  detail flow. Publishing and draft saves return faculty to the chapter
  management list. The chapter title uses a multiline field matching the
  description field.
- Added student published-chapter listing to enrolled course details and a
  chapter reading page. Student reads are enrollment-scoped and published-only;
  draft and unenrolled direct requests return not found. The reading-page header
  displays only the chapter title.
- No AI calls, evaluation logic, agent tables or submission uploads are
  involved in this phase.
- Added store, handler and migration tests for CRUD, chapter ordering,
  ownership, CSRF, draft/published visibility, invalid inputs and preservation
  of existing data across migration.
- Phase 1 verification: `go test ./...`, `go vet ./...`, and
  `go build -o bin/educore .` (results recorded in the implementation
  completion report).
- UX follow-up verification includes publish redirect and student chapter
  visibility handler tests.

## AI Learning Studio — student experience and AI tutor

- Added the student Learning Studio navigation entry and a dedicated enrolled-course
  view showing course descriptions and ordered, published chapters only.
- Added a direct Learning Studio button to each student course overview.
- Added idempotent showcase content for the seeded Databases course: Chapter 1
  introduces relational databases and Chapter 2 is "Relational Model, Keys and
  Integrity Constraints." Existing courses with faculty-created chapters are
  left untouched by reseeding.
- Expanded published chapter reading pages into a responsive lesson workspace:
  course context, chapter description and source material, course outline, guided
  lesson panel, suggested questions and tutor chat.
- The guided lesson and chat call the existing OpenAI-compatible provider
  transport server-side. The learning handlers depend on the provider interface,
  not the assessment engine; evaluation, agent runs, submissions and uploads are
  unchanged. Provider configuration comes from the existing `AI_*` settings.
- Added an AI-generated three-question multiple-choice knowledge check below
  the guided lesson. Quiz answer keys remain server-side in short-lived,
  student/course/chapter-bound in-memory quiz sessions; submitted answers are
  scored on the server and receive per-question explanations. Quiz loading is
  independent from lesson generation, and response parsing accepts JSON wrapped
  in common Markdown fences or short provider preambles.
- Every lesson/chat request re-loads the chapter through the enrolled-student,
  published-only store query. Requests are CSRF-protected; question length,
  history length and chapter context are bounded. Chat history stays in the
  current page and is not persisted.
- Provider errors return user-friendly messages without returning provider
  diagnostics, credentials or student prompts. When AI is not configured, the
  UI reports that state rather than showing a fake response.
- Tutor chat responses request up to 2,400 output tokens, allow up to 8,000
  response characters, and ask the model for focused but complete answers.
- Added handler tests for the course/chapter view, real chapter-context prompt
  construction, follow-up history, CSRF, invalid input, draft/unenrolled access
  and provider failures.
- Verification: `go test ./...`, `go vet ./...`, `go build -o bin/educore .`,
  and `git diff --check` all pass. Tutor provider behavior is tested with a
  controlled test stub; no live AI provider request was made during development.
