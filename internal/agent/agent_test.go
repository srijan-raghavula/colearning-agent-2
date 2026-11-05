package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"educore/internal/database"
	"educore/internal/evaluator"
	"educore/internal/store"
)

// ---- test harness ---------------------------------------------------------

type fakeModel struct {
	server *httptest.Server
	calls  int32
	script []func(r *http.Request) string // one entry per model turn
}

func newFakeModel(t *testing.T, script []func(r *http.Request) string) (*fakeModel, *evaluator.Provider) {
	t.Helper()
	fm := &fakeModel{script: script}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&fm.calls, 1))
		if i > len(fm.script) {
			http.Error(w, `{"error":{"message":"script exhausted"}}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Scripts return the assistant MESSAGE; wrap into the chat-completions envelope.
		envelope := map[string]any{"choices": []map[string]any{{"message": json.RawMessage(fm.script[i-1](r))}}}
		out, _ := json.Marshal(envelope)
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	fm.server = srv
	return fm, &evaluator.Provider{BaseURL: srv.URL, APIKey: "sk-test", Model: "deepseek-test"}
}

func toolCallJSON(id, name string, args map[string]any) string {
	aargs, _ := json.Marshal(args)
	msg := map[string]any{
		"role": "assistant",
		"tool_calls": []map[string]any{{
			"id":       id,
			"type":     "function",
			"function": map[string]any{"name": name, "arguments": string(aargs)},
		}},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func textJSON(text string) string {
	m := map[string]any{"role": "assistant", "content": text}
	b, _ := json.Marshal(m)
	return string(b)
}

func mustScriptJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// fixture: faculty + student + published assignment with 2 criteria + submission.
func agentFixture(t *testing.T) (*store.Store, int64, int64, int64) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/agent.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close(db) })
	st := store.New(db)
	var fac, stu, aid, subID int64
	_ = st.DB.QueryRow(`INSERT INTO users (email,name,password_hash,role) VALUES ('prof@x.edu','Prof X','x','FACULTY') RETURNING id`).Scan(&fac)
	_ = st.DB.QueryRow(`INSERT INTO users (email,name,password_hash,role) VALUES ('stu@x.edu','Stu Y','x','STUDENT') RETURNING id`).Scan(&stu)
	var cid int64
	_ = st.DB.QueryRow(`INSERT INTO courses (faculty_id,name,code,description) VALUES (?,'Compilers','C1','d') RETURNING id`, fac).Scan(&cid)
	_ = st.Enroll(cid, stu, fac)
	due := time.Now().UTC().Add(48 * time.Hour).Format(store.TimeLayout)
	_ = st.DB.QueryRow(`INSERT INTO assignments (course_id,title,description,topic,due_date,published,total_marks)
		VALUES (?,'Parser Basics','Explain recursive descent parsers.','Parsing',?,1,100) RETURNING id`, cid, due).Scan(&aid)
	_, _ = st.DB.Exec(`INSERT INTO rubric_criteria (assignment_id,position,title,description,max_points) VALUES (?,1,'Grammar','correct grammar rules',60)`, aid)
	_, _ = st.DB.Exec(`INSERT INTO rubric_criteria (assignment_id,position,title,description,max_points) VALUES (?,2,'Complexity','limits',40)`, aid)
	_, _ = st.DB.Exec(`INSERT INTO submissions (assignment_id,student_id,content,status,submitted_at) VALUES (?,?,'Recursive descent uses one function per non-terminal; LLVM is a compiler.','SUBMITTED',?)`, aid, stu, store.NowUTC())
	_ = st.DB.QueryRow(`SELECT id FROM submissions WHERE assignment_id = ? AND student_id = ?`, aid, stu).Scan(&subID)
	return st, fac, stu, subID
}

func debugEvents(t *testing.T, st *store.Store, runID int64) []*store.AgentEvent {
	t.Helper()
	ev, err := st.AgentEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}
