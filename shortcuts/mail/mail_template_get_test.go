// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/httpmock"
)

func TestMailTemplateGet_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "non-decimal template-id",
			args:    []string{"+template-get", "--template-id", "abc"},
			wantErr: "decimal integer string",
		},
		{
			name:    "negative template-id",
			args:    []string{"+template-get", "--template-id", "-1"},
			wantErr: "decimal integer string",
		},
		{
			name:    "template-id with whitespace",
			args:    []string{"+template-get", "--template-id", "12 3"},
			wantErr: "decimal integer string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, stdout, _, _ := mailShortcutTestFactory(t)
			err := runMountedMailShortcut(t, MailTemplateGet, tt.args, f, stdout)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestMailTemplateGet_MissingRequiredFlag(t *testing.T) {
	f, stdout, _, _ := mailShortcutTestFactory(t)
	err := runMountedMailShortcut(t, MailTemplateGet, []string{"+template-get"}, f, stdout)
	if err == nil {
		t.Fatal("expected error for missing --template-id, got nil")
	}
	if !strings.Contains(err.Error(), "template-id") {
		t.Fatalf("expected error to mention --template-id, got %q", err.Error())
	}
}

func TestMailTemplateGet_ExecuteJSONPassThrough(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/12345",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template_id":      "12345",
				"name":             "Quarterly report",
				"subject":          "Hello",
				"template_content": "<p>body</p>",
				"to":               []interface{}{map[string]interface{}{"mail_address": "a@example.com"}},
				"cc":               []interface{}{map[string]interface{}{"mail_address": "b@example.com"}},
				"bcc":              []interface{}{},
				"attachments":      []interface{}{map[string]interface{}{"id": "att_1"}},
			},
		},
	})

	err := runMountedMailShortcut(t, MailTemplateGet, []string{
		"+template-get", "--template-id", "12345",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	// json.Marshal HTML-escapes `<` / `>` to `<` / `>`.
	for _, want := range []string{`"template_id"`, `"12345"`, `"Quarterly report"`, `"template_content"`, "\\u003cp\\u003ebody\\u003c/p\\u003e"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected JSON pass-through output to contain %q, got %s", want, out)
		}
	}
}

func TestMailTemplateGet_PathEscapesMailbox(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		// `@` is a sub-delim that url.PathEscape leaves untouched, so the
		// outgoing URL keeps the literal `@`. mailboxPath would still escape
		// reserved chars (e.g. `/`); this guards against accidental raw
		// fmt.Sprintf regression on the segment.
		URL: "/user_mailboxes/user@example.com/templates/9",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template_id": "9",
				"name":        "x",
				"subject":     "y",
			},
		},
	})

	err := runMountedMailShortcut(t, MailTemplateGet, []string{
		"+template-get", "--mailbox", "user@example.com", "--template-id", "9",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMailTemplateGet_APIError(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/77",
		Body: map[string]interface{}{
			"code": 99991663,
			"msg":  "template not found",
		},
	})

	err := runMountedMailShortcut(t, MailTemplateGet, []string{
		"+template-get", "--template-id", "77",
	}, f, stdout)
	if err == nil {
		t.Fatal("expected error for non-zero API code, got nil")
	}
	if !strings.Contains(err.Error(), "template not found") {
		t.Errorf("expected error to surface server msg, got %q", err.Error())
	}
}

func TestMailTemplateGet_PrettyTruncation(t *testing.T) {
	long := strings.Repeat("a", 500)
	data := map[string]interface{}{
		"template_id":      "1",
		"name":             "n",
		"subject":          "s",
		"template_content": long,
		"to":               []interface{}{map[string]interface{}{}, map[string]interface{}{}},
		"cc":               []interface{}{map[string]interface{}{}},
		"attachments":      []interface{}{map[string]interface{}{}, map[string]interface{}{}, map[string]interface{}{}},
	}

	if got := attachmentCount(data); got != 3 {
		t.Errorf("attachmentCount = %d, want 3", got)
	}
	if got := recipientCount(data); got != 3 {
		t.Errorf("recipientCount = %d, want 3", got)
	}
	// Defensive recipient key fallback.
	dataAlt := map[string]interface{}{
		"to_recipients":  []interface{}{map[string]interface{}{}},
		"cc_recipients":  []interface{}{map[string]interface{}{}},
		"bcc_recipients": []interface{}{map[string]interface{}{}, map[string]interface{}{}},
	}
	if got := recipientCount(dataAlt); got != 4 {
		t.Errorf("recipientCount fallback = %d, want 4", got)
	}

	var buf strings.Builder
	renderTemplatePretty(&buf, data)
	out := buf.String()
	if !strings.Contains(out, strings.Repeat("a", 200)) {
		t.Errorf("expected 200 a's in pretty output")
	}
	if strings.Contains(out, strings.Repeat("a", 201)) {
		t.Errorf("template_content was not truncated to 200 runes")
	}
}
