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

func TestReportShareRotateAndRevoke(t *testing.T) {
	database, router, report := setupReportHandlerTest(t)
	first := performReportRequest(router, http.MethodPost, "/reports/1/share", "")
	if first.Code != http.StatusOK {
		t.Fatalf("share: %d %s", first.Code, first.Body.String())
	}
	firstToken := responseString(t, first, "token")
	if len(firstToken) < 40 {
		t.Fatalf("token is too short: %q", firstToken)
	}
	var stored model.Report
	if err := database.First(&stored, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ShareTokenHash == firstToken || len(stored.ShareTokenHash) != 64 {
		t.Fatal("raw share token was stored")
	}

	public := performReportRequest(router, http.MethodGet, "/public/reports/1", firstToken)
	if public.Code != http.StatusOK {
		t.Fatalf("public report: %d %s", public.Code, public.Body.String())
	}
	if public.Header().Get("Cache-Control") != "no-store" || strings.Contains(public.Body.String(), "deploy_stages") {
		t.Fatal("public response caching or field isolation is incorrect")
	}

	second := performReportRequest(router, http.MethodPost, "/reports/1/share", "")
	secondToken := responseString(t, second, "token")
	if secondToken == firstToken {
		t.Fatal("share rotation reused a token")
	}
	if got := performReportRequest(router, http.MethodGet, "/public/reports/1", firstToken); got.Code != http.StatusNotFound {
		t.Fatalf("old token should be invalid, got %d", got.Code)
	}
	if got := performReportRequest(router, http.MethodDelete, "/reports/1/share", ""); got.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", got.Code, got.Body.String())
	}
	if got := performReportRequest(router, http.MethodGet, "/public/reports/1", secondToken); got.Code != http.StatusNotFound {
		t.Fatalf("revoked token should be invalid, got %d", got.Code)
	}
}

func TestPublicReportRejectsMissingAndWrongTokens(t *testing.T) {
	_, router, _ := setupReportHandlerTest(t)
	if got := performReportRequest(router, http.MethodGet, "/public/reports/1", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", got.Code)
	}
	if got := performReportRequest(router, http.MethodGet, "/public/reports/1", "wrong"); got.Code != http.StatusNotFound {
		t.Fatalf("wrong token: %d", got.Code)
	}
}

func setupReportHandlerTest(t *testing.T) (*gorm.DB, *gin.Engine, model.Report) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Project{}, &model.DeployTask{}, &model.Report{}); err != nil {
		t.Fatal(err)
	}
	project := model.Project{Name: "app", ProjectKey: "app", GitProvider: model.GitProviderGitHub, Branch: "main", AppDir: "/srv/app"}
	if err := database.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerWebhook, Branch: "main", CommitID: "def", BeforeCommitID: "abc", Status: model.TaskSuccess}
	if err := database.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	report := model.Report{Type: model.ReportTypeAIReview, ProjectID: project.ID, TaskID: task.ID, CommitID: "def", BeforeCommitID: "abc", Status: model.ReportSuccess, Conclusion: "passed", Summary: "safe", Issues: []model.ReportIssue{}, Markdown: "# Safe"}
	if err := database.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Server: config.ServerConfig{PublicURL: "https://go.postdare.com"}}
	svc := service.New(database, cfg, sse.NewHub(), zap.NewNop())
	h := &Handler{DB: database, Config: cfg, Service: svc}
	router := gin.New()
	router.GET("/public/reports/:report_id", h.GetPublicReport)
	router.POST("/reports/:report_id/share", h.ShareReport)
	router.DELETE("/reports/:report_id/share", h.RevokeReportShare)
	return database, router, report
}

func performReportRequest(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("X-Report-Token", token)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func responseString(t *testing.T, response *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	value, _ := body.Data[key].(string)
	return value
}

// A report shared before share salts existed has a token digest but no salt.
// Its link must keep resolving after the upgrade: verification goes through the
// stored digest, which the new derivation scheme leaves untouched.
func TestPublicReportAcceptsPreUpgradeToken(t *testing.T) {
	database, router, report := setupReportHandlerTest(t)
	legacyToken := "0Vv0legacy-token-minted-before-the-salt-column"
	if err := database.Model(&model.Report{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
		"share_enabled":    true,
		"share_token_hash": service.HashReportToken(legacyToken),
		"share_salt":       "",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if got := performReportRequest(router, http.MethodGet, "/public/reports/1", legacyToken); got.Code != http.StatusOK {
		t.Fatalf("a link shared before the upgrade must keep working, got %d %s", got.Code, got.Body.String())
	}
}

// A shared report carries its diff excerpts: a reader without repo access is
// exactly who needs the code beside the finding.
func TestPublicReportServesDiffHunks(t *testing.T) {
	database, router, report := setupReportHandlerTest(t)
	if err := database.Model(&model.Report{}).Where("id = ?", report.ID).Updates(map[string]interface{}{
		"issues": `[{"severity":"high","title":"race","location":"main.go:9","diff_hunk":"@@ -1 +1 @@\n-old\n+new"}]`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	shared := performReportRequest(router, http.MethodPost, "/reports/1/share", "")
	token := responseString(t, shared, "token")

	public := performReportRequest(router, http.MethodGet, "/public/reports/1", token)
	if public.Code != http.StatusOK {
		t.Fatalf("public report: %d %s", public.Code, public.Body.String())
	}
	if !strings.Contains(public.Body.String(), "diff_hunk") || !strings.Contains(public.Body.String(), "+new") {
		t.Fatalf("the shared report must carry the excerpt: %s", public.Body.String())
	}
}
