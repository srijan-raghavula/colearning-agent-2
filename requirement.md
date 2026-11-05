# OpenCode Master Build Prompt — EDUCORE

You are the lead full-stack engineer responsible for delivering a complete, working academic assessment intelligence platform.

Your task is to BUILD the application in the current repository, not merely propose an architecture, create mockups, or explain how it could be built.

## 1. Product objective

Build a dual-role educational assessment platform with two interfaces:

- **Faculty:** Create subjects and assignments, define marking rubrics, evaluate submissions, monitor missing work, and understand student comprehension.
- **Student:** View enrolled subjects, track assignments, submit work, view marks, and receive personalized diagnostic feedback.

The central feature is an assessment engine that evaluates submissions against faculty-defined rubrics and turns assessment results into meaningful topic-level and subject-level comprehension metrics.

The result must be a functional application that can be demonstrated locally.

## 2. Operating instructions

These instructions are mandatory.

1. Treat the current working directory as the project root. Start by running `pwd`, examining the directory, checking Git status, and inspecting the existing source code.
2. Preserve the existing framework and working functionality whenever possible. Do not replace an existing project with a new scaffold just because you prefer another stack.
3. If the repository is empty, build the application with the mandatory Go technology stack defined in section 3.
4. Work directly in the repository using your available terminal and file-editing tools.
5. Do not spend the task explaining plans to me. Write a short implementation checklist and immediately execute it.
6. Do not ask me questions. Make reasonable engineering decisions.
7. Implement features end-to-end. A button must perform its advertised action; forms must persist data; dashboards must use actual database queries.
8. Do not introduce dependencies or infrastructure without a concrete need. No Docker, hosted database, paid service, or external authentication provider should be required for local execution.
9. Keep track of completed tasks and outstanding issues in `BUILD_PROGRESS.md`.
10. After every major phase, run the relevant checks and fix failures before moving forward.

Do not stop after creating the frontend. The backend, database, authentication, evaluation logic, and user workflows are equally important.

## 3. Technology stack, architecture and data model

### Mandatory technology stack

The application is a lightweight, single-binary Go web application that runs locally without containers, build toolchains, or external services.

- **Language / backend:** Go, organized into small packages (web handlers, data access, auth, evaluation engine, analytics, seed).
- **HTTP server:** Go standard library `net/http` (Go 1.27+'s routing enhancements, e.g. method-and-path patterns, are fine to use).
- **Frontend:** Server-rendered HTML using `html/template`. No client-side framework, no bundler, no Node.js.
- **Styling:** Custom CSS authored for this project, served as static assets (design specification in section 9 is unchanged and must be implemented faithfully).
- **Interactivity:** Minimal vanilla JavaScript only (progressive enhancement: dropdown toggles, chart tooltips if needed, form hints). Every feature must work without it or degrade gracefully.
- **Database:** SQLite, stored locally in `./data/educore.db`.
- **Database access:** Go `database/sql` with the `modernc.org/sqlite` driver (pure-Go, CGO-free). No database server, migrations via tracked SQL files applied at startup.
- **Authentication:** Server-side sessions stored in SQLite, HTTP-only secure cookies. Password hashing with `golang.org/x/crypto/bcrypt` (or `crypto/argon2` — one bucket of decision, no alternatives).
- **Charts:** SVG rendered on the server by Go code (hand-written templates or generated path data). No chart JavaScript libraries.
- **AI integration:** OpenAI-compatible chat-completions HTTP API called with the standard `net/http` client, JSON handled with `encoding/json`.
- **File storage:** Local filesystem under `./data/uploads/`, with server-side validation of extension, MIME, and size.
- **Icons:** Lucide icons embedded as inline SVG (server-side partials); no icon font or icon package runtime.

Prefer the standard library over dependencies everywhere it is reasonably practical. The external dependency set must stay minimal (roughly: the SQLite driver and a password-hashing/crypto package; nothing else without concrete need).

### Architecture principles

Use a clean, maintainable architecture. Separate presentation, data access, business logic, authentication, and evaluation services.

Use these entities:

### User
- ID
- Name
- Email
- Password hash
- Role: FACULTY or STUDENT

### Course
A course represents one academic subject.

- ID
- Name
- Course code
- Description
- Faculty owner

### Enrollment
Connects students to courses.

- Student ID
- Course ID

### Assignment
- ID
- Course ID
- Title
- Description
- Topic
- Due date
- Publication status
- Total marks

### RubricCriterion
Every assignment must have multiple evaluation criteria.

- Criterion title
- Criterion description
- Maximum points
- Relevant topic
- Optional evaluation keywords

All criterion maximum points for an assignment must add up to 100.

### Submission
- ID
- Assignment ID
- Student ID
- Text content
- Uploaded filename, if applicable
- Stored file location, if applicable
- Submission timestamp
- Current status

### Evaluation
- Submission ID
- Total score
- Overall diagnostic feedback
- Identified strengths
- Identified learning gaps
- Suggested improvements
- Confidence level
- Evaluation method
- Review/publication status

### CriterionResult
- Evaluation ID
- Criterion ID
- Awarded points
- Evidence extracted from the submission
- Criterion-specific feedback

Support multiple evaluation attempts, but consistently display the latest approved result.

Avoid duplicating information across tables. Enforce database relations and uniqueness constraints where appropriate.

## 4. Authentication and authorization

Implement actual authentication, not a frontend-only role selector.

Requirements:

- Passwords must be securely hashed.
- Sessions must use secure, HTTP-only cookies.
- Protected pages and API operations must validate the authenticated user on the server.
- Students must never be able to access another student's submissions or feedback.
- Students cannot create assignments, change grades, or access faculty analytics.
- Faculty can only administer their own courses.
- Never trust a role supplied by the client.
- Validate and bind inputs on the server (struct field validation in Go handlers; no reliance on client-side checks). Form inputs must be bound with safe limits, and HTML output escaped by `html/template` by default.

Create a login screen with two demo accounts.

Faculty:
`faculty@educore.demo`
Password: `Faculty123!`

Student:
`student@educore.demo`
Password: `Student123!`

Document these credentials in the README.

Allow faculty to create student accounts and enroll students into their courses. Do not require email delivery.

## 5. Student interface

Create the following pages.

### A. Student dashboard

Show:

- Welcome message and student identity.
- Overall academic performance.
- Subject-wise comprehension.
- Pending assignments.
- Upcoming deadlines.
- Recently evaluated submissions.
- Recent faculty/AI feedback.

Include a meaningful subject mastery visualization.

Every metric must come from actual stored data.

### B. My assignments

Provide a filterable assignment list.

Each assignment must display:

- Assignment name.
- Subject.
- Topic.
- Due date.
- Submission status.
- Marks, when available.

Use the following statuses:

- Not submitted.
- Submitted.
- Evaluated.
- Needs review.
- Overdue.

Make the status visually obvious but not excessively colorful.

### C. Assignment submission

Students must be able to:

- Read the assignment instructions.
- See its rubric and maximum marks.
- Submit an answer through a text editor/textarea.
- Upload PDF, DOCX, or TXT files.
- Replace or update their submission where permitted.
- See successful submission confirmation.

Implement file validation, reasonable file-size limits, safe filenames, and server-side file storage.

Extract usable text from uploaded documents before evaluation. Reject unsupported formats with a clear message.

### D. Performance and feedback

Display:

- Assignment marks.
- Criterion-wise scores.
- Topic mastery.
- Overall feedback.
- Identified strengths.
- Specific learning gaps.
- Recommended next steps.
- Evidence from the submitted answer where available.

Feedback must be understandable and actionable, not generic comments such as "Good work, improve your understanding."

Do not expose another student's data.

## 6. Faculty interface

### A. Faculty dashboard

Build a faculty overview containing:

**Summary metrics**
- Total enrolled students.
- Total published assignments.
- Submission completion rate.
- Average subject comprehension.
- Pending evaluations.
- Overdue/missing submissions.

**Learning analytics**
- Topic-wise mastery chart.
- Assignment performance overview.
- Learning-gap indicators.
- Students requiring attention.

**Submission monitoring**
- Latest submissions.
- Missing work.
- Overdue assessments.
- Evaluations awaiting faculty review.

Allow faculty to navigate from summary information to the relevant student, assignment, or course.

### B. Course management

Faculty must be able to:

- Create a course.
- Edit course details.
- View enrolled students.
- Create student accounts and enroll them.
- View course-level performance.

### C. Assignment management

Faculty must be able to:

- Create an assignment.
- Select its course.
- Define its title and instructions.
- Define its topic.
- Set a deadline.
- Add, remove, and edit rubric criteria.
- Set maximum points for each criterion.
- Publish the assignment.

Validate that rubric points total exactly 100 before publication.

Allow faculty to see submissions for each assignment.

### D. Student analytics

Provide an individual student performance view showing:

- Enrolled subjects.
- Assignment completion.
- Marks history.
- Topic-wise comprehension.
- Topics where the student is struggling.
- Missing or overdue submissions.
- Evaluation feedback.

Faculty should be able to identify a student who is performing poorly in a specific topic without manually inspecting every submission.

### E. Evaluation review

Faculty must be able to:

- Open a submission.
- Read the extracted submission content.
- Inspect criterion-level scores.
- Inspect the AI's evidence and reasoning summary.
- Override individual criterion scores.
- Add faculty feedback.
- Approve/publish the final evaluation.
- Re-evaluate a submission when necessary.

The faculty override must become the authoritative published result.

## 7. Automated evaluation engine

This is the most important backend component.

Implement it as a separate service, not as arbitrary grading logic embedded inside a page or API route.

### Evaluation input

The engine receives:

- Assignment instructions.
- Assignment topic.
- Rubric criteria and maximum points.
- Student submission text.

### Evaluation output

Produce structured results containing:

- Score for each rubric criterion.
- Evidence supporting the score.
- Criterion-specific feedback.
- Overall score.
- Strengths.
- Learning gaps.
- Suggested improvements.
- Confidence level.

### Scoring rules

1. Evaluate against the faculty's rubric, not generic notions of answer quality.
2. The model may suggest individual criterion scores, but the backend must calculate the final total from the validated criterion scores.
3. Never allow a criterion score below zero or above its maximum points.
4. Total score must never exceed 100.
5. Validate the model response with a schema.
6. Reject malformed or incomplete results and retry at most once.
7. Do not invent evidence. Any quoted evidence must be checked against the actual submitted text. Invalid quotations must be removed and flagged for review.
8. Mark low-confidence evaluations as requiring faculty review.
9. Preserve the original AI evaluation when a faculty member overrides the grade.

### AI provider integration

Make the AI integration configurable using environment variables:

- `AI_BASE_URL`
- `AI_API_KEY`
- `AI_MODEL`

Use an OpenAI-compatible client or equivalent adapter.

Never expose the API key to the browser.

The integration must work with compatible external providers without requiring the UI or grading logic to be rewritten.

If no provider credentials are available, the application must still function.

Implement a clearly labeled DEMO evaluation mode using rubric keyword matching and deterministic scoring.

Because there is no client-side framework, do not load data via ad-hoc client-side fetching tricks. Every page's data must come from server-rendered templates fed by real database queries; interactive updates (e.g. faculty score override) use standard HTML form POSTs with server-side redirects, plus small vanilla-JavaScript enhancements where they genuinely help. Make its limitations explicit and mark its results as requiring faculty verification.

Never pretend that a heuristic result was generated by an AI model.

After successful submission, trigger evaluation automatically. If evaluation fails, retain the submission, display an appropriate status, and allow faculty to retry or grade manually.

## 8. Analytics calculations

Do not use arbitrary percentages or hardcoded dashboard values.

Implement shared calculation functions and test them.

### Assignment completion rate

`Submitted submissions / Expected submissions × 100`

Expected submissions are based on published assignments and enrolled students.

### Individual topic mastery

Calculate a student's average evaluated performance for assignments associated with the topic.

Where a topic has multiple assignments, aggregate its scores consistently.

### Subject comprehension

Calculate each student's average across their evaluated assignments in a subject, then average the available student-level scores.

This avoids allowing students with more submissions to disproportionately influence the subject average.

### Important distinction

Never treat missing assignments as zero marks when calculating demonstrated comprehension.

Track these separately:

- Learning performance.
- Submission completion.
- Evaluation coverage.

Display the number of evaluated students/submissions alongside aggregated mastery percentages.

Faculty should be able to distinguish between a subject with genuinely low comprehension and one where very few students have been evaluated.

### Missing submissions

Determine missing work by comparing published assignments, enrollment records, submission records, and deadlines.

An assignment without a submission becomes overdue when its deadline has passed.

Calculate this dynamically. Do not require manual creation of missing-submission records.

Update analytics immediately after a submission or approved evaluation.

## 9. UI/UX design specification

This is a strict design requirement.

Build an actual professional SaaS application, not a generic AI dashboard template.

### Visual identity

Product name: EDUCORE

Design language: Minimal, professional, analytical, approachable.

Use the following palette:

- Primary background: `#F5F7FB`
- Card background: `#FFFFFF`
- Sidebar: `#17243A`
- Main text: `#172033`
- Secondary text: `#64748B`
- Primary accent: `#536FE5`
- Success: `#0F766E`
- Warning: `#B45309`
- Error: `#DC2626`
- Borders: `#E4E8F0`

Use Inter or Geist as the primary font.

### Layout

Desktop:

- Fixed or sticky left sidebar approximately 240–250px wide.
- Main workspace fills the remaining available width.
- Top header around 64–72px tall.
- Consistent content padding of 28–32px.
- Content should have sensible maximum widths.
- Use a predictable grid system.

Mobile:

- Collapsible navigation.
- Responsive cards and tables.
- No horizontal overflow.
- Forms and submission controls must remain usable.

### Component styling

- Cards: white background, subtle border, 10–14px radius, very restrained shadow.
- Buttons: clear primary and secondary variants.
- Inputs: consistent heights, visible labels, clear focus states.
- Tables: aligned columns, readable row spacing, subtle separators.
- Badges: restrained semantic colors.
- Charts: clean axes, useful labels, no unnecessary visual noise.
- Empty states: explain what is missing and provide an action.
- Loading states: skeletons where appropriate.
- Errors: clear, human-readable messages.

Use Lucide icons consistently.

### Visual hierarchy

Each page must have:

1. A clear page heading.
2. A brief contextual description where useful.
3. Its primary action clearly identifiable.
4. Important metrics presented before detailed tables.
5. Secondary information visually subordinate.

Avoid:

- Giant gradient hero banners.
- Excessive rounded containers.
- Random gradients.
- Too many colors.
- Emoji as interface icons.
- Unnecessary animations.
- Overcrowded dashboards.
- Huge empty spaces.
- Fake charts with random numbers.
- Buttons that do nothing.
- Repeated cards that show the same information.

The authenticated application is the product. Do not waste implementation time on a decorative marketing landing page.

## 10. Demo data

Seed the database with realistic and internally consistent sample data.

Create:

- One faculty account.
- At least four student accounts.
- At least two courses/subjects.
- At least three assignments across different topics.
- Rubric criteria for every assignment.
- Several existing submissions.
- Existing evaluations with different scores.
- At least one missing overdue submission.
- At least one submission requiring review.

Generate deadlines relative to the current date so the demonstration remains meaningful.

Ensure all initial dashboard metrics are derived from the seeded database.

The demo should visibly demonstrate differences between good performance, weak topic comprehension, incomplete submissions, and overdue assessments.

Make database seeding repeatable and safe.

## 11. Engineering quality

Mandatory requirements:

- No fake authentication.
- No frontend-only data persistence.
- No hardcoded analytics.
- No arbitrary mock evaluation response in normal provider mode.
- No exposed API secrets.
- No unrestricted file uploads.
- No insecure access to other students' data.
- No broken or decorative-only controls.
- No silent errors.
- No unhandled API failures.
- No large, unmaintainable components containing the whole application.
- No unnecessary dependencies.

Use reusable components and clear naming.

Use proper loading, empty, success, and failure states.

Provide a `.env.example` with all required and optional environment variables (`APP_ADDR`, `APP_DB_PATH`, `AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`, plus any cookie/session flags you need). Load configuration from the environment with a tiny loader in Go (or stdlib + direct `os.Getenv`); no config package is required.

The application must run with plain Go tooling and no infrastructure:

- Build: `go build -o bin/educore .`
- Run: `./bin/educore` (serves on the configured address, default `127.0.0.1:8080`)
- Seed: `./bin/educore seed` or an automatic idempotent seed on first startup
- Tests: `go test ./...`

Provide a useful README with installation, database setup, seeding, demo credentials, AI configuration, and launch instructions.

## 12. Required execution phases

Execute the following phases in order.

### Phase 1 — Repository inspection
- Inspect the existing project.
- Identify the stack and existing functionality.
- Create `BUILD_PROGRESS.md`.
- Record the implementation plan.
- Identify existing problems before changing anything.

### Phase 2 — Foundation
- Complete or repair the database.
- Implement authentication and role protection.
- Create the data models and seed data.
- Verify that both demo accounts can log in.

### Phase 3 — Core workflows
- Implement course creation and management.
- Implement student enrollment.
- Implement assignment and rubric creation.
- Implement text and file submissions.
- Verify actual persistence.

### Phase 4 — Evaluation engine
- Implement the provider adapter.
- Implement validated evaluation output.
- Implement demo fallback mode.
- Implement faculty review and score overrides.
- Verify score calculations.

### Phase 5 — Interfaces
- Build the complete faculty dashboard.
- Build the complete student dashboard.
- Build assignment, submission, evaluation, and analytics views.
- Apply the specified visual design throughout.
- Verify responsive behavior.

### Phase 6 — Integration and testing
Run the complete application.

Test this exact workflow:

1. Log in as faculty.
2. Create a course.
3. Enroll a student.
4. Create and publish an assignment with a rubric.
5. Log in as that student.
6. Submit an answer.
7. Verify that the submission is persisted.
8. Verify that evaluation is triggered.
9. View the student's marks and feedback.
10. Return to faculty.
11. Inspect and approve or override the evaluation.
12. Verify that subject and topic metrics update.
13. Verify that missing/overdue submissions appear correctly.
14. Verify that students cannot access other students' records.
15. Verify that students cannot access faculty routes or actions.

Run available unit tests, integration tests, linting, and production build checks. Fix errors rather than merely reporting them.

If browser automation is already available, use it to exercise the UI. Do not spend excessive time setting up unnecessary testing infrastructure.

### Phase 7 — Final delivery

The application must start using a documented, straightforward command.

Before finishing:

- Verify the database can be initialized.
- Verify the seed command.
- Verify the development server.
- Verify the production build.
- Remove unused components and broken placeholders.
- Document anything that is genuinely incomplete.
- Update `BUILD_PROGRESS.md`.

## 13. Definition of done

The project is not complete just because the dashboard renders.

It is complete when a faculty member can create an assignment, a student can submit an answer, the evaluation system can generate a score and useful diagnostic feedback, the faculty member can review it, and the analytics reflect the resulting assessment data.

The student and faculty experiences must both work.

Prioritize a smaller number of completely working features over a large number of superficial features.

Start by inspecting the current repository. Then execute the phases in order. Do not stop at planning.
