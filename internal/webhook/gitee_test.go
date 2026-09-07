package webhook

import (
	"net/http"
	"testing"
)

func TestGiteeParserNormalizesPushEvents(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		hookName  string
		wantEvent string
	}{
		{name: "plain push", hookName: "push", wantEvent: "push"},
		{name: "push hooks", hookName: "push_hooks", wantEvent: "push"},
		{name: "push hook header", header: "Push Hook", hookName: "push_hooks", wantEvent: "push"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			if tt.header != "" {
				headers.Set("X-Gitee-Event", tt.header)
			}
			body := []byte(`{"hook_name":"` + tt.hookName + `","ref":"refs/heads/main","before":"base123","after":"abc123","head_commit":{"message":"test","author":{"name":"kim"}}}`)

			event, err := GiteeWebhookParser{}.Parse(headers, body)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if event.EventType != tt.wantEvent {
				t.Fatalf("EventType = %q, want %q", event.EventType, tt.wantEvent)
			}
			if event.BeforeCommitID != "base123" {
				t.Fatalf("BeforeCommitID = %q, want base123", event.BeforeCommitID)
			}
		})
	}
}
