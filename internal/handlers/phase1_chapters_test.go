package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"educore/internal/store"
)

func TestFacultyChapterLifecycleAndStudentVisibility(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")

	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)
	coursePage := getPage(t, faculty, server.URL+"/faculty/courses/"+courseID)
	if !strings.Contains(coursePage, "Manage chapters") {
		t.Fatal("faculty course page does not link to chapter management")
	}
	listURL := server.URL + "/faculty/courses/" + courseID + "/chapters"
	newURL := listURL + "/new"
	if body := getPage(t, faculty, newURL); !strings.Contains(body, "Learning content") || !strings.Contains(body, `name="_csrf"`) {
		t.Fatalf("new chapter form missing content field or CSRF token")
	}

	csrf := extractCSRF(getPage(t, faculty, newURL))
	resp := postForm(t, faculty, listURL, url.Values{
		"_csrf": {csrf}, "title": {"Graph traversal"}, "description": {"Search strategies"},
		"content": {"## Breadth-first search\n\nUses a queue."}, "position": {"2"}, "intent": {"draft"},
	})
	location := resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || location != "/faculty/courses/"+courseID+"/chapters?ok=Chapter+saved+as+a+draft." {
		t.Fatalf("create draft redirect: status=%d location=%q", resp.StatusCode, location)
	}
	var chapterIDNum int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM course_chapters WHERE course_id = ? AND title = ?`,
		courseIDInt(t, courseID), "Graph traversal").Scan(&chapterIDNum); err != nil {
		t.Fatal(err)
	}
	chapterID := strconv.FormatInt(chapterIDNum, 10)

	list := getPage(t, faculty, listURL)
	if !strings.Contains(list, "Graph traversal") || !strings.Contains(list, "Draft") {
		t.Fatalf("faculty chapter list does not show saved draft")
	}
	if chapters, err := deps.Store.PublishedChaptersForStudent(courseIDInt(t, courseID), studentID(t, deps)); err != nil || len(chapters) != 0 {
		t.Fatalf("student can see draft chapter: chapters=%+v err=%v", chapters, err)
	}

	editURL := listURL + "/" + chapterID
	edit := getPage(t, faculty, editURL)
	csrf = extractCSRF(edit)
	resp = postForm(t, faculty, editURL, url.Values{
		"_csrf": {csrf}, "title": {"Graph traversal"}, "description": {"Search strategies"},
		"content": {"## Breadth-first search\n\nUses a queue."}, "position": {"2"}, "intent": {"publish"},
	})
	location = resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(location, "/faculty/courses/"+courseID+"/chapters?ok=") {
		t.Fatalf("publish redirect: status=%d location=%q", resp.StatusCode, location)
	}
	if chapter, err := deps.Store.PublishedChapterForStudent(courseIDInt(t, courseID), chapterIDNum, studentID(t, deps)); err != nil || chapter.Title != "Graph traversal" {
		t.Fatalf("published chapter unavailable to enrolled student: chapter=%+v err=%v", chapter, err)
	}

	// Choosing "Save as draft" explicitly removes a previously published chapter
	// from the student-facing store query.
	csrf = extractCSRF(getPage(t, faculty, editURL))
	resp = postForm(t, faculty, editURL, url.Values{
		"_csrf": {csrf}, "title": {"Graph traversal"}, "description": {"Search strategies"},
		"content": {"Updated lesson"}, "position": {"2"}, "intent": {"draft"},
	})
	location = resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || location != "/faculty/courses/"+courseID+"/chapters?ok=Chapter+saved+as+a+draft." {
		t.Fatalf("update draft redirect: status=%d location=%q", resp.StatusCode, location)
	}
	if chapters, err := deps.Store.PublishedChaptersForStudent(courseIDInt(t, courseID), studentID(t, deps)); err != nil || len(chapters) != 0 {
		t.Fatalf("student can see chapter after it was returned to draft: chapters=%+v err=%v", chapters, err)
	}

	csrf = extractCSRF(getPage(t, faculty, editURL))
	resp = postForm(t, faculty, editURL+"/delete", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Location"), "/faculty/courses/"+courseID+"/chapters?ok=") {
		t.Fatalf("delete redirect: %q", resp.Header.Get("Location"))
	}
	if _, err := deps.Store.ChapterForFaculty(courseIDInt(t, courseID), chapterIDNum, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted chapter still accessible: %v", err)
	}
}

func TestStudentCourseChaptersShowPublishedContentOnly(t *testing.T) {
	deps, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	student := client(t, false)
	loginAs(t, student, server.URL, "student@educore.demo", "Student123!")

	courseID := createCourse(t, server.URL, faculty)
	enrollSeedStudent(t, server.URL, faculty, courseID)
	courseIDValue := courseIDInt(t, courseID)
	var facultyID int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	draft, err := deps.Store.CreateChapter(facultyID, courseIDValue, "Private draft", "", "DRAFT_SECRET_CONTENT", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	published, err := deps.Store.CreateChapter(facultyID, courseIDValue, "Published lesson", "Public lesson description", "Published lesson content", 2, true)
	if err != nil {
		t.Fatal(err)
	}

	coursePageURL := server.URL + "/student/courses/" + courseID
	body := getPage(t, student, coursePageURL)
	if !strings.Contains(body, "Published lesson") || strings.Contains(body, "Private draft") || strings.Contains(body, "DRAFT_SECRET_CONTENT") {
		t.Fatalf("student course page shows draft or hides published chapter")
	}
	if !strings.Contains(body, `href="/student/learning-studio">✦ Learning Studio</a>`) {
		t.Fatal("student course overview does not link to Learning Studio")
	}
	chapterURL := coursePageURL + "/chapters/" + strconv.FormatInt(published.ID, 10)
	body = getPage(t, student, chapterURL)
	if !strings.Contains(body, "Published lesson content") || !strings.Contains(body, "<h1>Published lesson</h1>") {
		t.Fatalf("student cannot view published chapter content")
	}
	if !strings.Contains(body, "Quick knowledge check") || !strings.Contains(body, `id="quickQuiz"`) {
		t.Fatal("student chapter page does not include the interactive quiz")
	}
	if !strings.Contains(body, "Public lesson description") || !strings.Contains(body, "CS201 · Data Structures") {
		t.Fatalf("student chapter page does not show chapter description and course context")
	}
	headerStart := strings.Index(body, `<header class="app-header">`)
	headerEnd := strings.Index(body, "</header>")
	if headerStart < 0 || headerEnd < headerStart ||
		!strings.Contains(body[headerStart:headerEnd], "<h1>Published lesson</h1>") ||
		strings.Contains(body[headerStart:headerEnd], "Public lesson description") {
		t.Fatalf("student chapter top bar does not show only the chapter title")
	}
	resp, err := student.Get(coursePageURL + "/chapters/" + strconv.FormatInt(draft.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("student draft chapter status=%d, want 404", resp.StatusCode)
	}

	// A student without an enrollment cannot see chapters on the course page or
	// load an otherwise-published chapter by direct URL.
	if _, err := deps.Auth.CreateAccount("unenrolled@educore.demo", "Unenrolled Student", "Student456!", "STUDENT"); err != nil {
		t.Fatal(err)
	}
	unEnrolled := client(t, false)
	loginAs(t, unEnrolled, server.URL, "unenrolled@educore.demo", "Student456!")
	body = getPage(t, unEnrolled, coursePageURL)
	if strings.Contains(body, "Published lesson") {
		t.Fatal("unenrolled student saw a course chapter")
	}
	resp, err = unEnrolled.Get(chapterURL)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unenrolled student chapter status=%d, want 404", resp.StatusCode)
	}
}

func TestFacultyChapterOwnershipAndCSRF(t *testing.T) {
	deps, server := newDeps(t)
	owner := client(t, false)
	loginAs(t, owner, server.URL, "faculty@educore.demo", "Faculty123!")
	courseID := createCourse(t, server.URL, owner)
	courseIDValue := courseIDInt(t, courseID)
	facultyID := int64(0)
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'faculty@educore.demo'`).Scan(&facultyID); err != nil {
		t.Fatal(err)
	}
	chapter, err := deps.Store.CreateChapter(facultyID, courseIDValue, "Private draft", "", "Lesson", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Auth.CreateAccount("other-faculty@educore.demo", "Other Faculty", "Faculty456!", "FACULTY"); err != nil {
		t.Fatal(err)
	}
	other := client(t, false)
	loginAs(t, other, server.URL, "other-faculty@educore.demo", "Faculty456!")
	listURL := server.URL + "/faculty/courses/" + courseID + "/chapters"
	resp, err := other.Get(listURL)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign faculty chapter list status=%d", resp.StatusCode)
	}
	editURL := listURL + "/" + strconv.FormatInt(chapter.ID, 10)
	resp, err = other.Get(editURL)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign faculty chapter edit status=%d", resp.StatusCode)
	}

	csrf := extractCSRF(getPage(t, other, server.URL+"/faculty"))
	resp = postForm(t, other, editURL, url.Values{
		"_csrf": {csrf}, "title": {"Hijacked"}, "description": {""},
		"content": {"Unauthorized change"}, "position": {"1"}, "intent": {"publish"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "err=") {
		t.Fatalf("foreign faculty update was not rejected: status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	got, err := deps.Store.ChapterForFaculty(courseIDValue, chapter.ID, facultyID)
	if err != nil || got.Title != "Private draft" || got.Published {
		t.Fatalf("foreign faculty changed chapter: chapter=%+v err=%v", got, err)
	}

	resp = postForm(t, owner, listURL, url.Values{
		"title": {"No CSRF"}, "content": {"No CSRF"}, "position": {"1"}, "intent": {"draft"},
	})
	readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("chapter create without CSRF status=%d", resp.StatusCode)
	}
}

func TestFacultyChapterRejectsInvalidPublication(t *testing.T) {
	_, server := newDeps(t)
	faculty := client(t, false)
	loginAs(t, faculty, server.URL, "faculty@educore.demo", "Faculty123!")
	courseID := createCourse(t, server.URL, faculty)
	listURL := server.URL + "/faculty/courses/" + courseID + "/chapters"
	newURL := listURL + "/new"
	csrf := extractCSRF(getPage(t, faculty, newURL))
	resp := postForm(t, faculty, listURL, url.Values{
		"_csrf": {csrf}, "title": {"Empty lesson"}, "description": {""},
		"content": {""}, "position": {"1"}, "intent": {"publish"},
	})
	location := resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(location, "err=") {
		t.Fatalf("empty published chapter accepted: status=%d location=%q", resp.StatusCode, location)
	}
	csrf = extractCSRF(getPage(t, faculty, newURL))
	resp = postForm(t, faculty, listURL, url.Values{
		"_csrf": {csrf}, "title": {"Ready lesson"}, "description": {""},
		"content": {"Ready lesson content."}, "position": {"1"}, "intent": {"publish"},
	})
	location = resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || location != "/faculty/courses/"+courseID+"/chapters?ok=Chapter+published." {
		t.Fatalf("published chapter did not return to manage list: status=%d location=%q", resp.StatusCode, location)
	}
}

func courseIDInt(t *testing.T, value string) int64 {
	t.Helper()
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func studentID(t *testing.T, deps *Deps) int64 {
	t.Helper()
	var id int64
	if err := deps.Store.DB.QueryRow(`SELECT id FROM users WHERE email = 'student@educore.demo'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
