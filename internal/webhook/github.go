package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

type GitHubWebhookParser struct{}

func (GitHubWebhookParser) VerifySignature(secret string, headers http.Header, body []byte) bool {
	if secret == "" {
		return false
	}
	got := headers.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(got, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(got), []byte(expected))
}

func (GitHubWebhookParser) Parse(headers http.Header, body []byte) (*Event, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	commit := mapFromMap(payload, "head_commit")
	author := mapFromMap(commit, "author")
	return &Event{
		Provider:       GitProviderGitHub,
		EventType:      headers.Get("X-GitHub-Event"),
		Branch:         BranchFromRef(stringFromMap(payload, "ref")),
		CommitID:       firstNonEmpty(stringFromMap(payload, "after"), stringFromMap(commit, "id")),
		BeforeCommitID: stringFromMap(payload, "before"),
		CommitMessage:  stringFromMap(commit, "message"),
		CommitAuthor:   firstNonEmpty(stringFromMap(author, "name"), stringFromMap(author, "username"), stringFromMap(author, "email")),
		DeliveryID:     headers.Get("X-GitHub-Delivery"),
		RawPayload:     body,
	}, nil
}
