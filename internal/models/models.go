package models

// User is an account holder.
type User struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Role           string `json:"role"` // FACULTY or STUDENT
	PasswordHash   string `json:"-"`
	CreatedAtS     string `json:"-"`
	CreatedAtPrint string `json:"-"`
}

// DisplayName returns a friendly name for templates.
func (u User) DisplayName() string { return u.Name }

// IsFaculty is true when the user manages courses.
func (u User) IsFaculty() bool { return u.Role == "FACULTY" }

// IsStudent is true when the user is a learner.
func (u User) IsStudent() bool { return u.Role == "STUDENT" }

// Entity field references for later phases are documented here to keep the
// template layer shape-stable:
//   Course       { ID, FacultyID, Name, Code, Description }
//   Enrollment   { StudentID, CourseID }
//   Assignment   { ID, CourseID, Title, Description, Topic, DueDate, Published, TotalMarks }
//   Criterion    { ID, AssignmentID, Position, Title, Description, MaxPoints, Topic, Keywords }
//   Submission   { ID, AssignmentID, StudentID, Content, Filename, FilePath, Status, SubmittedAt }
//   Evaluation   { ID, SubmissionID, Attempt, TotalScore, Overall, Strengths[], Gaps[], Suggestions[], Confidence, Method, Approved, FacultyFeedback }
//   CriterionResult { EvaluationID, CriterionID, AwardedPoints, Evidence, Feedback }
