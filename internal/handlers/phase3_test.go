package handlers

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"
)

const phaseCourseDue = "2026-12-01T10:00"

func newApp(t *testing.T) (*httptest.Server, *http.Client, *http.Client) {
	_, server := newDeps(t)
	fc := client(t, false)
	loginAs(t, fc, server.URL, "faculty@educore.demo", "Faculty123!")
	sc := client(t, false)
	loginAs(t, sc, server.URL, "student@educore.demo", "Student123!")
	return server, fc, sc
}

func createCourse(t *testing.T, base string, c *http.Client) string {
	t.Helper()
	csrf := extractCSRF(getPage(t, c, base+"/faculty/courses/new"))
	resp := postForm(t, c, base+"/faculty/courses", url.Values{
		"_csrf": {csrf}, "name": {"Data Structures"}, "code": {"CS201"}, "description": {"trees graphs"},
	})
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.Contains(loc, "/faculty/courses/") || strings.Contains(loc, "?err") {
		t.Fatalf("course create failed: %s", loc)
	}
	id := loc[strings.LastIndex(loc, "/")+1:]
	return id[:strings.Index(id, "?")]
}

func getPage(t *testing.T, c *http.Client, path string) string {
	t.Helper()
	resp, err := c.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	return readBody(t, resp)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func createAssignment(t *testing.T, base string, fc *http.Client, courseID, due string, points ...int) string {
	t.Helper()
	csrf := extractCSRF(getPage(t, fc, base+"/faculty/courses/"+courseID+"/assignments/new"))
	form := url.Values{
		"_csrf": {csrf}, "title": {"Graph Traversal"}, "description": {"BFS/DFS"},
		"topic": {"Graphs"}, "due_date": {due}, "total_marks": {"100"},
	}
	var order []string
	for i := 0; i < len(points); i++ {
		idx := strings.Repeat("k", i) + "n"
		order = append(order, idx)
		form.Set("crit_title_"+idx, "Criterion")
		form.Set("crit_description_"+idx, "desc")
		form.Set("crit_points_"+idx, itoa(points[i]))
	}
	form.Set("criteria_order", strings.Join(order, ","))
	resp := postForm(t, fc, base+"/faculty/courses/"+courseID+"/assignments", form)
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	return loc
}

func TestFacultyOnlyEndpoints(t *testing.T) {
	server, _, sc := newApp(t)
	resp, err := sc.Get(server.URL + "/faculty/courses")
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student reached faculty area: %s", resp.Header.Get("Location"))
	}
}

func TestStudentSeesOnlyEnrolledCourses(t *testing.T) {
	server, fc, _ := newApp(t)
	cid := createCourse(t, server.URL, fc)
	sc := client(t, false)
	loginAs(t, sc, server.URL, "student@educore.demo", "Student123!")
	body := getPage(t, sc, server.URL+"/student/courses")
	if strings.Contains(body, "Data Structures") {
		t.Fatalf("unenrolled student sees the course")
	}
	resp, err := sc.Get(server.URL + "/student/courses/" + cid)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("un-enrolled direct course access: %d", resp.StatusCode)
	}
}

func TestRubricPublicationGate(t *testing.T) {
	server, fc, _ := newApp(t)
	cid := createCourse(t, server.URL, fc)

	loc := createAssignment(t, server.URL, fc, cid, phaseCourseDue, 60, 30)
	aid := loc[strings.LastIndex(loc, "/")+1:]
	csrf := extractCSRF(getPage(t, fc, server.URL+"/faculty/assignments/"+aid))
	resp := postForm(t, fc, server.URL+"/faculty/assignments/"+aid+"/publish", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "err=") {
		t.Fatalf("publish with rubric 90 succeeded: %s", loc)
	}
	// Fix the rubric to 100.
	csrf = extractCSRF(getPage(t, fc, server.URL+"/faculty/assignments/"+aid))
	resp = postForm(t, fc, server.URL+"/faculty/assignments/"+aid+"/rubric", url.Values{
		"_csrf": {csrf}, "criteria_order": {"1,2"},
		"crit_title_1": {"Correctness"}, "crit_description_1": {"d"}, "crit_points_1": {"60"},
		"crit_title_2": {"Style"}, "crit_description_2": {"d"}, "crit_points_2": {"40"},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "publication-ready") {
		t.Fatalf("rubric 100 not accepted: %s", loc)
	}
	csrf = extractCSRF(getPage(t, fc, server.URL+"/faculty/assignments/"+aid))
	resp = postForm(t, fc, server.URL+"/faculty/assignments/"+aid+"/publish", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "published") {
		t.Fatalf("publish with rubric 100 failed: %s", loc)
	}
}

func makeDocxBytes(t *testing.T) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="urn:x"><w:body><w:p><w:r><w:t>DOCX payload text</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	return buf.Bytes()
}

func uploadReq(t *testing.T, base, path, csrf, fileField, fileName string, fileBytes []byte, contentType string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("_csrf", csrf)
	_ = mw.WriteField("content", "")
	if fileName != "" {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="`+fileField+`"; filename="`+fileName+`"`)
		if contentType != "" {
			hdr.Set("Content-Type", contentType)
		}
		part, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(fileBytes)
	}
	_ = mw.Close()
	req, err := http.NewRequest("POST", base+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func enrollSeedStudent(t *testing.T, base string, fc *http.Client, courseID string) {
	t.Helper()
	body := getPage(t, fc, base+"/faculty/courses/"+courseID)
	csrf := extractCSRF(body)
	resp := postForm(t, fc, base+"/faculty/courses/"+courseID+"/students", url.Values{
		"_csrf": {csrf}, "student_id": {}, "email": {"student@educore.demo"},
	})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("enroll failed: %s", loc)
	}
}

func TestFullSubmissionFlow(t *testing.T) {
	server, fc, sc := newApp(t)
	cid := createCourse(t, server.URL, fc)
	enrollSeedStudent(t, server.URL, fc, cid)
	due := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02T15:04")
	loc := createAssignment(t, server.URL, fc, cid, due, 60, 40)
	aid := loc[strings.LastIndex(loc, "/")+1:]
	publishAssignment(t, server.URL, fc, aid)

	body := getPage(t, sc, server.URL+"/student/assignments/"+aid)
	if !strings.Contains(body, "Graph Traversal") || !strings.Contains(body, "Grading rubric") {
		t.Fatalf("student assignment view broken")
	}
	csrf := extractCSRF(body)

	resp := postForm(t, sc, server.URL+"/student/assignments/"+aid+"/submit", url.Values{
		"_csrf": {csrf}, "content": {"BFS uses a queue, DFS uses a stack."},
	})
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Location"), "received") {
		t.Fatalf("text submission failed: %s", resp.Header.Get("Location"))
	}

	// DOCX replacement submission.
	req := uploadReq(t, server.URL, "/student/assignments/"+aid+"/submit", csrf,
		"document", "answer.docx", makeDocxBytes(t), "application/zip")
	resp2, err := sc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp2)
	if !strings.Contains(resp2.Header.Get("Location"), "received") {
		t.Fatalf("docx submission failed: %s", resp2.Header.Get("Location"))
	}

	// Invalid format rejected.
	req = uploadReq(t, server.URL, "/student/assignments/"+aid+"/submit", csrf,
		"document", "evil.exe", []byte("MZ anything"), "application/octet-stream")
	resp3, err := sc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp3)
	if !strings.Contains(resp3.Header.Get("Location"), "err=") {
		t.Fatalf(".exe upload accepted: %s", resp3.Header.Get("Location"))
	}

	// Faculty roster shows the submitted student.
	page := getPage(t, fc, server.URL+"/faculty/assignments/"+aid+"/submissions")
	if !strings.Contains(page, "Arjun Mehta") {
		t.Fatalf("roster missing student")
	}

	// Faculty downloads the stored attachment.
	dl := strings.Index(page, "/faculty/submissions/")
	if dl < 0 {
		t.Fatalf("no submission link on roster")
	}
	sid := page[dl+len("/faculty/submissions/"):]
	sid = sid[:strings.Index(sid, `"`)]
	respDL, err := fc.Get(server.URL + "/faculty/submissions/" + sid + "/download")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(respDL.Body)
	respDL.Body.Close()
	if respDL.StatusCode != 200 || len(data) == 0 || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("download failed: %d len=%d", respDL.StatusCode, len(data))
	}
}

func TestUnauthorizedDownload(t *testing.T) {
	server, _, _ := newApp(t)
	sc := client(t, false)
	loginAs(t, sc, server.URL, "student@educore.demo", "Student123!")
	resp, err := sc.Get(server.URL + "/faculty/submissions/1/download")
	if err != nil {
		t.Fatal(err)
	}
	readBody(t, resp)
	if resp.Header.Get("Location") != "/student" {
		t.Fatalf("student reached faculty download: %s", resp.Header.Get("Location"))
	}
	anon := client(t, false)
	resp, _ = anon.Get(server.URL + "/faculty/submissions/1/download")
	readBody(t, resp)
	if resp.Header.Get("Location") != "/login?next=/faculty" {
		t.Fatalf("anonymous download allowed: %s", resp.Header.Get("Location"))
	}
}

func publishAssignment(t *testing.T, base string, fc *http.Client, aid string) {
	t.Helper()
	csrf := extractCSRF(getPage(t, fc, base+"/faculty/assignments/"+aid))
	resp := postForm(t, fc, base+"/faculty/assignments/"+aid+"/publish", url.Values{"_csrf": {csrf}})
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("publish failed: %s", loc)
	}
}

func TestDbgAssignmentView(t *testing.T) {
	server, fc, sc := newApp(t)
	cid := createCourse(t, server.URL, fc)
	enrollSeedStudent(t, server.URL, fc, cid)
	due := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02T15:04")
	loc := createAssignment(t, server.URL, fc, cid, due, 60, 40)
	t.Logf("loc=%q", loc)
	aid := loc[strings.LastIndex(loc, "/")+1:]
	body := getPage(t, sc, server.URL+"/student/assignments/"+aid)
	t.Logf("body len=%d contains Graph=%v contains rubric=%v contains forbidden=%v loc=%q",
		len(body), strings.Contains(body, "Graph Traversal"), strings.Contains(body, "Grading rubric"),
		strings.Contains(body, "not enrolled"), body[:min(200, len(body))])
	_ = fc
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
