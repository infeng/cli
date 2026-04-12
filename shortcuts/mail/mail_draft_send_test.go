// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/larksuite/cli/internal/httpmock"
)

func TestMailDraftSend_ImmediateSend(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	sendStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/mail/v1/user_mailboxes/me/drafts/DR_001/send",
		Body:   `{"code":0,"data":{"message_id":"MSG_001","thread_id":"THR_001"}}`,
	}
	reg.Register(sendStub)

	err := runMountedMailShortcut(t, MailDraftSend, []string{
		"+draft-send", "--mailbox", "me", "--draft-id", "DR_001",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify no send_time in request body
	if sendStub.CapturedBody != nil && len(sendStub.CapturedBody) > 0 {
		var body map[string]interface{}
		if err := json.Unmarshal(sendStub.CapturedBody, &body); err == nil {
			if _, ok := body["send_time"]; ok {
				t.Error("unexpected send_time in request body for immediate send")
			}
		}
	}

	data := decodeShortcutEnvelopeData(t, stdout)
	if data["message_id"] != "MSG_001" {
		t.Errorf("expected message_id=MSG_001, got %v", data["message_id"])
	}
}

func TestMailDraftSend_ScheduledSend(t *testing.T) {
	f, stdout, _, reg := mailShortcutTestFactory(t)

	futureTS := time.Now().Unix() + 10*60
	futureTSStr := strconv.FormatInt(futureTS, 10)

	sendStub := &httpmock.Stub{
		Method: "POST",
		URL:    "/open-apis/mail/v1/user_mailboxes/me/drafts/DR_002/send",
		Body:   `{"code":0,"data":{"message_id":"MSG_002","thread_id":"THR_002"}}`,
	}
	reg.Register(sendStub)

	err := runMountedMailShortcut(t, MailDraftSend, []string{
		"+draft-send", "--mailbox", "me", "--draft-id", "DR_002", "--send-time", futureTSStr,
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify send_time was included in request body
	if sendStub.CapturedBody == nil {
		t.Fatal("expected request body to be captured")
	}
	var capturedBody map[string]interface{}
	if err := json.Unmarshal(sendStub.CapturedBody, &capturedBody); err != nil {
		t.Fatalf("failed to decode captured body: %v", err)
	}
	if capturedBody["send_time"] != futureTSStr {
		t.Errorf("expected send_time=%s in request body, got %v", futureTSStr, capturedBody["send_time"])
	}

	data := decodeShortcutEnvelopeData(t, stdout)
	if data["scheduled"] != true {
		t.Errorf("expected scheduled=true in output, got %v", data["scheduled"])
	}
}

func TestMailDraftSend_MissingDraftID(t *testing.T) {
	f, stdout, _, _ := mailShortcutTestFactory(t)

	err := runMountedMailShortcut(t, MailDraftSend, []string{
		"+draft-send", "--mailbox", "me",
	}, f, stdout)
	if err == nil {
		t.Fatal("expected error for missing --draft-id")
	}
	if !strings.Contains(err.Error(), "draft-id") {
		t.Fatalf("expected error about draft-id, got: %v", err)
	}
}
