package store

import (
	"errors"
	"strings"
	"testing"
)

func TestChapterCRUDOwnershipAndStudentVisibility(t *testing.T) {
	s := tmpDB(t)
	facultyID, studentID := facultyAndStudent(s, t)
	course, err := s.CreateCourse(facultyID, "Data Structures", "DS-CH", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Enroll(course.ID, studentID, facultyID); err != nil {
		t.Fatal(err)
	}
	res, err := s.DB.Exec(`INSERT INTO users (email, name, password_hash, role)
		VALUES ('f2@x.edu', 'F2', 'x', 'FACULTY')`)
	if err != nil {
		t.Fatal(err)
	}
	otherFaculty, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	draft, err := s.CreateChapter(facultyID, course.ID, "Graphs", "Graph basics", "Draft material", 2, false)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	published, err := s.CreateChapter(facultyID, course.ID, "Trees", "Tree basics", "Published material", 1, true)
	if err != nil {
		t.Fatalf("create published chapter: %v", err)
	}
	if draft.Published || !published.Published {
		t.Fatalf("wrong publication state: draft=%t published=%t", draft.Published, published.Published)
	}

	chapters, err := s.ChaptersByCourseForFaculty(course.ID, facultyID)
	if err != nil || len(chapters) != 2 || chapters[0].ID != published.ID || chapters[1].ID != draft.ID {
		t.Fatalf("chapters not returned in chapter order: %+v err=%v", chapters, err)
	}
	if _, err := s.ChaptersByCourseForFaculty(course.ID, otherFaculty); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign faculty listed chapters: %v", err)
	}
	if _, err := s.ChapterForFaculty(course.ID, draft.ID, otherFaculty); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign faculty loaded a chapter: %v", err)
	}

	studentChapters, err := s.PublishedChaptersForStudent(course.ID, studentID)
	if err != nil || len(studentChapters) != 1 || studentChapters[0].ID != published.ID {
		t.Fatalf("student published chapter visibility: %+v err=%v", studentChapters, err)
	}
	if _, err := s.PublishedChapterForStudent(course.ID, draft.ID, studentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("student accessed draft chapter: %v", err)
	}
	if _, err := s.PublishedChapterForStudent(course.ID, published.ID, studentID+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unenrolled student accessed published chapter: %v", err)
	}

	if err := s.UpdateChapter(facultyID, course.ID, draft.ID, "Graph Search", "Updated", "New content", 3, true); err != nil {
		t.Fatalf("update and publish chapter: %v", err)
	}
	updated, err := s.ChapterForFaculty(course.ID, draft.ID, facultyID)
	if err != nil || updated.Title != "Graph Search" || updated.Position != 3 || !updated.Published {
		t.Fatalf("chapter update lost: %+v err=%v", updated, err)
	}
	if err := s.UpdateChapter(facultyID, course.ID, published.ID, "Trees", "", "Updated as draft", 1, false); err != nil {
		t.Fatalf("unpublish chapter: %v", err)
	}
	studentChapters, err = s.PublishedChaptersForStudent(course.ID, studentID)
	if err != nil || len(studentChapters) != 1 || studentChapters[0].ID != draft.ID {
		t.Fatalf("draft transition visibility: %+v err=%v", studentChapters, err)
	}
	if err := s.DeleteChapter(otherFaculty, course.ID, published.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign faculty deleted chapter: %v", err)
	}
	if err := s.DeleteChapter(facultyID, course.ID, published.ID); err != nil {
		t.Fatalf("delete chapter: %v", err)
	}
	if _, err := s.ChapterForFaculty(course.ID, published.ID, facultyID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted chapter still exists: %v", err)
	}
}

func TestChapterInputValidation(t *testing.T) {
	s := tmpDB(t)
	facultyID, _ := facultyAndStudent(s, t)
	course, err := s.CreateCourse(facultyID, "Data Structures", "DS-VAL", "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		title       string
		description string
		content     string
		position    int
		published   bool
	}{
		{name: "empty title", description: "", content: "body", position: 1},
		{name: "long title", title: strings.Repeat("x", maxChapterTitleRunes+1), content: "body", position: 1},
		{name: "long description", title: "Title", description: strings.Repeat("x", maxChapterDescriptionRunes+1), content: "body", position: 1},
		{name: "long content", title: "Title", content: strings.Repeat("x", maxChapterContentRunes+1), position: 1},
		{name: "invalid position", title: "Title", content: "body", position: 0},
		{name: "position too large", title: "Title", content: "body", position: maxChapterPosition + 1},
		{name: "empty published content", title: "Title", position: 1, published: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.CreateChapter(facultyID, course.ID, tc.title, tc.description, tc.content, tc.position, tc.published)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid chapter accepted: %v", err)
			}
		})
	}
}
