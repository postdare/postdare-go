package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestBoardToolsCallRESTPaths(t *testing.T) {
	var requested []string
	backend := recordingBackend(t, &requested)
	server := NewServer(backend.URL, "test-token")

	call(t, server, "tools/call", `{"name":"postdare_go.list_boards","arguments":{}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.get_board","arguments":{"board_id":3}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.list_board_issues","arguments":{"board_id":3,"status":"in_progress","assignee_id":"none","q":"login"}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.get_issue","arguments":{"issue_id":9}}`)

	want := []string{
		"/api/v1/boards",
		"/api/v1/boards/3",
		"/api/v1/boards/3/issues?assignee_id=none&q=login&status=in_progress",
		"/api/v1/issues/9",
	}
	if strings.Join(requested, ",") != strings.Join(want, ",") {
		t.Fatalf("requested %v, want %v", requested, want)
	}
}

func TestBoardToolsAreListed(t *testing.T) {
	server := NewServer("http://127.0.0.1:0", "")
	result := call(t, server, "tools/list", "")["result"].(map[string]interface{})
	listed := map[string]bool{}
	for _, entry := range result["tools"].([]interface{}) {
		listed[entry.(map[string]interface{})["name"].(string)] = true
	}
	for _, name := range []string{
		"postdare_go.list_boards",
		"postdare_go.get_board",
		"postdare_go.list_board_issues",
		"postdare_go.get_issue",
		"postdare_go.create_issue",
		"postdare_go.update_issue",
		"postdare_go.move_issue",
		"postdare_go.list_issue_comments",
		"postdare_go.comment_on_issue",
	} {
		if !listed[name] {
			t.Fatalf("tool %s is not listed", name)
		}
	}
}

// recordingBodyBackend keeps the method and JSON body of every call, so a test
// can assert what a mutation tool forwards -- confirm above all, since that is
// what the backend's mutation gate reads.
func recordingBodyBackend(t *testing.T, methods *[]string, bodies *[]map[string]interface{}) *httptest.Server {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*methods = append(*methods, r.Method)
		raw, _ := io.ReadAll(r.Body)
		body := map[string]interface{}{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("request body %q is not json: %v", raw, err)
			}
		}
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":9}}`))
	}))
	t.Cleanup(backend.Close)
	return backend
}

// A mutation tool cannot leave confirm behind: the backend refuses the write
// without it. Fields the caller did not send must stay out of the body too,
// because the PATCH handler reads a missing field as "not part of this edit".
func TestIssueMutationToolsForwardConfirm(t *testing.T) {
	var methods []string
	var bodies []map[string]interface{}
	backend := recordingBodyBackend(t, &methods, &bodies)
	server := NewServer(backend.URL, "test-token")

	call(t, server, "tools/call", `{"name":"postdare_go.create_issue","arguments":{"board_id":3,"title":"fix login","labels":["bug"],"confirm":true}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.update_issue","arguments":{"issue_id":9,"priority":"high","confirm":true}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.move_issue","arguments":{"issue_id":9,"status":"done","confirm":true}}`)
	call(t, server, "tools/call", `{"name":"postdare_go.comment_on_issue","arguments":{"issue_id":9,"body":"deployed to staging","confirm":true}}`)

	wantMethods := []string{http.MethodPost, http.MethodPatch, http.MethodPost, http.MethodPost}
	if !reflect.DeepEqual(methods, wantMethods) {
		t.Fatalf("methods are %v, want %v", methods, wantMethods)
	}
	wantBodies := []map[string]interface{}{
		{"title": "fix login", "labels": []interface{}{"bug"}, "confirm": true},
		{"priority": "high", "confirm": true},
		{"status": "done", "confirm": true},
		{"body": "deployed to staging", "confirm": true},
	}
	if !reflect.DeepEqual(bodies, wantBodies) {
		t.Fatalf("bodies are %v, want %v", bodies, wantBodies)
	}
}

// The boards collection and a board's issue list sit under the same prefix, so
// the more specific URI has to be matched first.
func TestBoardResourceURIsRouteSeparately(t *testing.T) {
	var requested []string
	backend := recordingBackend(t, &requested)
	server := NewServer(backend.URL, "test-token")

	call(t, server, "resources/read", `{"uri":"postdare-go://boards"}`)
	call(t, server, "resources/read", `{"uri":"postdare-go://boards/3/issues"}`)
	call(t, server, "resources/read", `{"uri":"postdare-go://boards/3"}`)
	call(t, server, "resources/read", `{"uri":"postdare-go://issues/9"}`)

	want := []string{"/api/v1/boards", "/api/v1/boards/3/issues", "/api/v1/boards/3", "/api/v1/issues/9"}
	if strings.Join(requested, ",") != strings.Join(want, ",") {
		t.Fatalf("requested %v, want %v", requested, want)
	}
}
