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
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func setupIssueUpdateTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Board{}, &model.Issue{}, &model.User{}, &model.Attachment{}, &model.DeployTask{}, &model.Project{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()
	hub := sse.NewHub()
	h := &Handler{DB: database, Config: cfg, Service: service.New(database, cfg, hub, zap.NewNop()), Hub: hub}
	router := gin.New()
	router.PATCH("/issues/:issue_id", h.UpdateIssue)
	router.GET("/issues/:issue_id", h.GetIssue)
	router.GET("/boards/:board_id/labels", h.ListBoardLabels)
	return database, router
}

func seedIssue(t *testing.T, database *gorm.DB, labels []string) model.Issue {
	t.Helper()
	board := model.Board{Name: "Engineering", Key: "ENG"}
	if err := database.Create(&board).Error; err != nil {
		t.Fatal(err)
	}
	issue := model.Issue{
		BoardID: board.ID, Number: 1, Title: "x", Status: model.IssueTodo,
		Priority: model.IssuePriorityNone, Position: "i", Labels: labels,
	}
	if err := database.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	return issue
}

func patchIssue(t *testing.T, router *gin.Engine, id uint64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/issues/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

// Labels are a []string behind a JSON serializer. A map-based update hands the
// driver the bare slice and the write fails, so this is the case that keeps the
// update on the path where the serializer runs.
func TestUpdateIssueWritesLabels(t *testing.T) {
	database, router := setupIssueUpdateTest(t)
	issue := seedIssue(t, database, []string{"old"})

	res := patchIssue(t, router, issue.ID, `{"labels":["Bug","SERVER"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Data struct {
			Labels []string `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Labels) != 2 || body.Data.Labels[0] != "Bug" || body.Data.Labels[1] != "SERVER" {
		t.Fatalf("unexpected labels in response: %#v", body.Data.Labels)
	}

	var reloaded model.Issue
	if err := database.First(&reloaded, issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Labels) != 2 || reloaded.Labels[0] != "Bug" {
		t.Fatalf("labels were not persisted: %#v", reloaded.Labels)
	}
}

// Removing the last label, or emptying the description, is a deliberate write of
// a zero value -- the kind a struct update skips unless the column is selected.
func TestUpdateIssueWritesClearedFields(t *testing.T) {
	database, router := setupIssueUpdateTest(t)
	assignee := model.User{Username: "kim", PasswordHash: "x", Role: "admin"}
	if err := database.Create(&assignee).Error; err != nil {
		t.Fatal(err)
	}
	issue := seedIssue(t, database, []string{"keep"})
	if err := database.Model(&issue).Updates(map[string]interface{}{"description": "some text", "assignee_id": assignee.ID}).Error; err != nil {
		t.Fatal(err)
	}

	res := patchIssue(t, router, issue.ID, `{"labels":[],"description":"","clear_assignee":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var reloaded model.Issue
	if err := database.First(&reloaded, issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Labels) != 0 {
		t.Fatalf("expected labels to be cleared, got %#v", reloaded.Labels)
	}
	if reloaded.Description != "" {
		t.Fatalf("expected description to be cleared, got %q", reloaded.Description)
	}
	if reloaded.AssigneeID != nil {
		t.Fatalf("expected assignee to be cleared, got %#v", reloaded.AssigneeID)
	}
}

func TestUpdateIssueNormalizesLabels(t *testing.T) {
	database, router := setupIssueUpdateTest(t)
	issue := seedIssue(t, database, nil)

	res := patchIssue(t, router, issue.ID, `{"labels":["  Bug  ","bug","","SERVER"]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var reloaded model.Issue
	if err := database.First(&reloaded, issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Trimmed, blanks dropped, and a case-insensitive duplicate kept only once.
	if len(reloaded.Labels) != 2 || reloaded.Labels[0] != "Bug" || reloaded.Labels[1] != "SERVER" {
		t.Fatalf("unexpected normalized labels: %#v", reloaded.Labels)
	}
}

func TestListBoardLabelsIsTheBoardsVocabulary(t *testing.T) {
	database, router := setupIssueUpdateTest(t)
	issue := seedIssue(t, database, []string{"Bug", "SERVER"})
	second := model.Issue{
		BoardID: issue.BoardID, Number: 2, Title: "y", Status: model.IssueTodo,
		Priority: model.IssuePriorityNone, Position: "j", Labels: []string{"bug", "web"},
	}
	if err := database.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/boards/1/labels", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	var body struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// "Bug" and "bug" are the same label; the vocabulary lists it once, sorted.
	want := []string{"Bug", "SERVER", "web"}
	if len(body.Data) != len(want) {
		t.Fatalf("expected %v, got %v", want, body.Data)
	}
	for i := range want {
		if body.Data[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, body.Data)
		}
	}
}
