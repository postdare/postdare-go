package webhook

import "testing"

func TestParseIssueRefsClosingKeywords(t *testing.T) {
	refs := ParseIssueRefs("abc123", "fix ENG-42: drop the stale cache")
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %#v", refs)
	}
	if refs[0].BoardKey != "ENG" || refs[0].Number != 42 || !refs[0].Closing {
		t.Fatalf("unexpected ref %#v", refs[0])
	}
	if refs[0].CommitID != "abc123" {
		t.Fatalf("expected commit id to be carried, got %q", refs[0].CommitID)
	}
}

func TestParseIssueRefsBareMentionDoesNotClose(t *testing.T) {
	refs := ParseIssueRefs("", "follow-up for ENG-7")
	if len(refs) != 1 || refs[0].Closing {
		t.Fatalf("expected a non-closing ref, got %#v", refs)
	}
}

func TestParseIssueRefsLowercaseAndDedup(t *testing.T) {
	refs := ParseIssueRefs("", "refs eng-1 and then fixes ENG-1")
	if len(refs) != 1 {
		t.Fatalf("expected the same issue once, got %#v", refs)
	}
	if refs[0].BoardKey != "ENG" || !refs[0].Closing {
		t.Fatalf("expected the closing mention to win, got %#v", refs[0])
	}
}

func TestParseIssueRefsKeywordVariants(t *testing.T) {
	for _, message := range []string{"closes ENG-3", "closed ENG-3", "fixed ENG-3", "resolves ENG-3", "Fixes: ENG-3"} {
		refs := ParseIssueRefs("", message)
		if len(refs) != 1 || !refs[0].Closing {
			t.Fatalf("expected %q to close, got %#v", message, refs)
		}
	}
}

// An identifier-shaped string whose prefix is not a board is still parsed here;
// dropping it is the caller's job, since only the database knows the board keys.
func TestParseIssueRefsIgnoresMalformedIdentifiers(t *testing.T) {
	refs := ParseIssueRefs("", "bumped to ENG-0 and E-1 and 9X-2")
	for _, ref := range refs {
		if ref.Number == 0 {
			t.Fatalf("expected zero-numbered identifiers to be dropped, got %#v", refs)
		}
		if ref.BoardKey == "E" || ref.BoardKey == "9X" {
			t.Fatalf("expected malformed keys to be dropped, got %#v", refs)
		}
	}
}

func TestEventIssueRefsScansEveryCommit(t *testing.T) {
	event := &Event{
		CommitID:      "head",
		CommitMessage: "Merge pull request #22",
		Commits: []Commit{
			{ID: "c1", Message: "fixes ENG-1"},
			{ID: "c2", Message: "refs ENG-2"},
			{ID: "head", Message: "Merge pull request #22"},
		},
	}
	refs := event.IssueRefs()
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %#v", refs)
	}
	if refs[0].Number != 1 || !refs[0].Closing || refs[0].CommitID != "c1" {
		t.Fatalf("unexpected first ref %#v", refs[0])
	}
	if refs[1].Number != 2 || refs[1].Closing {
		t.Fatalf("unexpected second ref %#v", refs[1])
	}
}

// Gitee and GitHub both send a commits array, but a payload without one must
// still yield the head commit's references rather than nothing.
func TestEventIssueRefsFallsBackToHeadCommit(t *testing.T) {
	event := &Event{CommitID: "head", CommitMessage: "fix ENG-9"}
	refs := event.IssueRefs()
	if len(refs) != 1 || refs[0].Number != 9 || !refs[0].Closing {
		t.Fatalf("unexpected refs %#v", refs)
	}
}

func TestParseCommitsFromPushPayload(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main","after":"c2","before":"c0","head_commit":{"id":"c2","message":"second"},"commits":[{"id":"c1","message":"fixes ENG-4"},{"id":"c2","message":"second"},{"id":"c3","message":""}]}`)
	event, err := GitHubWebhookParser{}.Parse(map[string][]string{"X-Github-Event": {"push"}}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Commits) != 2 {
		t.Fatalf("expected empty messages to be dropped, got %#v", event.Commits)
	}
	refs := event.IssueRefs()
	if len(refs) != 1 || refs[0].Number != 4 || !refs[0].Closing {
		t.Fatalf("unexpected refs %#v", refs)
	}
}
