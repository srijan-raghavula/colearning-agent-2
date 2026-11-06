package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type learningAIStub struct {
	calls     int
	system    string
	prompt    string
	response  string
	responses []string
	maxTokens int
	err       error
}

func (s *learningAIStub) Complete(_ context.Context, system, prompt string, maxTokens int) (string, error) {
	s.calls++
	s.system = system
	s.prompt = prompt
	s.maxTokens = maxTokens
	if len(s.responses) >= s.calls {
		return s.responses[s.calls-1], s.err
	}
	return s.response, s.err
}

func TestLearningStudioListsEnrolledPublishedChapters(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")

	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)
	cid := courseIDInt(t, courseID)
	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.CreateChapter(facultyID, cid, "Draft only", "", "HIDDEN_LESSON", 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.CreateChapter(facultyID, cid, "Published chapter", "Published summary", "Lesson material", 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.CreateChapter(facultyID, cid, "Foundations", "First in course order", "Foundational lesson", 1, true); err != nil {
		t.Fatal(err)
	}
	otherCourse, err := deps.Store.CreateCourse(facultyID, "Other course", "CS202", "Another course description")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.CreateChapter(facultyID, otherCourse.ID, "Other course chapter", "", "Other course content", 1, true); err != nil {
		t.Fatal(err)
	}

	body := getPage(t, student, server.URL+"/student/learning-studio")
	if !strings.Contains(body, "Learning Studio") || !strings.Contains(body, "Data Structures") ||
		!strings.Contains(body, "trees graphs") || !strings.Contains(body, "Published chapter") ||
		!strings.Contains(body, "Published summary") || strings.Contains(body, "Other course chapter") ||
		strings.Contains(body, "Draft only") ||
		strings.Contains(body, "HIDDEN_LESSON") {
		t.Fatalf("learning studio course list has unexpected content")
	}
	if strings.Index(body, "Foundations") > strings.Index(body, "Published chapter") {
		t.Fatal("published chapters are not ordered by chapter position")
	}
	if !strings.Contains(body, `href="/student/learning-studio" class="nav-item is-active"`) {
		t.Fatal("student sidebar does not mark Learning Studio as active")
	}
}

func TestLearningTutorLessonAndContextualChat(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")
	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)

	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	chapter, err := deps.Store.CreateChapter(facultyID, courseIDInt(t, courseID),
		"Relational Model, Keys and Integrity Constraints", "Understand database relationships.",
		"Primary keys uniquely identify rows. Foreign keys reference keys in another table.", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	ai := &learningAIStub{response: "## Guided lesson\n\nA primary key uniquely identifies each row."}
	deps.AIOn = true
	deps.LearningAI = ai

	chapterURL := server.URL + "/student/courses/" + courseID + "/chapters/" + url.PathEscape(strings.TrimSpace(strings.TrimSpace(stringID(chapter.ID))))
	page := getPage(t, student, chapterURL)
	if !strings.Contains(page, "Data Structures") || !strings.Contains(page, "Understand database relationships.") ||
		!strings.Contains(page, "Primary keys uniquely identify rows") || !strings.Contains(page, `id="tutorForm"`) ||
		!strings.Contains(page, `href="/student/learning-studio" class="nav-item is-active"`) {
		t.Fatal("learning page is missing course/chapter content or tutor interface")
	}
	csrf := extractCSRF(page)
	lessonURL := chapterURL + "/lesson"
	response := postTutorJSON(t, student, lessonURL, csrf, `{}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body, "Guided lesson") {
		t.Fatalf("lesson response status=%d body=%s", response.Code, response.Body)
	}
	if ai.calls != 1 || !strings.Contains(ai.system, "Never invent facts") ||
		!strings.Contains(ai.prompt, "Primary keys uniquely identify rows") ||
		!strings.Contains(ai.prompt, "Relational Model, Keys and Integrity Constraints") {
		t.Fatalf("AI lesson did not receive chapter context: calls=%d prompt=%q", ai.calls, ai.prompt)
	}

	chatURL := chapterURL + "/chat"
	chatBody := `{"question":"Explain foreign keys","history":[{"role":"user","content":"What is a primary key?"},{"role":"assistant","content":"A key that uniquely identifies a row."}]}`
	response = postTutorJSON(t, student, chatURL, csrf, chatBody)
	if response.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", response.Code, response.Body)
	}
	if !strings.Contains(ai.prompt, "What is a primary key?") ||
		!strings.Contains(ai.prompt, "A key that uniquely identifies a row.") ||
		!strings.Contains(ai.prompt, "Explain foreign keys") ||
		!strings.Contains(ai.prompt, "Foreign keys reference keys") {
		t.Fatalf("chat prompt omitted chapter or conversation context: %q", ai.prompt)
	}
	if ai.maxTokens < 2000 {
		t.Fatalf("chat output token budget = %d, want at least 2000", ai.maxTokens)
	}

	response = postTutorJSON(t, student, chatURL, "", `{"question":"No CSRF"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("chat without CSRF status=%d", response.Code)
	}
	response = postTutorJSON(t, student, chatURL, csrf, `{"question":" ","history":[]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("empty chat question status=%d", response.Code)
	}
}

func TestLearningTutorRejectsDraftAndUnenrolledAccess(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")
	courseID := createCourse(t, server.URL, faculty)
	cid := courseIDInt(t, courseID)
	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	draft, err := deps.Store.CreateChapter(facultyID, cid, "Draft", "", "Secret draft", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	ai := &learningAIStub{response: "Must not be called"}
	deps.AIOn = true
	deps.LearningAI = ai
	csrf := extractCSRF(getPage(t, student, server.URL+"/student"))
	draftURL := server.URL + "/student/courses/" + courseID + "/chapters/" + stringID(draft.ID) + "/lesson"
	resp := postTutorJSON(t, student, draftURL, csrf, `{}`)
	if resp.Code != http.StatusNotFound || ai.calls != 0 {
		t.Fatalf("draft access status=%d calls=%d", resp.Code, ai.calls)
	}

	if _, err := deps.Store.CreateChapter(facultyID, cid, "Published", "", "Public lesson", 2, true); err != nil {
		t.Fatal(err)
	}
	var publishedID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM course_chapters WHERE course_id = ? AND title = 'Published'`, cid).Scan(&publishedID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Auth.CreateAccount("unenrolled@educore.demo", "Unenrolled Student", "Student456!", "STUDENT"); err != nil {
		t.Fatal(err)
	}
	otherStudent := client(t, false)
	loginAs(t, otherStudent, server.URL, "unenrolled@educore.demo", "Student456!")
	publishedURL := server.URL + "/student/courses/" + courseID + "/chapters/" + stringID(publishedID) + "/lesson"
	otherCSRF := extractCSRF(getPage(t, otherStudent, server.URL+"/student"))
	resp = postTutorJSON(t, otherStudent, publishedURL, otherCSRF, `{}`)
	if resp.Code != http.StatusNotFound || ai.calls != 0 {
		t.Fatalf("unenrolled access status=%d calls=%d", resp.Code, ai.calls)
	}
}

func TestLearningTutorProviderFailureIsVisibleAndSafe(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")
	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)
	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	chapter, err := deps.Store.CreateChapter(facultyID, courseIDInt(t, courseID), "Chapter", "", "Material", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	deps.AIOn = true
	deps.LearningAI = &learningAIStub{err: errors.New("provider detail and key should not be returned")}
	pageURL := server.URL + "/student/courses/" + courseID + "/chapters/" + stringID(chapter.ID)
	page := getPage(t, student, pageURL)
	resp := postTutorJSON(t, student, pageURL+"/lesson", extractCSRF(page), `{}`)
	if resp.Code != http.StatusBadGateway || strings.Contains(resp.Body, "provider detail") {
		t.Fatalf("provider failure response leaked details or status: %d %s", resp.Code, resp.Body)
	}

	deps.LearningAI = nil
	resp = postTutorJSON(t, student, pageURL+"/lesson", extractCSRF(page), `{}`)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured AI status=%d", resp.Code)
	}
}

func TestParseChapterQuizAcceptsJSONWithProseOrFence(t *testing.T) {
	jsonQuiz := `{"questions":[{"question":"Q1?","options":["A","B","C","D"],"answer_index":0,"explanation":"A is right."},{"question":"Q2?","options":["A","B","C","D"],"answer_index":1,"explanation":"B is right."},{"question":"Q3?","options":["A","B","C","D"],"answer_index":2,"explanation":"C is right."}]}`
	for _, response := range []string{
		jsonQuiz,
		"Here is your quiz:\n```json\n" + jsonQuiz + "\n```\nGood luck!",
	} {
		if _, err := parseChapterQuiz(response); err != nil {
			t.Fatalf("parse quiz response %q: %v", response, err)
		}
	}
}

func TestLearningChapterQuizScoresAnswersOnServer(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")
	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)
	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	chapter, err := deps.Store.CreateChapter(facultyID, courseIDInt(t, courseID),
		"Keys", "Identify and connect records.", "A primary key uniquely identifies each row. A foreign key references a key in a related table.", 1, true)
	if err != nil {
		t.Fatal(err)
	}
	quizJSON := `{"questions":[{"question":"What does a primary key do?","options":["Identifies a row uniquely","Stores a password","Sorts every table","Deletes duplicate tables"],"answer_index":0,"explanation":"A primary key uniquely identifies each row."},{"question":"What does a foreign key reference?","options":["A key in a related table","A CSS class","A database server","An index name"],"answer_index":0,"explanation":"A foreign key refers to a key in a related table."},{"question":"Which rule helps keep references valid?","options":["Foreign-key constraint","Font size","Table color","Sorting order"],"answer_index":0,"explanation":"Foreign-key constraints require referenced records to exist."}]}`
	ai := &learningAIStub{responses: []string{quizJSON}}
	deps.AIOn = true
	deps.LearningAI = ai
	pageURL := server.URL + "/student/courses/" + courseID + "/chapters/" + stringID(chapter.ID)
	page := getPage(t, student, pageURL)
	csrf := extractCSRF(page)
	response := postTutorJSON(t, student, pageURL+"/quiz", csrf, `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("quiz generation status=%d body=%s", response.Code, response.Body)
	}
	var generated struct {
		QuizID    string               `json:"quiz_id"`
		Questions []publicQuizQuestion `json:"questions"`
	}
	if err := json.Unmarshal([]byte(response.Body), &generated); err != nil {
		t.Fatal(err)
	}
	if generated.QuizID == "" || len(generated.Questions) != 3 ||
		strings.Contains(response.Body, "answer_index") || strings.Contains(response.Body, `"explanation"`) {
		t.Fatalf("quiz output exposes answer key or has unexpected shape: %s", response.Body)
	}
	submit := `{"quiz_id":"` + generated.QuizID + `","answers":[0,1,0]}`
	response = postTutorJSON(t, student, pageURL+"/quiz/submit", csrf, submit)
	if response.Code != http.StatusOK || !strings.Contains(response.Body, `"score":2`) ||
		!strings.Contains(response.Body, `"total":3`) || !strings.Contains(response.Body, `"correct":false`) {
		t.Fatalf("quiz scoring status=%d body=%s", response.Code, response.Body)
	}
	response = postTutorJSON(t, student, pageURL+"/quiz/submit", csrf,
		`{"quiz_id":"`+generated.QuizID+`","answers":[0,0,0]}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body, `"score":3`) {
		t.Fatalf("repeated quiz attempt status=%d body=%s", response.Code, response.Body)
	}
}

func postTutorJSON(t *testing.T, client *http.Client, target, csrf, body string) *httpResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return &httpResponse{Code: resp.StatusCode, Body: string(payload)}
}

type httpResponse struct {
	Code int
	Body string
}

func stringID(id int64) string {
	return strconv.FormatInt(id, 10)
}
