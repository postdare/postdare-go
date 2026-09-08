package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingBackend answers every REST call with a fixed body and remembers the
// paths the MCP server asked for, so a test can assert the routing rather than
// the payload.
func recordingBackend(t *testing.T, requested *[]string) *httptest.Server {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("missing bearer token, got %q", got)
		}
		*requested = append(*requested, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":7,"type":"ai_review"}}`))
	}))
	t.Cleanup(backend.Close)
	return backend
}

func call(t *testing.T, server *Server, method string, params string) map[string]interface{} {
	t.Helper()
	line := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"`
	if params != "" {
		line += `,"params":` + params
	}
	line += `}`
	raw, ok := server.HandleLine([]byte(line))
	if !ok {
		t.Fatalf("%s produced no response", method)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if errObj, exists := decoded["error"]; exists {
		t.Fatalf("%s failed: %v", method, errObj)
	}
	return decoded
}

func TestReportToolsCallRESTPaths(t *testing.T) {
	var requested []string
	backend := recordingBackend(t, &requested)
	server := NewServer(backend.URL, "test-token")

	call(t, server, "tools/call", `{"name":"postdare_go.list_deploy_task_reports","arguments":{"task_id":12}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.get_report","arguments":{"report_id":7}}`)

	want := []string{"/api/v1/deploy-tasks/12/reports", "/api/v1/reports/7"}
	if strings.Join(requested, ",") != strings.Join(want, ",") {
		t.Fatalf("requested %v, want %v", requested, want)
	}
}

func TestReportToolsAreListed(t *testing.T) {
	server := NewServer("http://127.0.0.1:0", "")
	result := call(t, server, "tools/list", "")["result"].(map[string]interface{})
	listed := map[string]bool{}
	for _, entry := range result["tools"].([]interface{}) {
		listed[entry.(map[string]interface{})["name"].(string)] = true
	}
	for _, name := range []string{"postdare_go.list_deploy_task_reports", "postdare_go.get_report"} {
		if !listed[name] {
			t.Fatalf("tool %s is not listed", name)
		}
	}
}

// A report URI must not be swallowed by the deploy-task branches, and a task's
// report list must not be read as a task detail.
func TestReportResourceURIsRouteSeparately(t *testing.T) {
	var requested []string
	backend := recordingBackend(t, &requested)
	server := NewServer(backend.URL, "test-token")

	call(t, server, "resources/read", `{"uri":"postdare-go://reports/7"}`)
	call(t, server, "resources/read", `{"uri":"postdare-go://deploy-tasks/12/reports"}`)

	want := []string{"/api/v1/reports/7", "/api/v1/deploy-tasks/12/reports"}
	if strings.Join(requested, ",") != strings.Join(want, ",") {
		t.Fatalf("requested %v, want %v", requested, want)
	}
}
