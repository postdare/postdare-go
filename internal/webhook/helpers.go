package webhook

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func mapFromMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	child, _ := m[key].(map[string]interface{})
	return child
}

// commitsFromMap reads the push's commit list, keeping only entries that carry
// a message: an empty one can neither link nor close an issue.
func commitsFromMap(m map[string]interface{}, key string) []Commit {
	raw, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	commits := make([]Commit, 0, len(raw))
	for _, entry := range raw {
		commit, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		message := stringFromMap(commit, "message")
		if message == "" {
			continue
		}
		commits = append(commits, Commit{ID: stringFromMap(commit, "id"), Message: message})
	}
	if len(commits) == 0 {
		return nil
	}
	return commits
}
