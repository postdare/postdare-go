package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestDeleteProjectReturnsNotFound(t *testing.T) {
	_, router := setupDeleteProjectTest(t)

	req := httptest.NewRequest(http.MethodDelete, "/projects/404", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

func TestDeleteProjectBlocksActiveTask(t *testing.T) {
	database, router := setupDeleteProjectTest(t)
	project := createHandlerTestProject(t, database, "app")
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerManual, Status: model.TaskRunning}
	if err := database.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	stage := model.DeployTaskStage{TaskID: task.ID, Name: "deploy", Status: model.StageRunning}
	if err := database.Create(&stage).Error; err != nil {
		t.Fatal(err)
	}
	eventProjectID := project.ID
	event := model.WebhookEvent{Provider: model.GitProviderGitHub, ProjectID: &eventProjectID, ProjectKey: project.ProjectKey}
	if err := database.Create(&event).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/projects/1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", res.Code, res.Body.String())
	}

	assertRowCount(t, database, &model.Project{}, "id = ?", 1, project.ID)
	assertRowCount(t, database, &model.DeployTask{}, "id = ?", 1, task.ID)
	assertRowCount(t, database, &model.DeployTaskStage{}, "id = ?", 1, stage.ID)
	assertRowCount(t, database, &model.WebhookEvent{}, "id = ?", 1, event.ID)
}

func TestDeleteProjectCascadesRelatedRecordsAndKeepsLogFile(t *testing.T) {
	database, router := setupDeleteProjectTest(t)
	project := createHandlerTestProject(t, database, "app")
	otherProject := createHandlerTestProject(t, database, "other")
	logFile := filepath.Join(t.TempDir(), "deploy.log")
	if err := os.WriteFile(logFile, []byte("deploy output\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tasks := []model.DeployTask{
		{ProjectID: project.ID, TriggerType: model.TriggerManual, Status: model.TaskSuccess, LogFile: logFile},
		{ProjectID: project.ID, TriggerType: model.TriggerRollback, Status: model.TaskFailed},
		{ProjectID: otherProject.ID, TriggerType: model.TriggerManual, Status: model.TaskSuccess},
	}
	if err := database.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	taskA, taskB, otherTask := tasks[0], tasks[1], tasks[2]
	reports := []model.Report{
		{Type: model.ReportTypeAIReview, ProjectID: project.ID, TaskID: taskA.ID, Status: model.ReportSuccess},
		{Type: model.ReportTypeAIReview, ProjectID: otherProject.ID, TaskID: otherTask.ID, Status: model.ReportSuccess},
	}
	if err := database.Create(&reports).Error; err != nil {
		t.Fatal(err)
	}

	stages := []model.DeployTaskStage{
		{TaskID: taskA.ID, Name: "build", Status: model.StageSuccess},
		{TaskID: taskB.ID, Name: "rollback", Status: model.StageFailed},
		{TaskID: otherTask.ID, Name: "build", Status: model.StageSuccess},
	}
	if err := database.Create(&stages).Error; err != nil {
		t.Fatal(err)
	}
	projectID := project.ID
	otherProjectID := otherProject.ID
	events := []model.WebhookEvent{
		{Provider: model.GitProviderGitHub, ProjectID: &projectID, ProjectKey: project.ProjectKey},
		{Provider: model.GitProviderGitee, ProjectKey: project.ProjectKey},
		{Provider: model.GitProviderGitHub, ProjectID: &otherProjectID, ProjectKey: otherProject.ProjectKey},
	}
	if err := database.Create(&events).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/projects/1", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", res.Code, res.Body.String())
	}

	assertRowCount(t, database, &model.Project{}, "id = ?", 0, project.ID)
	assertRowCount(t, database, &model.Project{}, "id = ?", 1, otherProject.ID)
	assertRowCount(t, database, &model.DeployTask{}, "project_id = ?", 0, project.ID)
	assertRowCount(t, database, &model.DeployTask{}, "project_id = ?", 1, otherProject.ID)
	assertRowCount(t, database, &model.DeployTaskStage{}, "task_id IN ?", 0, []uint64{taskA.ID, taskB.ID})
	assertRowCount(t, database, &model.DeployTaskStage{}, "task_id = ?", 1, otherTask.ID)
	assertRowCount(t, database, &model.Report{}, "project_id = ?", 0, project.ID)
	assertRowCount(t, database, &model.Report{}, "project_id = ?", 1, otherProject.ID)
	assertRowCount(t, database, &model.WebhookEvent{}, "project_id = ? OR project_key = ?", 0, project.ID, project.ProjectKey)
	assertRowCount(t, database, &model.WebhookEvent{}, "project_id = ? OR project_key = ?", 1, otherProject.ID, otherProject.ProjectKey)
	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("expected physical deploy log file to remain, got %v", err)
	}
}

func setupDeleteProjectTest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Project{}, &model.DeployTask{}, &model.DeployTaskStage{}, &model.Report{}); err != nil {
		t.Fatal(err)
	}
	// SQLite requires globally unique index names, so this test creates the
	// webhook table directly instead of AutoMigrating two idx_created_at models.
	if err := database.Exec(`CREATE TABLE webhook_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		provider text NOT NULL,
		project_id integer,
		project_key text,
		event_type text,
		branch text,
		commit_id text,
		before_commit_id text,
		commit_message text,
		commit_author text,
		delivery_id text,
		signature_valid numeric NOT NULL DEFAULT false,
		handled numeric NOT NULL DEFAULT false,
		ignored_reason text,
		raw_payload json,
		created_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	svc := service.New(database, cfg, sse.NewHub(), zap.NewNop())
	h := &Handler{DB: database, Config: cfg, Service: svc, Hub: sse.NewHub()}
	router := gin.New()
	router.DELETE("/projects/:project_id", h.DeleteProject)
	return database, router
}

func createHandlerTestProject(t *testing.T, database *gorm.DB, key string) model.Project {
	t.Helper()
	project := model.Project{Name: key, ProjectKey: key, GitProvider: model.GitProviderGitHub, Branch: "main", AppDir: "/data/" + key}
	if err := database.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	return project
}

func assertRowCount(t *testing.T, database *gorm.DB, modelValue interface{}, query string, want int64, args ...interface{}) {
	t.Helper()
	var got int64
	if err := database.Model(modelValue).Where(query, args...).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("expected %s count %d, got %d", query, want, got)
	}
}
