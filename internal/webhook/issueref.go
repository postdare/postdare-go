package webhook

import (
	"regexp"
	"strconv"
	"strings"
)

// IssueRef is one board issue named by a commit message. Closing records that
// the commit used a keyword like "fixes", which is the difference between a
// commit that finishes an issue and one that merely refers to it.
type IssueRef struct {
	BoardKey string
	Number   uint64
	Closing  bool
	CommitID string
}

// issueRefPattern matches an identifier like ENG-42, optionally preceded by a
// closing keyword. Matching is case-insensitive because people type "eng-42",
// and the key is uppercased before it is looked up; a reference to a board that
// does not exist is dropped by the caller, which is what keeps incidental
// matches like "UTF-8" from linking anything.
var issueRefPattern = regexp.MustCompile(`(?i)\b(?:(close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+)?([a-z][a-z0-9]{1,9})-(\d{1,9})\b`)

// ParseIssueRefs extracts the issue references in a commit message. The same
// issue mentioned twice is returned once, and a closing mention wins over a
// bare one so "refs ENG-1, fixes ENG-1" still closes the issue.
func ParseIssueRefs(commitID string, message string) []IssueRef {
	matches := issueRefPattern.FindAllStringSubmatch(message, -1)
	refs := make([]IssueRef, 0, len(matches))
	seen := map[string]int{}
	for _, match := range matches {
		key := strings.ToUpper(match[2])
		number, err := strconv.ParseUint(match[3], 10, 64)
		if err != nil || number == 0 {
			continue
		}
		closing := match[1] != ""
		id := key + "-" + match[3]
		if at, ok := seen[id]; ok {
			if closing {
				refs[at].Closing = true
			}
			continue
		}
		seen[id] = len(refs)
		refs = append(refs, IssueRef{BoardKey: key, Number: number, Closing: closing, CommitID: commitID})
	}
	return refs
}

// IssueRefs collects the references across every commit in a push. A push is
// scanned commit by commit rather than from the head message alone, because the
// commit that says "fixes ENG-42" is very often not the last one.
func (e *Event) IssueRefs() []IssueRef {
	commits := e.Commits
	if len(commits) == 0 {
		commits = []Commit{{ID: e.CommitID, Message: e.CommitMessage}}
	}
	refs := make([]IssueRef, 0)
	seen := map[string]int{}
	for _, commit := range commits {
		for _, ref := range ParseIssueRefs(commit.ID, commit.Message) {
			id := ref.BoardKey + "-" + strconv.FormatUint(ref.Number, 10)
			if at, ok := seen[id]; ok {
				if ref.Closing && !refs[at].Closing {
					refs[at].Closing = true
					refs[at].CommitID = ref.CommitID
				}
				continue
			}
			seen[id] = len(refs)
			refs = append(refs, ref)
		}
	}
	return refs
}
