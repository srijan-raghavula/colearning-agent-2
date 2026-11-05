# EDUCORE — Academic Assessment Intelligence

A lightweight, server-rendered academic platform: faculty author rubric-based
assignments, students submit work, an automated evaluation engine (DEMO
deterministic rubric-keyword mode by default, optional OpenAI-compatible provider)
produces structured diagnostic results, and faculty review, override and approve
final marks.

## Stack

Go (single binary) · `net/http` · `html/template` · SQLite (`modernc.org/sqlite`,
CGO-free) · custom CSS · minimal vanilla JS · server-generated SVG charts.

## Run

```bash
go build -o bin/educore .
./bin/educore            # http://127.0.0.1:8080 (auto-migrates + seeds a fresh DB)
./bin/educore seed       # idempotent showcase dataset, safe to re-run
go test ./...            # full suite
```

Configuration via environment (see `.env.example`): `APP_ADDR`,
`APP_DB_PATH`, `AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`. Without AI credentials
the engine runs in clearly-labeled DEMO evaluation mode.

### Demo credentials

| Role | Email | Password |
|---|---|---|
| Faculty | `faculty@educore.demo` | `Faculty123!` |
| Student (rich dashboard) | `student@educore.demo` | `Student123!` |
| More students | `meera.iyer@`, `rohan.verma@`, `sara.khan@`, `dev.patel@`, `anaya.rao@`, `kabir.nair@`, `ishita.sen@` `@educore.demo` | `Student123!` |

Seeded students vary from strong to weak topic mastery so every dashboard,
attention list and chart is populated out of the box.

## Feature map

- **Faculty** — analytics Overview (topic mastery, completion, attention list,
  activity, deadlines), Courses (create/edit/enroll), Assignments (rubric
  builder, publish gate at exactly 100 points, submission monitoring),
  Students (create/enroll), Evaluations (review workspace with evidence,
  overrides with audit trail, approve/unpublish, re-evaluate, manual grading).
- **Student** — Overview dashboard (approved marks, upcoming assignments,
  approved results, learning insights), My Courses, My Assignments
  (submit text/PDF/DOCX/TXT, resubmission), Performance page
  (subject bars, topic bars, approved trend line, gaps & strengths).

Only faculty-approved evaluations count as published marks anywhere.
