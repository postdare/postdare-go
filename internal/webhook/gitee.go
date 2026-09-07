package webhook

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type GiteeWebhookParser struct{}

func (GiteeWebhookParser) VerifySignature(secret string, headers http.Header, _ []byte) bool {
	if secret == "" {
		return false
	}
	for _, name := range []string{"X-Gitee-Token", "X-Git-Osc-Token"} {
		token := headers.Get(name)
		if len(token) == len(secret) && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1 {
			return true
		}
	}
	return false
}

func (GiteeWebhookParser) Parse(headers http.Header, body []byte) (*Event, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	eventType := normalizeGiteeEventType(firstNonEmpty(headers.Get("X-Gitee-Event"), stringFromMap(payload, "hook_name"), stringFromMap(payload, "event_name")))
	if eventType == "" {
		eventType = "push"
	}
	commit := mapFromMap(payload, "head_commit")
	commitID := firstNonEmpty(stringFromMap(payload, "after"), stringFromMap(commit, "id"))
	if commitID == "" {
		if last := lastCommit(payload); last != nil {
			commit = last
			commitID = stringFromMap(last, "id")
		}
	}
	author := mapFromMap(commit, "author")
	return &Event{
		Provider:       GitProviderGitee,
		EventType:      eventType,
		Branch:         BranchFromRef(stringFromMap(payload, "ref")),
		CommitID:       commitID,
		BeforeCommitID: stringFromMap(payload, "before"),
		CommitMessage:  stringFromMap(commit, "message"),
		CommitAuthor:   firstNonEmpty(stringFromMap(author, "name"), stringFromMap(author, "username"), stringFromMap(author, "email")),
		DeliveryID:     firstNonEmpty(headers.Get("X-Gitee-Delivery"), headers.Get("X-Git-Osc-Delivery")),
		RawPayload:     body,
	}, nil
}

func normalizeGiteeEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	normalized := strings.ToLower(strings.NewReplacer("_", " ", "-", " ").Replace(eventType))
	fields := strings.Fields(normalized)
	if len(fields) > 0 && fields[0] == "push" {
		return "push"
	}
	return eventType
}

func lastCommit(payload map[string]interface{}) map[string]interface{} {
	raw, ok := payload["commits"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	commit, _ := raw[len(raw)-1].(map[string]interface{})
	return commit
}
