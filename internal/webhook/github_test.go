package webhook

import (
	"net/http"
	"testing"
)

func TestGitHubParserIncludesBeforeAndAfterCommits(t *testing.T) {
	headers := http.Header{"X-Github-Event": []string{"push"}}
	event, err := (GitHubWebhookParser{}).Parse(headers, []byte(`{"ref":"refs/heads/main","before":"abc123","after":"def456","head_commit":{"id":"def456"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.BeforeCommitID != "abc123" || event.CommitID != "def456" {
		t.Fatalf("unexpected commit range: %s..%s", event.BeforeCommitID, event.CommitID)
	}
}
