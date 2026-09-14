package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// actor is who the request is made as: the routes are mounted behind a stub of
// the auth middleware so a test can post as one user and try to edit as another.
type actor struct {
	userID uint64
	role   string
}

func setupCommentTest(t *testing.T, as *actor) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Board{}, &model.Issue{}, &model.IssueComment{}, &model.User{}, &model.Attachment{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()
	hub := sse.NewHub()
	h := &Handler{DB: database, Config: cfg, Service: service.New(database, cfg, hub, zap.NewNop()), Hub: hub}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if as.userID != 0 {
			c.Set(middleware.UserIDKey, as.userID)
		}
		c.Set(middleware.RoleKey, as.role)
		c.Next()
	})
	router.GET("/issues/:issue_id/comments", h.ListIssueComments)
	router.POST("/issues/:issue_id/comments", h.CreateIssueComment)
	router.PATCH("/issue-comments/:comment_id", h.UpdateIssueComment)
	router.DELETE("/issue-comments/:comment_id", h.DeleteIssueComment)
	router.GET("/boards/:board_id/issues", h.ListBoardIssues)
	return database, router
}

func seedCommentIssue(t *testing.T, database *gorm.DB) model.Issue {
	t.Helper()
	board := model.Board{Name: "Engineering", Key: "ENG"}
	if err := database.Create(&board).Error; err != nil {
		t.Fatal(err)
	}
	issue := model.Issue{
		BoardID: board.ID, Number: 1, Title: "login fails", Status: model.IssueTodo,
		Priority: model.IssuePriorityNone, Position: "i", Labels: []string{},
	}
	if err := database.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	return issue
}

func request(t *testing.T, router *gin.Engine, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestCommentCarriesItsAuthorsName(t *testing.T) {
	as := &actor{userID: 1, role: "member"}
	database, router := setupCommentTest(t, as)
	if err := database.Create(&model.User{ID: 1, Username: "kim", PasswordHash: "x", Role: "member"}).Error; err != nil {
		t.Fatal(err)
	}
	seedCommentIssue(t, database)

	res := request(t, router, http.MethodPost, "/issues/1/comments", `{"body":"looks like a cache"}`)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}
	var created struct {
		Data struct {
			AuthorName string `json:"author_name"`
			Edited     bool   `json:"edited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.AuthorName != "kim" {
		t.Fatalf("author is %q, want kim", created.Data.AuthorName)
	}
	if created.Data.Edited {
		t.Fatal("a comment that was just posted must not read as edited")
	}

	// The board's cards carry the count, so the thread shows up without the
	// board fetching it.
	res = request(t, router, http.MethodGet, "/boards/1/issues", "")
	var listed struct {
		Data []struct {
			CommentCount int `json:"comment_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].CommentCount != 1 {
		t.Fatalf("board issues report %+v, want one card with one comment", listed.Data)
	}
}

// A remark is attributed, so only its author may rewrite it.
func TestOnlyTheAuthorCanEditAComment(t *testing.T) {
	as := &actor{userID: 1, role: "member"}
	database, router := setupCommentTest(t, as)
	seedCommentIssue(t, database)

	if res := request(t, router, http.MethodPost, "/issues/1/comments", `{"body":"mine"}`); res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}

	as.userID = 2
	if res := request(t, router, http.MethodPatch, "/issue-comments/1", `{"body":"not mine"}`); res.Code != http.StatusForbidden {
		t.Fatalf("a second member got %d editing someone else's comment, want 403", res.Code)
	}
	if res := request(t, router, http.MethodDelete, "/issue-comments/1", ""); res.Code != http.StatusForbidden {
		t.Fatalf("a second member got %d deleting someone else's comment, want 403", res.Code)
	}

	// An admin keeps a way to take down what should not stand on the board.
	as.role = "admin"
	if res := request(t, router, http.MethodDelete, "/issue-comments/1", ""); res.Code != http.StatusNoContent {
		t.Fatalf("admin got %d deleting a comment, want 204", res.Code)
	}
}

func TestEditedCommentSaysSo(t *testing.T) {
	as := &actor{userID: 1, role: "member"}
	database, router := setupCommentTest(t, as)
	seedCommentIssue(t, database)
	request(t, router, http.MethodPost, "/issues/1/comments", `{"body":"first go"}`)

	// The stored CreatedAt is moved back rather than waiting a second: the flag
	// exists to survive the millisecond gap a fresh row already has.
	if err := database.Exec("UPDATE issue_comments SET created_at = datetime(created_at, '-1 hour') WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}

	res := request(t, router, http.MethodPatch, "/issue-comments/1", `{"body":"second go"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var updated struct {
		Data struct {
			Body   string `json:"body"`
			Edited bool   `json:"edited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Data.Body != "second go" || !updated.Data.Edited {
		t.Fatalf("updated comment is %+v, want the new body marked edited", updated.Data)
	}
}

func TestEmptyCommentIsRefused(t *testing.T) {
	as := &actor{userID: 1, role: "member"}
	database, router := setupCommentTest(t, as)
	seedCommentIssue(t, database)

	res := request(t, router, http.MethodPost, "/issues/1/comments", `{"body":"\n  "}`)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", res.Code, res.Body.String())
	}
}
