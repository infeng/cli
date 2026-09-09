// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/httpmock"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
)

func stubThreadManagePost(reg *httpmock.Registry, endpoint string, responseData map[string]interface{}) *httpmock.Stub {
	stub := &httpmock.Stub{
		Method: "POST",
		URL:    "/user_mailboxes/me/threads/" + endpoint,
		Body:   map[string]interface{}{"code": 0, "data": responseData},
	}
	reg.Register(stub)
	return stub
}

func requireThreadValidationParam(t *testing.T, err error, param string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error for %s", param)
	}
	var validationErr *errs.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *errs.ValidationError for %s, got %T: %v", param, err, err)
	}
	if validationErr.Param != param {
		t.Fatalf("param = %q, want %q", validationErr.Param, param)
	}
}

func decodeCapturedThreadBody(t *testing.T, stub *httpmock.Stub) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(stub.CapturedBody, &body); err != nil {
		t.Fatalf("unmarshal captured body: %v", err)
	}
	return body
}

func decodeThreadDryRunCall(t *testing.T, stdout string) map[string]interface{} {
	t.Helper()
	var envelope struct {
		OK   bool                   `json:"ok"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("unmarshal dry-run output: %v; stdout=%s", err, stdout)
	}
	if !envelope.OK {
		t.Fatalf("dry-run envelope is not ok: %s", stdout)
	}
	calls, ok := envelope.Data["api"].([]interface{})
	if !ok || len(calls) != 1 {
		t.Fatalf("api calls = %#v, want exactly one", envelope.Data["api"])
	}
	call, ok := calls[0].(map[string]interface{})
	if !ok {
		t.Fatalf("api call = %#v, want object", calls[0])
	}
	return call
}

func TestThreadModifyMetadata(t *testing.T) {
	if MailThreadModify.Command != "+thread-modify" || MailThreadModify.Risk != "write" {
		t.Fatalf("command/risk = %q/%q", MailThreadModify.Command, MailThreadModify.Risk)
	}
	if !reflect.DeepEqual(MailThreadModify.AuthTypes, []string{"user", "bot"}) {
		t.Fatalf("AuthTypes = %v, want [user bot]", MailThreadModify.AuthTypes)
	}
	if !reflect.DeepEqual(MailThreadModify.Scopes, []string{"mail:user_mailbox.message:modify"}) {
		t.Fatalf("Scopes = %v", MailThreadModify.Scopes)
	}
	flags := map[string]common.Flag{}
	for _, flag := range MailThreadModify.Flags {
		flags[flag.Name] = flag
	}
	for _, name := range []string{"mailbox", "thread-id", "add-label-id", "remove-label-id", "folder-id"} {
		if _, ok := flags[name]; !ok {
			t.Fatalf("missing --%s flag", name)
		}
	}
	for _, forbidden := range []string{"thread-ids", "add-label-ids", "remove-label-ids", "add-folder", "data"} {
		if _, ok := flags[forbidden]; ok {
			t.Fatalf("unexpected --%s bypass", forbidden)
		}
	}
	if flags["thread-id"].Type != "string_slice" || !flags["thread-id"].Required {
		t.Fatalf("--thread-id = %#v, want required string_slice", flags["thread-id"])
	}
	if flags["mailbox"].Default != "me" {
		t.Fatalf("--mailbox default = %q, want me", flags["mailbox"].Default)
	}
}

func TestThreadTrashMetadataAndRegistration(t *testing.T) {
	if MailThreadTrash.Command != "+thread-trash" || MailThreadTrash.Risk != "high-risk-write" {
		t.Fatalf("command/risk = %q/%q", MailThreadTrash.Command, MailThreadTrash.Risk)
	}
	if !reflect.DeepEqual(MailThreadTrash.AuthTypes, []string{"user", "bot"}) {
		t.Fatalf("AuthTypes = %v, want [user bot]", MailThreadTrash.AuthTypes)
	}
	if len(MailThreadTrash.Flags) != 2 || MailThreadTrash.Flags[1].Name != "thread-id" {
		t.Fatalf("Flags = %#v, want mailbox and thread-id", MailThreadTrash.Flags)
	}
	commands := map[string]bool{}
	for _, shortcut := range Shortcuts() {
		commands[shortcut.Command] = true
	}
	for _, command := range []string{"+thread-modify", "+thread-trash"} {
		if !commands[command] {
			t.Fatalf("Shortcuts() missing %s", command)
		}
	}
}

func TestNormalizeThreadValuesTrimsDeduplicatesAndRejectsEmpty(t *testing.T) {
	got, err := normalizeThreadValues([]string{" thread_a,thread_b ", "thread_a", " thread_c "}, "--thread-id", true)
	if err != nil {
		t.Fatalf("normalizeThreadValues returned error: %v", err)
	}
	want := []string{"thread_a", "thread_b", "thread_c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
	for _, raw := range [][]string{nil, {""}, {" "}, {"thread_a,,thread_b"}} {
		_, err := normalizeThreadValues(raw, "--thread-id", true)
		requireThreadValidationParam(t, err, "--thread-id")
	}
	got, err = normalizeThreadValues(nil, "--add-label-id", false)
	if err != nil || len(got) != 0 {
		t.Fatalf("optional empty values = %v, err=%v", got, err)
	}
}

func TestThreadModifyValidation(t *testing.T) {
	f, stdout, _, _ := mailShortcutTestFactory(t)
	tests := []struct {
		name  string
		args  []string
		param string
	}{
		{name: "no action", args: []string{"+thread-modify", "--thread-id", "thread_a"}, param: "--thread-modify"},
		{name: "empty thread", args: []string{"+thread-modify", "--thread-id", " ", "--folder-id", "folder_a"}, param: "--thread-id"},
		{name: "empty label", args: []string{"+thread-modify", "--thread-id", "thread_a", "--add-label-id", " "}, param: "--add-label-id"},
		{name: "empty folder", args: []string{"+thread-modify", "--thread-id", "thread_a", "--folder-id", " "}, param: "--folder-id"},
		{name: "label intersection", args: []string{"+thread-modify", "--thread-id", "thread_a", "--add-label-id", " label_a ", "--remove-label-id", "label_a"}, param: "--add-label-id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := runMountedMailShortcut(t, MailThreadModify, test.args, f, stdout)
			requireThreadValidationParam(t, err, test.param)
		})
	}
}

func TestThreadModifySingleRequestSafeMappingAndResponsePassthrough(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := stubThreadManagePost(reg, "batch_modify", map[string]interface{}{"opaque_result": "from-server"})
	err := runMountedMailShortcut(t, MailThreadModify, []string{
		"+thread-modify",
		"--thread-id", " thread_a,thread_b ",
		"--thread-id", "thread_a",
		"--add-label-id", " label_a,label_b ",
		"--remove-label-id", "label_c",
		"--folder-id", " folder_a ",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if len(post.CapturedBodies) != 1 {
		t.Fatalf("CallAPI count = %d, want 1", len(post.CapturedBodies))
	}
	body := decodeCapturedThreadBody(t, post)
	wantBody := map[string]interface{}{
		"thread_ids":       []interface{}{"thread_a", "thread_b"},
		"add_label_ids":    []interface{}{"label_a", "label_b"},
		"remove_label_ids": []interface{}{"label_c"},
		"add_folder":       "folder_a",
	}
	if !reflect.DeepEqual(body, wantBody) {
		t.Fatalf("body = %#v, want %#v", body, wantBody)
	}
	if len(body) != 4 {
		t.Fatalf("body contains non-whitelisted fields: %#v", body)
	}
	data := decodeShortcutEnvelopeData(t, stdout)
	if !reflect.DeepEqual(data, map[string]interface{}{"opaque_result": "from-server"}) {
		t.Fatalf("response data = %#v, want server data unchanged", data)
	}
	for _, synthesized := range []string{"success_thread_ids", "failed_thread_ids", "updated_count", "trashed_count"} {
		if _, ok := data[synthesized]; ok {
			t.Fatalf("response synthesized %q: %#v", synthesized, data)
		}
	}
}

func TestThreadModifyOmitsUnusedOptionalFields(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := stubThreadManagePost(reg, "batch_modify", map[string]interface{}{})
	err := runMountedMailShortcut(t, MailThreadModify, []string{
		"+thread-modify", "--thread-id", "thread_a", "--add-label-id", "label_a",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	body := decodeCapturedThreadBody(t, post)
	if len(body) != 2 {
		t.Fatalf("body = %#v, want only thread_ids and add_label_ids", body)
	}
	for _, omitted := range []string{"remove_label_ids", "add_folder", "folder_id", "data"} {
		if _, ok := body[omitted]; ok {
			t.Fatalf("body unexpectedly contains %q: %#v", omitted, body)
		}
	}
}

func TestThreadModifyDryRunMatchesExecuteRequest(t *testing.T) {
	args := []string{"+thread-modify", "--thread-id", " thread_a,thread_b ", "--add-label-id", " label_a ", "--folder-id", " folder_a "}
	f, stdout, _, _ := mailShortcutTestFactory(t)
	if err := runMountedMailShortcut(t, MailThreadModify, append(args, "--dry-run"), f, stdout); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}
	dryCall := decodeThreadDryRunCall(t, stdout.String())

	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := stubThreadManagePost(reg, "batch_modify", map[string]interface{}{})
	if err := runMountedMailShortcut(t, MailThreadModify, args, f, stdout); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if dryCall["method"] != "POST" || dryCall["url"] != "/open-apis/mail/v1/user_mailboxes/me/threads/batch_modify" {
		t.Fatalf("dry-run call = %#v", dryCall)
	}
	if !reflect.DeepEqual(dryCall["body"], decodeCapturedThreadBody(t, post)) {
		t.Fatalf("dry-run body = %#v, execute body = %#v", dryCall["body"], decodeCapturedThreadBody(t, post))
	}
}

func TestThreadTrashSingleRequestAndResponsePassthrough(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := stubThreadManagePost(reg, "batch_trash", map[string]interface{}{"status": "accepted"})
	err := runMountedMailShortcut(t, MailThreadTrash, []string{
		"+thread-trash", "--thread-id", " thread_a,thread_b ", "--thread-id", "thread_a", "--yes",
	}, f, stdout)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if len(post.CapturedBodies) != 1 {
		t.Fatalf("CallAPI count = %d, want 1", len(post.CapturedBodies))
	}
	body := decodeCapturedThreadBody(t, post)
	if !reflect.DeepEqual(body, map[string]interface{}{"thread_ids": []interface{}{"thread_a", "thread_b"}}) {
		t.Fatalf("body = %#v", body)
	}
	data := decodeShortcutEnvelopeData(t, stdout)
	if !reflect.DeepEqual(data, map[string]interface{}{"status": "accepted"}) {
		t.Fatalf("response data = %#v, want server data unchanged", data)
	}
}

func TestThreadTrashDryRunMatchesExecuteAndRequiresConfirmation(t *testing.T) {
	args := []string{"+thread-trash", "--thread-id", " thread_a,thread_b "}
	f, stdout, _, _ := mailShortcutTestFactory(t)
	err := runMountedMailShortcut(t, MailThreadTrash, args, f, stdout)
	if err == nil || output.ExitCodeOf(err) != output.ExitConfirmationRequired {
		t.Fatalf("without --yes error = %v, exit=%d", err, output.ExitCodeOf(err))
	}
	if err := runMountedMailShortcut(t, MailThreadTrash, append(args, "--dry-run"), f, stdout); err != nil {
		t.Fatalf("dry-run returned error: %v", err)
	}
	dryCall := decodeThreadDryRunCall(t, stdout.String())

	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := stubThreadManagePost(reg, "batch_trash", map[string]interface{}{})
	if err := runMountedMailShortcut(t, MailThreadTrash, append(args, "--yes"), f, stdout); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if dryCall["method"] != "POST" || dryCall["url"] != "/open-apis/mail/v1/user_mailboxes/me/threads/batch_trash" {
		t.Fatalf("dry-run call = %#v", dryCall)
	}
	if !reflect.DeepEqual(dryCall["body"], decodeCapturedThreadBody(t, post)) {
		t.Fatalf("dry-run body = %#v, execute body = %#v", dryCall["body"], decodeCapturedThreadBody(t, post))
	}
}

func TestThreadAPIErrorRemainsTypedAndIsNotRetried(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)
	post := &httpmock.Stub{
		Method: "POST",
		URL:    "/user_mailboxes/me/threads/batch_modify",
		Body:   map[string]interface{}{"code": 1230001, "msg": "bad request"},
	}
	reg.Register(post)
	err := runMountedMailShortcut(t, MailThreadModify, []string{
		"+thread-modify", "--thread-id", "thread_a", "--folder-id", "folder_a",
	}, f, stdout)
	if err == nil {
		t.Fatal("expected API error")
	}
	if _, ok := errs.ProblemOf(err); !ok {
		t.Fatalf("error = %T %v, want typed problem", err, err)
	}
	if len(post.CapturedBodies) != 1 {
		t.Fatalf("CallAPI count = %d, want no retry", len(post.CapturedBodies))
	}
	if strings.Contains(stdout.String(), "success_thread_ids") {
		t.Fatalf("error path synthesized success output: %s", stdout.String())
	}
}
