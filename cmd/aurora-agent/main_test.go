package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestJournalResultTruncatesLongStrings(t *testing.T) {
	raw := json.RawMessage(`{"url":"https://example.com","body":"` + strings.Repeat("x", journalStringLimit+10) + `"}`)

	result, err := journalResult(raw)
	if err != nil {
		t.Fatalf("journal result: %v", err)
	}
	object, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	body, ok := object["body"].(string)
	if !ok {
		t.Fatalf("body type = %T, want string", object["body"])
	}
	if got := utf8.RuneCountInString(body); got != journalStringLimit+len([]rune("[...]")) {
		t.Fatalf("body length = %d, want %d", got, journalStringLimit+len([]rune("[...]")))
	}
	if !strings.HasSuffix(body, "[...]") {
		t.Fatalf("body does not end with truncation marker: %q", body)
	}
	if object["url"] != "https://example.com" {
		t.Fatalf("short value changed: %v", object["url"])
	}
}

func TestJournalResultCountsUnicodeCharacters(t *testing.T) {
	raw, err := json.Marshal(map[string]string{
		"body": strings.Repeat("界", journalStringLimit+1),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	result, err := journalResult(raw)
	if err != nil {
		t.Fatalf("journal result: %v", err)
	}
	body := result.(map[string]any)["body"].(string)
	if got := utf8.RuneCountInString(strings.TrimSuffix(body, "[...]")); got != journalStringLimit {
		t.Fatalf("retained characters = %d, want %d", got, journalStringLimit)
	}
}

func TestJournalJSONTruncatesNestedCallContent(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "tool",
				"content": strings.Repeat("page", journalStringLimit),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	value, err := journalJSON(raw)
	if err != nil {
		t.Fatalf("journal json: %v", err)
	}
	messages := value.(map[string]any)["messages"].([]any)
	content := messages[0].(map[string]any)["content"].(string)
	if !strings.HasSuffix(content, "[...]") {
		t.Fatalf("nested content was not truncated: %q", content)
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(content, "[...]")); got != journalStringLimit {
		t.Fatalf("retained characters = %d, want %d", got, journalStringLimit)
	}
}
