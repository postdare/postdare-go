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

// Commit is one commit carried by a push. The slice matters for issue linking:
// the commit that closes an issue is usually not the head commit.
type Commit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type Event struct {
	Provider       GitProvider `json:"provider"`
	EventType      string      `json:"event_type"`
	Branch         string      `json:"branch"`
	CommitID       string      `json:"commit_id"`
	BeforeCommitID string      `json:"before_commit_id"`
	CommitMessage  string      `json:"commit_message"`
	CommitAuthor   string      `json:"commit_author"`
	DeliveryID     string      `json:"delivery_id"`
	Commits        []Commit    `json:"commits,omitempty"`
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
