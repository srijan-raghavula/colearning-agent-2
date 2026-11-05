package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"educore/internal/store"
)

type chapterPageData struct {
	PageData
	Course   *store.Course
	Chapters []*store.Chapter
	Chapter  *store.Chapter
	IsNew    bool
	ShowList bool
}

// FacultyChapters lists the chapters for a course owned by the signed-in faculty member.
func (d *Deps) FacultyChapters(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, err := chapterPathID(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	course, ok := d.ownedChapterCourse(w, r, courseID, fid)
	if !ok {
		return
	}
	chapters, err := d.Store.ChaptersByCourseForFaculty(courseID, fid)
	if err != nil {
		log.Printf("load course chapters: %v", err)
		http.Error(w, "Could not load course chapters.", http.StatusInternalServerError)
		return
	}
	p := d.pageFrom(r)
	p.Title = "Course chapters · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_chapters.html", chapterPageData{
		PageData: p, Course: course, Chapters: chapters, ShowList: true,
	}, http.StatusOK)
}

// FacultyChapterNew displays the editor for a new draft chapter.
func (d *Deps) FacultyChapterNew(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, err := chapterPathID(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	course, ok := d.ownedChapterCourse(w, r, courseID, fid)
	if !ok {
		return
	}
	p := d.pageFrom(r)
	p.Title = "New chapter · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_chapters.html", chapterPageData{
		PageData: p, Course: course, IsNew: true,
	}, http.StatusOK)
}

// FacultyChapterEdit displays an existing chapter editor after ownership checks.
func (d *Deps) FacultyChapterEdit(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, chapterID, ok := d.chapterIDs(w, r)
	if !ok {
		return
	}
	course, ok := d.ownedChapterCourse(w, r, courseID, fid)
	if !ok {
		return
	}
	chapter, err := d.Store.ChapterForFaculty(courseID, chapterID, fid)
	if err != nil {
		d.chapterLookupError(w, r, err)
		return
	}
	p := d.pageFrom(r)
	p.Title = chapter.Title + " · EDUCORE"
	p.Nav = d.facultyNav(r)
	d.applyFlash(r, &p)
	d.render(w, r, "faculty_chapters.html", chapterPageData{
		PageData: p, Course: course, Chapter: chapter,
	}, http.StatusOK)
}

// FacultyChapterCreate saves a new chapter as a draft or publishes it explicitly.
func (d *Deps) FacultyChapterCreate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, err := chapterPathID(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := parseChapterForm(w, r); err != nil {
		d.errRedirect(w, r, chapterListPath(courseID), "The chapter form could not be processed.")
		return
	}
	position, err := strconv.Atoi(r.PostFormValue("position"))
	if err != nil {
		d.errRedirect(w, r, chapterListPath(courseID), "Chapter order must be a positive number.")
		return
	}
	published, err := chapterPublishIntent(r.PostFormValue("intent"))
	if err != nil {
		d.errRedirect(w, r, chapterListPath(courseID), "Choose whether to save the chapter as a draft or publish it.")
		return
	}
	_, err = d.Store.CreateChapter(fid, courseID,
		r.PostFormValue("title"), r.PostFormValue("description"), r.PostFormValue("content"),
		position, published)
	if err != nil {
		d.errRedirect(w, r, chapterListPath(courseID), chapterOperationError(err))
		return
	}
	if published {
		http.Redirect(w, r, chapterListPath(courseID)+"?ok="+urlQuery("Chapter published."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, chapterListPath(courseID)+"?ok="+urlQuery("Chapter saved as a draft."), http.StatusSeeOther)
}

// FacultyChapterUpdate saves edits as a draft or publishes the submitted content.
func (d *Deps) FacultyChapterUpdate(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, chapterID, ok := d.chapterIDs(w, r)
	if !ok {
		return
	}
	back := chapterEditPath(courseID, chapterID)
	if err := parseChapterForm(w, r); err != nil {
		d.errRedirect(w, r, back, "The chapter form could not be processed.")
		return
	}
	position, err := strconv.Atoi(r.PostFormValue("position"))
	if err != nil {
		d.errRedirect(w, r, back, "Chapter order must be a positive number.")
		return
	}
	published, err := chapterPublishIntent(r.PostFormValue("intent"))
	if err != nil {
		d.errRedirect(w, r, back, "Choose whether to save the chapter as a draft or publish it.")
		return
	}
	if err := d.Store.UpdateChapter(fid, courseID, chapterID,
		r.PostFormValue("title"), r.PostFormValue("description"), r.PostFormValue("content"),
		position, published); err != nil {
		d.errRedirect(w, r, back, chapterOperationError(err))
		return
	}
	if published {
		http.Redirect(w, r, chapterListPath(courseID)+"?ok="+urlQuery("Chapter published."), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, chapterListPath(courseID)+"?ok="+urlQuery("Chapter saved as a draft."), http.StatusSeeOther)
}

// FacultyChapterDelete deletes one chapter after store-level ownership checks.
func (d *Deps) FacultyChapterDelete(w http.ResponseWriter, r *http.Request) {
	fid, _ := d.fp(r)
	courseID, chapterID, ok := d.chapterIDs(w, r)
	if !ok {
		return
	}
	if err := d.Store.DeleteChapter(fid, courseID, chapterID); err != nil {
		d.errRedirect(w, r, chapterEditPath(courseID, chapterID), chapterOperationError(err))
		return
	}
	http.Redirect(w, r, chapterListPath(courseID)+"?ok="+urlQuery("Chapter deleted."), http.StatusSeeOther)
}

func (d *Deps) ownedChapterCourse(w http.ResponseWriter, r *http.Request, courseID, facultyID int64) (*store.Course, bool) {
	owned, err := d.Store.FacultyOwnsCourse(courseID, facultyID)
	if err != nil {
		if err == store.ErrNotFound {
			http.NotFound(w, r)
			return nil, false
		}
		log.Printf("verify chapter course access: %v", err)
		http.Error(w, "Could not verify course access.", http.StatusInternalServerError)
		return nil, false
	}
	if !owned {
		d.renderShellError(w, r, http.StatusForbidden, "No access to this course", "You can manage chapters only for courses you own.")
		return nil, false
	}
	course, err := d.Store.CourseByID(courseID)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	return course, true
}

func (d *Deps) chapterIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	courseID, err := chapterPathID(r, "courseID")
	if err != nil {
		http.NotFound(w, r)
		return 0, 0, false
	}
	chapterID, err := chapterPathID(r, "chapterID")
	if err != nil {
		http.NotFound(w, r)
		return 0, 0, false
	}
	return courseID, chapterID, true
}

func (d *Deps) chapterLookupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, store.ErrForbidden):
		d.renderShellError(w, r, http.StatusForbidden, "No access to this course", "You can manage chapters only for courses you own.")
	default:
		log.Printf("load course chapter: %v", err)
		http.Error(w, "Could not load the course chapter.", http.StatusInternalServerError)
	}
}

func chapterOperationError(err error) string {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "That chapter no longer exists."
	case errors.Is(err, store.ErrForbidden):
		return "You are not allowed to manage this chapter."
	case errors.Is(err, store.ErrInvalid):
		return err.Error()
	default:
		log.Printf("chapter operation failed: %v", err)
		return "Could not save the chapter. Please try again."
	}
}

func chapterPathID(r *http.Request, key string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return id, nil
}

func parseChapterForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	return r.ParseForm()
}

func chapterPublishIntent(intent string) (bool, error) {
	switch intent {
	case "draft":
		return false, nil
	case "publish":
		return true, nil
	default:
		return false, fmt.Errorf("invalid publication intent")
	}
}

func chapterListPath(courseID int64) string {
	return fmt.Sprintf("/faculty/courses/%d/chapters", courseID)
}

func chapterEditPath(courseID, chapterID int64) string {
	return fmt.Sprintf("%s/%d", chapterListPath(courseID), chapterID)
}

func urlQuery(value string) string {
	return url.QueryEscape(value)
}
