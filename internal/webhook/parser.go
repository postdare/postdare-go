package webhook

import (
	"net/http"
	"strings"
)

type GitProvider string

const (
	GitProviderGitee  GitProvider = "gitee"
	GitProviderGitHub GitProvider = "github"
)

type Event struct {
	Provider       GitProvider `json:"provider"`
	EventType      string      `json:"event_type"`
	Branch         string      `json:"branch"`
	CommitID       string      `json:"commit_id"`
	BeforeCommitID string      `json:"before_commit_id"`
	CommitMessage  string      `json:"commit_message"`
	CommitAuthor   string      `json:"commit_author"`
	DeliveryID     string      `json:"delivery_id"`
	RawPayload     []byte      `json:"-"`
}

type WebhookParser interface {
	VerifySignature(secret string, headers http.Header, body []byte) bool
	Parse(headers http.Header, body []byte) (*Event, error)
}

func BranchFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return ref
}
