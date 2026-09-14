package handler

import (
	"encoding/json"
	"fmt"
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
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const issueGateToken = "issue-gate-test-token"

// The MCP token reaches every /api/v1 route, so issue writes carry the same
// double gate as deploy and rollback: mcp.allow_mutation_tools on the server,
// and confirm=true on the call itself.
func setupIssueMutationGate(t *testing.T, allowMutations bool) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&model.Board{},
		&model.Issue{},

		&model.Attachment{},
		&model.User{},
		&model.Project{},
		&model.DeployTask{},
		&model.DeployTaskStage{},
		&model.Setting{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&model.Board{Name: "Engineering", Key: "ENG"}).Error; err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("human-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&model.User{Username: "human", PasswordHash: string(hash), Role: "admin"}).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.JWT.Secret = "test-jwt-secret"
	cfg.JWT.ExpireHours = 72
	cfg.DataDir = t.TempDir()
	cfg.MCP.Enabled = true
	cfg.MCP.APIToken = issueGateToken
	cfg.MCP.AllowMutationTools = allowMutations

	hub := sse.NewHub()
	router := gin.New()
	RegisterRoutes(router, &Handler{
		DB:      database,
		Config:  cfg,
		Service: service.New(database, cfg, hub, zap.NewNop()),
		Hub:     hub,
	})
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database, router
}

func callAsMCP(t *testing.T, router *gin.Engine, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueGateToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func issuedCount(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := database.Model(&model.Issue{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// With the flag off an MCP caller cannot write, but reading the board it may
// still do: the gate guards mutations, not the token's whole reach.
func TestMCPCannotWriteIssuesWhileMutationToolsAreDisabled(t *testing.T) {
	database, router := setupIssueMutationGate(t, false)

	recorder := callAsMCP(t, router, http.MethodPost, "/api/v1/boards/1/issues", `{"title":"from mcp"}`)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "MCP_MUTATION_DISABLED") {
		t.Fatalf("create: got %d %s", recorder.Code, recorder.Body.String())
	}
	if got := issuedCount(t, database); got != 0 {
		t.Fatalf("issue was created despite the gate: %d", got)
	}
	if recorder := callAsMCP(t, router, http.MethodGet, "/api/v1/boards/1/issues", ""); recorder.Code != http.StatusOK {
		t.Fatalf("read: got %d %s", recorder.Code, recorder.Body.String())
	}
}

// Create, edit and move each need confirm=true, and each one leaves the database
// untouched when it is missing.
func TestMCPIssueWriteRequiresConfirm(t *testing.T) {
	database, router := setupIssueMutationGate(t, true)

	recorder := callAsMCP(t, router, http.MethodPost, "/api/v1/boards/1/issues", `{"title":"unconfirmed"}`)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "CONFIRM_REQUIRED") {
		t.Fatalf("create: got %d %s", recorder.Code, recorder.Body.String())
	}
	if got := issuedCount(t, database); got != 0 {
		t.Fatalf("issue was created without confirm: %d", got)
	}

	recorder = callAsMCP(t, router, http.MethodPost, "/api/v1/boards/1/issues", `{"title":"confirmed","priority":"high","confirm":true}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("confirmed create: got %d %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		Data struct {
			ID uint64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.ID == 0 {
		t.Fatalf("no issue id in %s", recorder.Body.String())
	}
	issuePath := fmt.Sprintf("/api/v1/issues/%d", created.Data.ID)

	recorder = callAsMCP(t, router, http.MethodPatch, issuePath, `{"priority":"low"}`)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "CONFIRM_REQUIRED") {
		t.Fatalf("update: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = callAsMCP(t, router, http.MethodPatch, issuePath, `{"priority":"low","confirm":true}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"priority":"low"`) {
		t.Fatalf("confirmed update: got %d %s", recorder.Code, recorder.Body.String())
	}

	movePath := issuePath + "/move"
	recorder = callAsMCP(t, router, http.MethodPost, movePath, `{"status":"done"}`)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "CONFIRM_REQUIRED") {
		t.Fatalf("move: got %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = callAsMCP(t, router, http.MethodPost, movePath, `{"status":"done","confirm":true}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"done"`) {
		t.Fatalf("confirmed move: got %d %s", recorder.Code, recorder.Body.String())
	}
}

// The gate is about the token's actor, not about issue edits: a signed-in person
// editing the same issue is never asked for confirm.
func TestJWTUserEditsAnIssueWithoutConfirm(t *testing.T) {
	database, router := setupIssueMutationGate(t, true)
	if err := database.Create(&model.Issue{
		BoardID:  1,
		Number:   1,
		Title:    "seeded",
		Status:   model.IssueBacklog,
		Priority: model.IssuePriorityNone,
		Position: "a",
	}).Error; err != nil {
		t.Fatal(err)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"human","password":"human-password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	router.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login: got %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	var session struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginResponse.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/issues/1", strings.NewReader(`{"priority":"urgent"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Data.Token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"priority":"urgent"`) {
		t.Fatalf("human edit: got %d %s", recorder.Code, recorder.Body.String())
	}
}
