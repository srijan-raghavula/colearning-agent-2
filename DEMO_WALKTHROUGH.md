# EDUCORE: 15-minute project walkthrough

This guide is written for two presenters. It covers the product, architecture,
the difference between standard AI evaluation and the agentic workflow, and a
short end-to-end demonstration.

## What to communicate

> EDUCORE is a two-sided academic assessment platform. Faculty define courses,
> assignments, and rubrics; students submit work and learn from feedback. AI can
> assist with rubric-based evaluation, but it does not publish grades: faculty
> review and approve every result. The agentic evaluator makes its work
> inspectable through evidence, tool calls, and a persistent execution trace.

## Before the presentation

### Prepare the app

From the project directory:

```bash
go test ./...
go build -o bin/educore .
```

For live AI features, configure all three settings locally in `.env` or as
environment variables: `AI_BASE_URL`, `AI_API_KEY`, and `AI_MODEL`. Keep the key
private; do not put it in slides, screenshots, screen shares, or chat. Check the
provider before the presentation:

```bash
./bin/educore provider-check
```

Start the app in its own terminal:

```bash
./bin/educore
```

If using a fresh database, startup seeds it automatically. For an existing
database, `./bin/educore seed` is safe to re-run and adds missing showcase data
without overwriting existing account passwords or submissions.

### Prepare the browser

- Open two browser profiles or separate browsers: one for faculty and one for
  student. This avoids signing in and out during the demo.
- Sign in with the seeded accounts:
  - Faculty: `faculty@educore.demo` / `Faculty123!`
  - Student: `student@educore.demo` / `Student123!`
- In the faculty profile, open **Assignments → Data Structures → Graph
  Algorithms in Practice → submissions → Arjun Mehta**. Keep this submission
  ready for the agent demo.
- In the student profile, open **Learning Studio → Databases → Relational
  Model, Keys and Integrity Constraints**.
- Test that each page loads before presenting. Do not rely on live model latency
  to fit the schedule.
- Live agent runs and AI tutor/quiz calls use the configured provider and may
  consume credits. Do not start a live run unless the team has approved that
  use. If the provider is unavailable, show the persisted seeded evaluation
  screens and explain that real agent runs require the provider.

## Minute-by-minute agenda

| Time | Presenter | Segment |
|---|---|---|
| 0:00–0:45 | A | Problem and product overview |
| 0:45–2:45 | B | Architecture and data flow |
| 2:45–4:30 | A | Faculty course, assignment, and rubric |
| 4:30–6:15 | B | Student submission and course experience |
| 6:15–8:15 | A | Standard evaluation and faculty review |
| 8:15–12:15 | B | Agentic evaluator and execution trace |
| 12:15–14:15 | A | Learning Studio, tutor, quiz, and dashboards |
| 14:15–15:00 | A + B | Takeaways and questions |

If time is short, skip creating or editing records live: use the seeded course
and assignment and spend the saved time on the agent trace and human approval.

## Presenter script and demo actions

### 0:00–0:45 — Problem and product

**Presenter A says:**

> In a typical course, assignment setup, submission tracking, grading, and
> student feedback are spread across different tools. EDUCORE connects those
> workflows. Faculty retain control of the rubric and final approval, while
> students get a clear place to submit work, view approved results, and learn
> from course material.

Point out that the demo will follow one assignment from faculty setup through
student work to review, then show how the agent carries out part of that review.

### 0:45–2:45 — Architecture and data flow

**Presenter B explains:**

> The application is a single Go web server. The standard library handles HTTP
> routing and the OpenAI-compatible provider transport. Pages are rendered with
> `html/template`; styling is custom CSS, and small interactions use vanilla
> JavaScript. SQLite stores users, course activity, submissions, evaluations,
> and agent traces. SQL migrations evolve the schema without replacing the
> database.

Show this overview slide or describe it verbally:

```text
Browser
  │ HTTP forms / small JSON requests
  ▼
net/http routes + middleware
  ├── auth, role checks, sessions, CSRF
  ├── handlers ───────────────► html/template pages + static CSS/JS
  ├── store ──────────────────► SQLite + migrations
  ├── evaluation worker ──────► evaluator (AI provider or labeled DEMO)
  ├── agent engine ───────────► model tool calls ↔ scoped store tools
  └── Learning Studio ────────► shared provider interface
```

**Architecture points to call out:**

- `internal/handlers` owns HTTP workflows and permissions at the route boundary.
- `internal/store` owns database queries, transactions, uploads, and persisted
  state.
- `internal/auth` and `internal/middleware` provide bcrypt password hashing,
  server-side sessions, role-aware access, and CSRF protection.
- `internal/evaluator` handles provider calls, deterministic DEMO scoring, and
  strict validation of rubric results.
- `internal/agent` contains the separate model-driven, tool-using evaluation
  loop. It reuses the evaluator provider and store operations rather than
  duplicating database business logic.
- `internal/analytics` derives student/course insights from evaluation data;
  charts are rendered as SVG by Go.
- Student submissions can be text or supported TXT, DOCX, and PDF uploads;
  extracted text is saved for evaluation and review.

### 2:45–4:30 — Faculty setup

**Presenter A demonstrates:**

1. In the faculty browser, show the overview and its course/assignment
   monitoring.
2. Open **Courses** and select **Data Structures**.
3. Open an assignment and point out that it has instructions, a topic, a due
   date, publication state, and rubric criteria.
4. If convenient, show the rubric editor without saving changes. Explain that
   publishing requires the rubric to total exactly 100 points.
5. Show the submissions list and explain that faculty can see who submitted,
   missing/late work, and evaluation state.

**Presenter A says:**

> The rubric is the contract for grading. It is authored by faculty, validated
> before publication, and reused by both evaluation paths. AI does not invent
> the criteria or decide whether a result is published.

### 4:30–6:15 — Student workflow

**Presenter B demonstrates:**

1. Switch to the prepared student browser.
2. Show the student overview, then **My Courses → Data Structures**.
3. Open **My Assignments** and point out status and due-date information.
4. If demonstrating a live submission, open an assignment and show the text/file
   submission form. Avoid overwriting seeded work during the presentation.
5. Point out that the student sees marks only after faculty approval.

**Presenter B says:**

> Submissions are tied to the student and assignment. Uploads are size/type
> checked, stored under server-generated names, and extracted text is retained
> for grading. A failed extraction is surfaced for human review rather than
> silently treated as a valid empty answer.

### 6:15–8:15 — Standard evaluation and review

**Presenter A demonstrates:**

1. Return to the faculty browser and open Arjun Mehta’s **Graph Algorithms in
   Practice** submission.
2. Point out the submission, assignment instructions, and rubric workspace.
3. Explain that **Run evaluation** queues a normal evaluation job. A background
   worker claims and processes it.
4. Show the existing provisional evaluation, if available. Identify the method
   badge: `AI`, `DEMO`, or `MANUAL`.
5. Point out criterion-level scores, feedback, evidence, confidence, and review
   warnings. Demonstrate where faculty can override a criterion and approve or
   unpublish.

**Presenter A says:**

> Standard evaluation is a single structured scoring workflow, not an
> autonomous agent. With a provider configured, the model proposes criterion
> results and evidence; the server validates rubric coverage, score bounds, and
> evidence before persistence. Without a provider, the separate deterministic
> keyword matcher is clearly labeled DEMO. Neither path auto-publishes.

Emphasize that the final total is computed from validated criterion scores by
the server, and only faculty approval publishes the mark.

### 8:15–12:15 — Agentic evaluation

**Presenter B demonstrates:**

1. From the grading workspace, open **Agent console**.
2. Briefly show the goal field and the explanation that the agent uses tools and
   saves a pending evaluation.
3. If an approved live run is available, start one with a focused optional goal
   such as: `Assess the BFS/DFS comparison and cite evidence for each rubric
   criterion.`
4. As the run progresses, point to the execution trace. Explain the sequence
   visible in the actual trace: model calls, selected tools, tool results, any
   validation errors, and the final event.
5. Open the saved result in the grading workspace. Show that it is still
   pending and needs faculty review.
6. Point out the read-only follow-up option. Explain that a follow-up can
   inspect and discuss evidence, but it cannot change scores or save an
   evaluation.

**Presenter B says:**

> This is the agentic part. The model chooses tools dynamically based on what it
> has learned so far; it is not replaying a hard-coded script. The tools expose
> narrow, faculty-scoped capabilities: get assignment context, inspect the
> submission, search for evidence, score one criterion, verify the draft, and
> save a pending result.

> The agent is instructed to treat submission text as untrusted data and to
> support scores with verbatim quotes. Tool implementations validate inputs and
> evidence; the completed draft goes through the production evaluator
> validation/persistence path. The agent cannot approve or publish marks.

**Explain durability and guardrails:**

- `agent_runs` stores run state, transcript checkpoints, status, goal, and
  result reference; `agent_events` stores the ordered trace.
- Interrupted runs can be recovered and resumed from persisted state.
- The run is bounded by model-turn, tool-call, and accumulated-output limits.
- Tool access checks ensure the run belongs to the faculty member who owns the
  course/submission.
- The read-only follow-up tool set excludes score-writing, verification, and
  save tools.
- A failed or incomplete run escalates to faculty review; it does not fabricate
  a successful evaluation.

If the live run is still processing near the end of this segment, continue
explaining the trace and return to the result only if it finishes. Avoid
restarting it repeatedly; each model turn can use provider credits.

### 12:15–14:15 — Learning Studio and analytics

**Presenter A demonstrates:**

1. In the student browser, open the prepared **Databases** chapter in Learning
   Studio.
2. Point out the source material and chapter outline, generated guided lesson,
   and suggested tutor questions.
3. Ask a chapter-specific question, for example: “How are primary keys and
   foreign keys different?”
4. Show the three-question chapter quiz. Explain that answer keys stay on the
   server, answers are scored server-side, and a student can retry the same
   questions until earning a perfect score.
5. Return to the student performance/overview page and show how approved
   results feed course and topic insights.

**Presenter A says:**

> The Learning Studio uses the same server-side provider configuration but is
> separate from the assessment agent. It grounds explanations and quizzes in
> published chapter material. Student access is enrollment-scoped, chat history
> is page-local, and no fabricated fallback is shown when AI is unavailable.

### 14:15–15:00 — Close

**Presenter A summarizes:**

> EDUCORE brings course setup, rubric-based assessment, student submissions,
> faculty review, and learning support into one application. The AI can assist
> and explain its work, but evidence validation, access controls, and faculty
> approval keep the final academic decision with the instructor.

**Presenter B adds:**

> The key distinction is transparency: the agent’s trace shows how it gathered
> evidence and reached a pending result, and faculty remain in control of what
> becomes a published mark.

## Agentic AI: concise explanation for questions

Use this explanation if someone asks what makes the feature “agentic”:

1. A faculty member starts an explicit evaluation run with a goal.
2. The model receives the goal and a set of declared tool schemas.
3. The model chooses a tool call. The server validates/executes it and returns a
   structured observation.
4. The model uses that observation to choose the next action. For example, it
   can inspect the rubric, search the submission for a criterion, record a
   score, and then verify the draft.
5. The loop can finish the evaluation only by invoking the save-pending tool.
   The production validation path persists it as needing faculty review.

This is a **single model-driven agent with tools**, not a multi-agent system.
The standard evaluator and the chapter tutor are other AI workflows and should
not be described as this agent loop.

## Data and trust boundaries

```text
Faculty-authored rubric ─┐
                         ├─► evaluator / agent ─► validated pending result
Student submission ─────┘                               │
                                                       ▼
                                           faculty review + approval
                                                       │
                                                       ▼
                                         student-visible published mark
```

- Submission content is untrusted input, not instructions to the model.
- The server owns identity, authorization, score validation, evidence checks,
  persistence, and approval state.
- Exact quotation checks help ensure cited evidence exists in the submitted
  text; they do not prove an interpretation is academically correct.
- AI output can be wrong. The review workspace and manual grading path are
  intentional parts of the product, not exceptions.

## Questions you may get

**Does the AI decide the final grade?**  
No. The server validates the result and a faculty member must approve it before
the mark is published.

**What happens without AI credentials?**  
Regular evaluation can run in a clearly labeled deterministic DEMO mode. The
agentic evaluator and Learning Studio AI features require a complete configured
provider and do not silently substitute fake AI responses.

**Why have both regular evaluation and an agent?**  
The regular evaluator is the simpler queued scoring path. The agent is useful
when the workflow benefits from multiple inspect/search/score/verify actions
and a trace of those steps.

**Can the agent change or publish marks?**  
It can save a new pending attempt. Faculty review and approval remain required.
Read-only follow-up runs cannot mutate scores.

**What is persistent?**  
Courses, assignments, submissions, evaluation attempts, approvals, and agent
runs/traces are stored in SQLite. Tutor chat history is not persisted; quiz
sessions are short-lived in-memory records.

## Project map for a code-oriented audience

| Area | Source |
|---|---|
| Startup, configuration wiring, HTTP server | [`main.go`](./main.go), [`internal/config`](./internal/config) |
| Routes and request handlers | [`internal/handlers`](./internal/handlers) |
| Templates and browser assets | [`templates`](./templates), [`static`](./static) |
| Authentication and middleware | [`internal/auth`](./internal/auth), [`internal/middleware`](./internal/middleware) |
| SQLite access, migrations, submissions, workers | [`internal/store`](./internal/store), [`internal/database`](./internal/database), [`migrations`](./migrations) |
| Standard evaluator and provider | [`internal/evaluator`](./internal/evaluator) |
| Agent loop and tool registry | [`internal/agent`](./internal/agent), [`agentboot.go`](./agentboot.go) |
| Learning Studio tutor and quiz | [`internal/handlers/learning_studio.go`](./internal/handlers/learning_studio.go), [`static/js/learning-studio.js`](./static/js/learning-studio.js) |
| Seeded demonstration accounts and content | [`internal/seed`](./internal/seed) |
