// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/httpmock"
)

// decodeCapturedBody JSON-parses a stub's captured request body. Returns nil
// when the stub was never hit.
func decodeCapturedBody(t *testing.T, stub *httpmock.Stub) map[string]interface{} {
	t.Helper()
	if stub == nil || len(stub.CapturedBody) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(stub.CapturedBody, &out); err != nil {
		t.Fatalf("decode captured body: %v (raw=%s)", err, stub.CapturedBody)
	}
	return out
}

// TestMailTemplateCreate_Happy verifies a +template-create call with no local
// <img> references and no --attach files POSTs the expected body and emits
// the server's echoed template.
func TestMailTemplateCreate_Happy(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	stub := &httpmock.Stub{
		Method: "POST",
		URL:    "/user_mailboxes/me/templates",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id":        "tpl_001",
					"name":               "Quarterly",
					"subject":            "Q4",
					"template_content":   "<p>hi</p>",
					"is_plain_text_mode": false,
				},
			},
		},
	}
	reg.Register(stub)

	err := runMountedMailShortcut(t, MailTemplateCreate, []string{
		"+template-create",
		"--name", "Quarterly",
		"--subject", "Q4",
		"--template-content", "<p>hi</p>",
		"--to", "alice@example.com",
	}, f, stdout)
	if err != nil {
		t.Fatalf("template-create failed: %v", err)
	}

	capturedBody := decodeCapturedBody(t, stub)
	if capturedBody == nil {
		t.Fatalf("expected POST body captured")
	}
	tplWrap, ok := capturedBody["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("template wrapper missing: %#v", capturedBody)
	}
	if tplWrap["name"] != "Quarterly" {
		t.Errorf("name: %v", tplWrap["name"])
	}
	if tplWrap["template_content"] != "<p>hi</p>" {
		t.Errorf("template_content unexpectedly wrapped: %v", tplWrap["template_content"])
	}

	data := decodeShortcutEnvelopeData(t, stdout)
	tpl, ok := data["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("output envelope template missing: %#v", data)
	}
	if tpl["template_id"] != "tpl_001" {
		t.Errorf("template_id = %v", tpl["template_id"])
	}
}

// TestMailTemplateCreate_PlainTextWrap verifies that a non-HTML content in
// HTML mode is line-break-wrapped before being sent.
func TestMailTemplateCreate_PlainTextWrap(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	stub := &httpmock.Stub{
		Method: "POST",
		URL:    "/user_mailboxes/me/templates",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{"template_id": "tpl_002", "name": "Multi"},
			},
		},
	}
	reg.Register(stub)

	err := runMountedMailShortcut(t, MailTemplateCreate, []string{
		"+template-create",
		"--name", "Multi",
		"--template-content", "line1\nline2",
	}, f, stdout)
	if err != nil {
		t.Fatalf("template-create failed: %v", err)
	}
	capturedBody := decodeCapturedBody(t, stub)
	if capturedBody == nil {
		t.Fatalf("expected captured body")
	}
	tplWrap := capturedBody["template"].(map[string]interface{})
	tc, _ := tplWrap["template_content"].(string)
	if tc == "line1\nline2" || !strings.Contains(tc, "line1") {
		t.Errorf("expected line-break wrapped content, got %q", tc)
	}
}

// TestMailTemplateCreate_ValidateErrors verifies Validate-layer errors fire
// before any network call.
func TestMailTemplateCreate_ValidateErrors(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		expect string
	}{
		{
			"name required",
			[]string{"+template-create"},
			`required flag(s) "name" not set`,
		},
		{
			"name too long",
			[]string{"+template-create", "--name", strings.Repeat("x", 101)},
			"--name must be at most 100 characters",
		},
		{
			"mutual exclusion",
			[]string{
				"+template-create",
				"--name", "n",
				"--template-content", "a",
				"--template-content-file", "b",
			},
			"--template-content and --template-content-file are mutually exclusive",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, stdout, _, _ := mailShortcutTestFactory(t)
			err := runMountedMailShortcut(t, MailTemplateCreate, c.args, f, stdout)
			if err == nil || !strings.Contains(err.Error(), c.expect) {
				t.Fatalf("expected %q, got %v", c.expect, err)
			}
		})
	}
}

// TestMailTemplateUpdate_PrintPatchTemplate verifies --print-patch-template is
// network-free and emits the skeleton fields.
func TestMailTemplateUpdate_PrintPatchTemplate(t *testing.T) {
	f, stdout, _, _ := mailShortcutTestFactory(t)
	err := runMountedMailShortcut(t, MailTemplateUpdate, []string{
		"+template-update",
		"--print-patch-template",
	}, f, stdout)
	if err != nil {
		t.Fatalf("print-patch-template failed: %v", err)
	}
	data := decodeShortcutEnvelopeData(t, stdout)
	for _, key := range []string{"name", "subject", "template_content", "is_plain_text_mode", "tos", "ccs", "bccs"} {
		if _, ok := data[key]; !ok {
			t.Errorf("skeleton missing %q; got %#v", key, data)
		}
	}
}

// TestMailTemplateUpdate_Inspect verifies --inspect calls GET and returns the
// fetched template without a PUT.
func TestMailTemplateUpdate_Inspect(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/42",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id":        "42",
					"name":               "Weekly",
					"subject":            "W",
					"is_plain_text_mode": false,
				},
			},
		},
	})

	err := runMountedMailShortcut(t, MailTemplateUpdate, []string{
		"+template-update",
		"--template-id", "42",
		"--inspect",
	}, f, stdout)
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	data := decodeShortcutEnvelopeData(t, stdout)
	tpl, ok := data["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("template wrapper missing: %#v", data)
	}
	if tpl["template_id"] != "42" {
		t.Errorf("template_id = %v", tpl["template_id"])
	}
}

// TestMailTemplateUpdate_Happy verifies GET + PUT flow with --set-subject.
func TestMailTemplateUpdate_Happy(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/42",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id":        "42",
					"name":               "Orig",
					"subject":            "old-subj",
					"template_content":   "<p>body</p>",
					"is_plain_text_mode": false,
					"tos":                []interface{}{map[string]interface{}{"mail_address": "a@x"}},
				},
			},
		},
	})
	putStub := &httpmock.Stub{
		Method: "PUT",
		URL:    "/user_mailboxes/me/templates/42",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id": "42",
					"name":        "Orig",
					"subject":     "new-subj",
				},
			},
		},
	}
	reg.Register(putStub)

	err := runMountedMailShortcut(t, MailTemplateUpdate, []string{
		"+template-update",
		"--template-id", "42",
		"--set-subject", "new-subj",
	}, f, stdout)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	putBody := decodeCapturedBody(t, putStub)
	if putBody == nil {
		t.Fatalf("expected PUT body captured")
	}
	tplWrap, ok := putBody["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("PUT body missing template wrapper: %#v", putBody)
	}
	if tplWrap["subject"] != "new-subj" {
		t.Errorf("subject not updated: %v", tplWrap["subject"])
	}
	// Name preserved from GET.
	if tplWrap["name"] != "Orig" {
		t.Errorf("name not preserved: %v", tplWrap["name"])
	}
}

// TestMailTemplateCreate_TemplateContentFile verifies --template-content-file
// loads body from disk.
func TestMailTemplateCreate_TemplateContentFile(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("body.html", []byte("<p>from-file</p>"), 0o644); err != nil {
		t.Fatalf("write body.html: %v", err)
	}

	f, stdout, _, reg := mailShortcutTestFactory(t)
	stub := &httpmock.Stub{
		Method: "POST",
		URL:    "/user_mailboxes/me/templates",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{"template_id": "tpl_003", "name": "FromFile"},
			},
		},
	}
	reg.Register(stub)

	err := runMountedMailShortcut(t, MailTemplateCreate, []string{
		"+template-create",
		"--name", "FromFile",
		"--template-content-file", "body.html",
	}, f, stdout)
	if err != nil {
		t.Fatalf("template-create failed: %v", err)
	}
	capturedBody := decodeCapturedBody(t, stub)
	if capturedBody == nil {
		t.Fatalf("expected captured body")
	}
	tplWrap := capturedBody["template"].(map[string]interface{})
	if tc, _ := tplWrap["template_content"].(string); !strings.Contains(tc, "from-file") {
		t.Errorf("template_content missing file contents: %q", tc)
	}
}

// TestMailTemplateUpdate_PatchFile verifies --patch-file loads JSON overlay
// and applies fields to the fetched template before PUT.
func TestMailTemplateUpdate_PatchFile(t *testing.T) {
	chdirTemp(t)
	patchJSON := `{"subject":"patched-subj","is_plain_text_mode":true}`
	if err := os.WriteFile("patch.json", []byte(patchJSON), 0o644); err != nil {
		t.Fatalf("write patch.json: %v", err)
	}

	f, stdout, _, reg := mailShortcutTestFactory(t)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/77",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id":        "77",
					"name":               "Base",
					"subject":            "orig-subj",
					"template_content":   "<p>body</p>",
					"is_plain_text_mode": false,
				},
			},
		},
	})
	putStub := &httpmock.Stub{
		Method: "PUT",
		URL:    "/user_mailboxes/me/templates/77",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{"template_id": "77"},
			},
		},
	}
	reg.Register(putStub)

	err := runMountedMailShortcut(t, MailTemplateUpdate, []string{
		"+template-update",
		"--template-id", "77",
		"--patch-file", "patch.json",
	}, f, stdout)
	if err != nil {
		t.Fatalf("update with --patch-file failed: %v", err)
	}
	putBody := decodeCapturedBody(t, putStub)
	if putBody == nil {
		t.Fatalf("expected PUT body captured")
	}
	tplWrap := putBody["template"].(map[string]interface{})
	if tplWrap["subject"] != "patched-subj" {
		t.Errorf("subject not overlaid: %v", tplWrap["subject"])
	}
	if tplWrap["is_plain_text_mode"] != true {
		t.Errorf("is_plain_text_mode not overlaid: %v", tplWrap["is_plain_text_mode"])
	}
	// Unpatched field preserved.
	if tplWrap["name"] != "Base" {
		t.Errorf("name should be preserved, got %v", tplWrap["name"])
	}
}

// TestMailTemplateUpdate_SetTemplateContentFile verifies the body-from-file
// path on update.
func TestMailTemplateUpdate_SetTemplateContentFile(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile("new-body.html", []byte("<p>updated</p>"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}

	f, stdout, _, reg := mailShortcutTestFactory(t)
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/user_mailboxes/me/templates/99",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{
					"template_id": "99",
					"name":        "Orig",
				},
			},
		},
	})
	putStub := &httpmock.Stub{
		Method: "PUT",
		URL:    "/user_mailboxes/me/templates/99",
		Body: map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"template": map[string]interface{}{"template_id": "99"},
			},
		},
	}
	reg.Register(putStub)

	err := runMountedMailShortcut(t, MailTemplateUpdate, []string{
		"+template-update",
		"--template-id", "99",
		"--set-template-content-file", "new-body.html",
	}, f, stdout)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	putBody := decodeCapturedBody(t, putStub)
	if putBody == nil {
		t.Fatalf("expected PUT body")
	}
	tplWrap := putBody["template"].(map[string]interface{})
	if tc, _ := tplWrap["template_content"].(string); !strings.Contains(tc, "updated") {
		t.Errorf("template_content missing updated body: %q", tc)
	}
}

// TestMailTemplateUpdate_ValidateErrors verifies Validate-layer errors fire
// before any network call.
func TestMailTemplateUpdate_ValidateErrors(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		expect string
	}{
		{
			"template-id required",
			[]string{"+template-update"},
			"--template-id is required",
		},
		{
			"template-id must be decimal",
			[]string{"+template-update", "--template-id", "abc"},
			"--template-id must be a decimal integer string",
		},
		{
			"content mutual exclusion",
			[]string{
				"+template-update",
				"--template-id", "1",
				"--set-template-content", "a",
				"--set-template-content-file", "b",
			},
			"mutually exclusive",
		},
		{
			"set-name too long",
			[]string{
				"+template-update",
				"--template-id", "1",
				"--set-name", strings.Repeat("x", 101),
			},
			"at most 100 characters",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, stdout, _, _ := mailShortcutTestFactory(t)
			err := runMountedMailShortcut(t, MailTemplateUpdate, c.args, f, stdout)
			if err == nil || !strings.Contains(err.Error(), c.expect) {
				t.Fatalf("expected %q, got %v", c.expect, err)
			}
		})
	}
}
