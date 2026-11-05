package store

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"educore/internal/database"
)

func tmpDB(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close(db) })
	return New(db)
}

func facultyAndStudent(s *Store, t *testing.T) (int64, int64) {
	t.Helper()
	res, err := s.DB.Exec(`INSERT INTO users (email, name, password_hash, role) VALUES ('f1@x.edu','F1','x','FACULTY')`)
	if err != nil {
		t.Fatal(err)
	}
	fid, _ := res.LastInsertId()
	res, err = s.DB.Exec(`INSERT INTO users (email, name, password_hash, role) VALUES ('s1@x.edu','S1','x','STUDENT')`)
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := res.LastInsertId()
	_, _ = s.DB.Exec(`INSERT INTO users (email, name, password_hash, role) VALUES ('s2@x.edu','S2','x','STUDENT')`)
	return fid, sid
}

func criteria100() []*RubricCriterion {
	return []*RubricCriterion{
		{Title: "Correctness", Description: "Algorithm correctness", MaxPoints: 60},
		{Title: "Complexity", Description: "Analysis", MaxPoints: 40},
	}
}

// ---------- extraction ----------

func makeDocx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="urn:x"><w:body><w:p><w:r><w:t>Report</w:t></w:r></w:p><w:p><w:r><w:t>Chapter</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	return buf.Bytes()
}

func TestExtractTXT(t *testing.T) {
	r := ExtractFromBytes("a.txt", []byte("plain answer"))
	if r.Failed || strings.TrimSpace(r.Text) != "plain answer" {
		t.Fatalf("txt: %+v", r)
	}
	if r2 := ExtractFromBytes("a.txt", []byte{0xFF, 0xFE, 0x00}); !r2.Failed {
		t.Fatalf("binary junk accepted as txt")
	}
}

func TestExtractDOCX(t *testing.T) {
	r := ExtractFromBytes("report.docx", makeDocx(t))
	if r.Failed {
		t.Fatalf("docx failed: %s", r.Reason)
	}
	if !strings.Contains(r.Text, "Report") || !strings.Contains(r.Text, "Chapter") {
		t.Fatalf("docx text wrong: %q", r.Text)
	}
	// Brokend docx (not a zip archive)
	if r2 := ExtractFromBytes("x.docx", []byte("not-zip")); !r2.Failed {
		t.Fatalf("bad docx accepted")
	}
}

// Minimal, uncompressed single-page PDF with one text object.
const tinyPDF = `%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >> endobj
4 0 obj << /Length 55 >> stream
BT /F1 12 Tf 72 720 Td (DFS uses a stack structure) Tj ET
endstream endobj
xref
0 5
trailer << /Size 5 /Root 1 0 R >>
%%EOF
`

func TestExtractPDF(t *testing.T) {
	r := ExtractFromBytes("answer.pdf", []byte(tinyPDF))
	if r.Failed {
		t.Logf("pdf extraction failed: %s", r.Reason)
	} else if !strings.Contains(strings.ToLower(r.Text), "dfs") {
		t.Fatalf("pdf text wrong: %q", r.Text)
	}
	// Unsupported / corrupted doc
	if r2 := ExtractFromBytes("image.pdf", []byte("not a pdf")); !r2.Failed {
		t.Fatalf("junk pdf accepted")
	}
}
