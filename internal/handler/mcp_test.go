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
	"github.com/hellodeveye/postdare-go/internal/mcp"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const mcpTestToken = "mcp-endpoint-test-token"

func setupMCPRouter(t *testing.T, mutate func(*config.Config)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Project{}, &model.DeployTask{}, &model.DeployTaskStage{}, &model.Report{}, &model.Setting{}, &model.User{}, &model.WebhookEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&model.Project{Name: "demo", ProjectKey: "demo", GitProvider: "gitee", Branch: "main"}).Error; err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.JWT.Secret = "test-jwt-secret"
	cfg.MCP.Enabled = true
	cfg.MCP.APIToken = mcpTestToken
	cfg.Server.CORSOrigins = []string{"http://localhost:5173"}
	if mutate != nil {
		mutate(cfg)
	}
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
	return router
}

type mcpCallOption func(*http.Request)

func withHeader(key string, value string) mcpCallOption {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

func postMCP(t *testing.T, router *gin.Engine, body string, options ...mcpCallOption) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+mcpTestToken)
	for _, option := range options {
		option(req)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func decodeRPC(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func TestMCPEndpointInitializeEchoesProtocolVersion(t *testing.T) {
	router := setupMCPRouter(t, nil)
	recorder := postMCP(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("initialize: %d %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type is %q", got)
	}
	result := decodeRPC(t, recorder)["result"].(map[string]interface{})
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocol version is %v", result["protocolVersion"])
	}

	// An unknown revision gets the newest one this server speaks.
	recorder = postMCP(t, router, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	result = decodeRPC(t, recorder)["result"].(map[string]interface{})
	if result["protocolVersion"] != mcp.ProtocolVersionLatest {
		t.Fatalf("protocol version is %v", result["protocolVersion"])
	}
}

// A tool call must reach the REST handlers in process and come back with their
// data, which is what makes this transport equivalent to the stdio one.
func TestMCPEndpointToolCallReachesRESTHandlers(t *testing.T) {
	router := setupMCPRouter(t, nil)
	recorder := postMCP(t, router, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"postdare_go.list_projects","arguments":{}}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("tools/call: %d %s", recorder.Code, recorder.Body.String())
	}
	decoded := decodeRPC(t, recorder)
	if errObj, exists := decoded["error"]; exists {
		t.Fatalf("tools/call failed: %v", errObj)
	}
	result := decoded["result"].(map[string]interface{})
	text := result["content"].([]interface{})[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, `"project_key": "demo"`) {
		t.Fatalf("project is missing from tool output: %s", text)
	}
}

// Mutation tools stay gated when reached over HTTP, not just over stdio.
func TestMCPEndpointHonoursMutationGate(t *testing.T) {
	router := setupMCPRouter(t, nil)
	recorder := postMCP(t, router, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"postdare_go.trigger_deploy","arguments":{"project_id":1,"confirm":true}}}`)
	message := decodeRPC(t, recorder)["error"].(map[string]interface{})["message"].(string)
	if !strings.Contains(message, "MCP_MUTATION_DISABLED") {
		t.Fatalf("mutation was not refused: %s", message)
	}
}

func TestMCPEndpointNotificationGets202(t *testing.T) {
	router := setupMCPRouter(t, nil)
	recorder := postMCP(t, router, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if recorder.Code != http.StatusAccepted || recorder.Body.Len() != 0 {
		t.Fatalf("notification: %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestMCPEndpointRejectsBadRequests(t *testing.T) {
	router := setupMCPRouter(t, nil)
	cases := []struct {
		name    string
		body    string
		options []mcpCallOption
		want    int
	}{
		{"no token", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, []mcpCallOption{withHeader("Authorization", "")}, http.StatusUnauthorized},
		{"wrong token", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, []mcpCallOption{withHeader("Authorization", "Bearer nope")}, http.StatusUnauthorized},
		{"foreign origin", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, []mcpCallOption{withHeader("Origin", "http://evil.example")}, http.StatusForbidden},
		{"unsupported version", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, []mcpCallOption{withHeader("MCP-Protocol-Version", "1999-01-01")}, http.StatusBadRequest},
		{"json not accepted", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, []mcpCallOption{withHeader("Accept", "text/plain")}, http.StatusNotAcceptable},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, nil, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := postMCP(t, router, testCase.body, testCase.options...)
			if recorder.Code != testCase.want {
				t.Fatalf("got %d want %d: %s", recorder.Code, testCase.want, recorder.Body.String())
			}
		})
	}

	// A configured origin is what a browser-based client would send.
	if recorder := postMCP(t, router, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, withHeader("Origin", "http://localhost:5173")); recorder.Code != http.StatusOK {
		t.Fatalf("allowed origin: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMCPEndpointRejectsOtherVerbs(t *testing.T) {
	router := setupMCPRouter(t, nil)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+mcpTestToken)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: %d %s", method, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Allow"); got != "POST" {
			t.Fatalf("%s: Allow is %q", method, got)
		}
	}
}

// With mcp.enabled off the endpoint must not exist at all.
func TestMCPEndpointAbsentWhenDisabled(t *testing.T) {
	router := setupMCPRouter(t, func(cfg *config.Config) { cfg.MCP.Enabled = false })
	recorder := postMCP(t, router, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("got %d want 404: %s", recorder.Code, recorder.Body.String())
	}
}
