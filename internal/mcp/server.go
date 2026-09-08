package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	client *RESTClient
}

func NewServer(baseURL string, apiToken string) *Server {
	return &Server{client: NewRESTClient(baseURL, apiToken)}
}

type RESTClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func NewRESTClient(baseURL string, token string) *RESTClient {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8088"
	}
	return &RESTClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) HandleLine(line []byte) ([]byte, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return mustJSON(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}), true
	}
	if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
		return nil, false
	}
	result, err := s.dispatch(req.Method, req.Params)
	if err != nil {
		return mustJSON(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}}), true
	}
	return mustJSON(response{JSONRPC: "2.0", ID: req.ID, Result: result}), true
}

func (s *Server) dispatch(method string, params json.RawMessage) (interface{}, error) {
	switch method {
	case "initialize":
		return map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
				"prompts":   map[string]interface{}{},
			},
			"serverInfo": map[string]string{"name": "postdare-go", "version": "0.1.0"},
		}, nil
	case "tools/list":
		return map[string]interface{}{"tools": tools()}, nil
	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return s.callTool(p.Name, p.Arguments)
	case "resources/list":
		return map[string]interface{}{"resources": resources()}, nil
	case "resources/templates/list":
		return map[string]interface{}{"resourceTemplates": resourceTemplates()}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return s.readResource(p.URI)
	case "prompts/list":
		return map[string]interface{}{"prompts": prompts()}, nil
	case "prompts/get":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return getPrompt(p.Name, p.Arguments)
	default:
		return nil, fmt.Errorf("unsupported method %s", method)
	}
}

func (s *Server) callTool(name string, args map[string]interface{}) (interface{}, error) {
	var result interface{}
	var err error
	switch name {
	case "postdare_go.list_projects":
		q := url.Values{}
		if provider := strArg(args, "git_provider"); provider != "" {
			q.Set("provider", provider)
		}
		result, err = s.client.Get("/api/v1/projects", q)
	case "postdare_go.get_project":
		result, err = s.client.Get(fmt.Sprintf("/api/v1/projects/%d", uintArg(args, "project_id")), nil)
	case "postdare_go.list_deploy_tasks":
		q := url.Values{}
		if projectID := uintArg(args, "project_id"); projectID > 0 {
			q.Set("project_id", strconv.FormatUint(projectID, 10))
		}
		if status := strArg(args, "status"); status != "" {
			q.Set("status", status)
		}
		limit := uintArg(args, "limit")
		if limit == 0 {
			limit = 10
		}
		q.Set("page_size", strconv.FormatUint(limit, 10))
		result, err = s.client.Get("/api/v1/deploy-tasks", q)
	case "postdare_go.get_deploy_task":
		result, err = s.client.Get(fmt.Sprintf("/api/v1/deploy-tasks/%d", uintArg(args, "task_id")), nil)
	case "postdare_go.read_deploy_log":
		q := url.Values{}
		q.Set("lines", strconv.FormatUint(defaultUint(uintArg(args, "lines"), 200), 10))
		result, err = s.client.Get(fmt.Sprintf("/api/v1/deploy-tasks/%d/logs", uintArg(args, "task_id")), q)
	case "postdare_go.read_app_log":
		q := url.Values{}
		q.Set("lines", strconv.FormatUint(defaultUint(uintArg(args, "lines"), 200), 10))
		result, err = s.client.Get(fmt.Sprintf("/api/v1/projects/%d/app-logs", uintArg(args, "project_id")), q)
	case "postdare_go.trigger_deploy":
		result, err = s.client.Post(fmt.Sprintf("/api/v1/projects/%d/deploy-tasks", uintArg(args, "project_id")), map[string]bool{"confirm": boolArg(args, "confirm")})
	case "postdare_go.trigger_rollback":
		result, err = s.client.Post(fmt.Sprintf("/api/v1/projects/%d/rollback-tasks", uintArg(args, "project_id")), map[string]bool{"confirm": boolArg(args, "confirm")})
	case "postdare_go.analyze_failed_deploy":
		result, err = s.client.Get(fmt.Sprintf("/api/v1/deploy-tasks/%d/analysis", uintArg(args, "task_id")), nil)
	case "postdare_go.list_deploy_task_reports":
		result, err = s.client.Get(fmt.Sprintf("/api/v1/deploy-tasks/%d/reports", uintArg(args, "task_id")), nil)
	case "postdare_go.get_report":
		result, err = s.client.Get(fmt.Sprintf("/api/v1/reports/%d", uintArg(args, "report_id")), nil)
	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
	if err != nil {
		return nil, err
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]interface{}{
		"content": []map[string]string{{"type": "text", "text": string(text)}},
		"isError": false,
	}, nil
}

func (s *Server) readResource(uri string) (interface{}, error) {
	var result interface{}
	var err error
	switch {
	case uri == "postdare-go://projects":
		result, err = s.client.Get("/api/v1/projects", nil)
	case strings.HasPrefix(uri, "postdare-go://projects/") && strings.HasSuffix(uri, "/app-logs"):
		id := between(uri, "postdare-go://projects/", "/app-logs")
		q := url.Values{"lines": []string{"200"}}
		result, err = s.client.Get("/api/v1/projects/"+id+"/app-logs", q)
	case strings.HasPrefix(uri, "postdare-go://projects/"):
		id := strings.TrimPrefix(uri, "postdare-go://projects/")
		result, err = s.client.Get("/api/v1/projects/"+id, nil)
	case strings.HasPrefix(uri, "postdare-go://reports/"):
		id := strings.TrimPrefix(uri, "postdare-go://reports/")
		result, err = s.client.Get("/api/v1/reports/"+id, nil)
	case strings.HasPrefix(uri, "postdare-go://deploy-tasks/") && strings.HasSuffix(uri, "/reports"):
		id := between(uri, "postdare-go://deploy-tasks/", "/reports")
		result, err = s.client.Get("/api/v1/deploy-tasks/"+id+"/reports", nil)
	case strings.HasPrefix(uri, "postdare-go://deploy-tasks/") && strings.HasSuffix(uri, "/logs"):
		id := between(uri, "postdare-go://deploy-tasks/", "/logs")
		q := url.Values{"lines": []string{"200"}}
		result, err = s.client.Get("/api/v1/deploy-tasks/"+id+"/logs", q)
	case strings.HasPrefix(uri, "postdare-go://deploy-tasks/"):
		id := strings.TrimPrefix(uri, "postdare-go://deploy-tasks/")
		result, err = s.client.Get("/api/v1/deploy-tasks/"+id, nil)
	default:
		return nil, fmt.Errorf("unknown resource %s", uri)
	}
	if err != nil {
		return nil, err
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]interface{}{
		"contents": []map[string]string{{
			"uri":      uri,
			"mimeType": "application/json",
			"text":     string(text),
		}},
	}, nil
}

func (c *RESTClient) Get(path string, q url.Values) (interface{}, error) {
	if q == nil {
		q = url.Values{}
	}
	return c.Do(http.MethodGet, path, q, nil)
}

func (c *RESTClient) Post(path string, body interface{}) (interface{}, error) {
	return c.Do(http.MethodPost, path, nil, body)
}

func (c *RESTClient) Do(method string, path string, q url.Values, body interface{}) (interface{}, error) {
	endpoint := c.BaseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("REST %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return decoded, nil
}

func tools() []map[string]interface{} {
	schema := func(props map[string]interface{}, required []string) map[string]interface{} {
		return map[string]interface{}{"type": "object", "properties": props, "required": required}
	}
	intProp := map[string]string{"type": "integer"}
	strProp := map[string]string{"type": "string"}
	boolProp := map[string]string{"type": "boolean"}
	return []map[string]interface{}{
		{"name": "postdare_go.list_projects", "description": "List Postdare Go projects.", "inputSchema": schema(map[string]interface{}{"status": strProp, "git_provider": strProp}, nil)},
		{"name": "postdare_go.get_project", "description": "Get a project by id with sensitive fields masked.", "inputSchema": schema(map[string]interface{}{"project_id": intProp}, []string{"project_id"})},
		{"name": "postdare_go.list_deploy_tasks", "description": "List recent deploy tasks.", "inputSchema": schema(map[string]interface{}{"project_id": intProp, "status": strProp, "limit": intProp}, nil)},
		{"name": "postdare_go.get_deploy_task", "description": "Get deploy task detail and stages.", "inputSchema": schema(map[string]interface{}{"task_id": intProp}, []string{"task_id"})},
		{"name": "postdare_go.read_deploy_log", "description": "Read deploy log tail.", "inputSchema": schema(map[string]interface{}{"task_id": intProp, "lines": intProp}, []string{"task_id"})},
		{"name": "postdare_go.read_app_log", "description": "Read application log tail.", "inputSchema": schema(map[string]interface{}{"project_id": intProp, "lines": intProp}, []string{"project_id"})},
		{"name": "postdare_go.trigger_deploy", "description": "Trigger a deploy. Requires backend mcp.allow_mutation_tools=true and confirm=true.", "inputSchema": schema(map[string]interface{}{"project_id": intProp, "confirm": boolProp}, []string{"project_id", "confirm"})},
		{"name": "postdare_go.trigger_rollback", "description": "Trigger rollback. Requires backend mcp.allow_mutation_tools=true and confirm=true.", "inputSchema": schema(map[string]interface{}{"project_id": intProp, "confirm": boolProp}, []string{"project_id", "confirm"})},
		{"name": "postdare_go.analyze_failed_deploy", "description": "Analyze failed deploy by rules and logs.", "inputSchema": schema(map[string]interface{}{"task_id": intProp}, []string{"task_id"})},
		{"name": "postdare_go.list_deploy_task_reports", "description": "List reports captured by a deploy task, with summary, issues and markdown.", "inputSchema": schema(map[string]interface{}{"task_id": intProp}, []string{"task_id"})},
		{"name": "postdare_go.get_report", "description": "Get one report by id, including its issues and markdown body.", "inputSchema": schema(map[string]interface{}{"report_id": intProp}, []string{"report_id"})},
	}
}

func resources() []map[string]string {
	return []map[string]string{
		{"uri": "postdare-go://projects", "name": "Projects", "mimeType": "application/json"},
	}
}

func resourceTemplates() []map[string]string {
	return []map[string]string{
		{"uriTemplate": "postdare-go://projects/{project_id}", "name": "Project detail", "mimeType": "application/json"},
		{"uriTemplate": "postdare-go://deploy-tasks/{task_id}", "name": "Deploy task detail", "mimeType": "application/json"},
		{"uriTemplate": "postdare-go://deploy-tasks/{task_id}/logs", "name": "Deploy logs", "mimeType": "application/json"},
		{"uriTemplate": "postdare-go://projects/{project_id}/app-logs", "name": "Application logs", "mimeType": "application/json"},
		{"uriTemplate": "postdare-go://deploy-tasks/{task_id}/reports", "name": "Deploy task reports", "mimeType": "application/json"},
		{"uriTemplate": "postdare-go://reports/{report_id}", "name": "Report detail", "mimeType": "application/json"},
	}
}

func prompts() []map[string]interface{} {
	return []map[string]interface{}{
		{"name": "analyze_deploy_failure", "description": "Analyze deploy task, stages, and logs.", "arguments": []map[string]string{{"name": "task_id", "description": "Deploy task id", "required": "true"}}},
		{"name": "generate_release_summary", "description": "Generate release summary from recent deployment.", "arguments": []map[string]string{{"name": "task_id", "description": "Deploy task id", "required": "false"}}},
		{"name": "suggest_ci_improvements", "description": "Suggest improvements for tests, build, health check, and alerts.", "arguments": []map[string]string{{"name": "project_id", "description": "Project id", "required": "true"}}},
	}
}

func getPrompt(name string, args map[string]interface{}) (interface{}, error) {
	var text string
	switch name {
	case "analyze_deploy_failure":
		text = "请根据 Postdare Go 部署任务、阶段状态和部署日志，分析失败原因并给出可执行处理建议。任务 ID: " + fmt.Sprint(args["task_id"])
	case "generate_release_summary":
		text = "请根据 Postdare Go 最近一次或指定部署记录生成发布摘要，包含项目、分支、commit、阶段结果、耗时和风险。任务 ID: " + fmt.Sprint(args["task_id"])
	case "suggest_ci_improvements":
		text = "请根据 Postdare Go 项目配置和最近部署记录，建议如何优化单元测试、集成测试、构建、健康检查和失败告警。项目 ID: " + fmt.Sprint(args["project_id"])
	default:
		return nil, fmt.Errorf("unknown prompt %s", name)
	}
	return map[string]interface{}{
		"description": name,
		"messages": []map[string]interface{}{{
			"role": "user",
			"content": map[string]string{
				"type": "text",
				"text": text,
			},
		}},
	}, nil
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func uintArg(args map[string]interface{}, key string) uint64 {
	switch v := args[key].(type) {
	case float64:
		return uint64(v)
	case int:
		return uint64(v)
	case json.Number:
		n, _ := strconv.ParseUint(string(v), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseUint(v, 10, 64)
		return n
	default:
		return 0
	}
}

func boolArg(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func defaultUint(v uint64, def uint64) uint64 {
	if v == 0 {
		return def
	}
	return v
}

func between(s string, prefix string, suffix string) string {
	s = strings.TrimPrefix(s, prefix)
	return strings.TrimSuffix(s, suffix)
}

func mustJSON(v interface{}) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"json marshal failed"}}`)
	}
	return raw
}

func EnvServer() *Server {
	return NewServer(os.Getenv("POSTDARE_GO_BASE_URL"), os.Getenv("POSTDARE_GO_API_TOKEN"))
}
