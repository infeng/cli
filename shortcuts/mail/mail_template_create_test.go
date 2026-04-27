// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTemplateAttachmentBuilder_InlineAlwaysSmall covers the §2 invariant:
// inline attachments are always SMALL even when largeBucket would otherwise
// have been latched. Forcing the accumulator past the 25 MB threshold via a
// pre-existing emlProjectedSize must not affect inline classification.
func TestTemplateAttachmentBuilder_InlineAlwaysSmall(t *testing.T) {
	b := newTemplateAttachmentBuilder(0)
	// Force largeBucket via direct field manipulation to simulate a prior
	// non-inline attachment having tripped the switch (in production
	// rejectLarge=true would have errored out first; this branch tests the
	// invariant of the classifier itself).
	b.rejectLarge = false
	b.largeBucket = true
	b.emlProjectedSize = 24 * 1024 * 1024 // already above pre-cap

	// Inline append: must stay SMALL.
	att, err := b.AppendInline("fk-inline", "logo.png", "cid-x", 1024)
	if err != nil {
		t.Fatalf("AppendInline error = %v", err)
	}
	if att.AttachmentType != AttachmentTypeSMALL {
		t.Fatalf("inline AttachmentType = %q, want SMALL even with largeBucket=true", att.AttachmentType)
	}
	if !att.IsInline {
		t.Fatalf("inline IsInline = false, want true")
	}
}

// TestTemplateAttachmentBuilder_SmallToLargeSwitchBoundary covers the §2
// invariant: non-inline switches to LARGE once emlProjectedSize+base64Size
// >= 25 MB (order-sensitive). The first attach that would cross the line
// trips largeBucket. With rejectLarge=true (templates mode) this surfaces
// as ErrValidation.
func TestTemplateAttachmentBuilder_SmallToLargeSwitchBoundary(t *testing.T) {
	b := newTemplateAttachmentBuilder(0)
	// Seed eml accumulator just below 25 MB so the next file tips it over.
	b.emlProjectedSize = MaxTemplateCumulativeBytes - 1024

	// 4 KB raw → ~5.5 KB base64 → emlProjectedSize crosses 25 MB.
	if _, err := b.AppendAttachment("fk-1", "big.bin", 4096); err == nil {
		t.Fatalf("expected ErrValidation when crossing 25 MB cap, got nil")
	}
	if !b.largeBucket {
		t.Fatal("largeBucket should latch true after crossing the threshold")
	}
}

// TestTemplateAttachmentBuilder_SmallStaysSmallBelowCap verifies that
// attachments well below the 25 MB cumulative cap stay SMALL.
func TestTemplateAttachmentBuilder_SmallStaysSmallBelowCap(t *testing.T) {
	b := newTemplateAttachmentBuilder(0)
	att, err := b.AppendAttachment("fk-1", "doc.pdf", 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if att.AttachmentType != AttachmentTypeSMALL {
		t.Fatalf("AttachmentType = %q, want SMALL", att.AttachmentType)
	}
	if att.IsInline {
		t.Fatal("non-inline AppendAttachment should produce IsInline=false")
	}
}

// TestValidateTemplateContentCap_3MBRejection covers spec §2 + KB §2:
// template_content > 3 MB (rune OR byte stricter) must reject with
// ErrValidation pre-flight, not wait for server errno.
func TestValidateTemplateContentCap_3MBRejection(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"under 3 MB ASCII passes", strings.Repeat("a", 1024), false},
		{"exactly 3 MB ASCII passes", strings.Repeat("a", MaxTemplateContentBytes), false},
		{"over 3 MB ASCII rejects", strings.Repeat("a", MaxTemplateContentBytes+1), true},
		// Multi-byte runes — bytes > rune count, byte side trips first.
		{"over 3 MB multibyte rejects", strings.Repeat("中", MaxTemplateContentBytes/2), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTemplateContentCap(tt.content)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v (bytes=%d runes=%d)", err, tt.wantErr,
					len(tt.content), utf8.RuneCountInString(tt.content))
			}
		})
	}
}

// TestApplyTemplateBodyWrap_PlainTextWrapping covers spec §2 risk row 4 +
// §4.2 + KB §3: --plain-text mode must wrap via buildBodyDiv (HTML escape +
// \n→<br> + <div>), not store verbatim.
func TestApplyTemplateBodyWrap_PlainTextWrapping(t *testing.T) {
	in := "line one\nline two & <bold>"
	got := applyTemplateBodyWrap(in, true)
	if !strings.Contains(got, "<div") {
		t.Errorf("plain-text wrap missing <div>: %q", got)
	}
	if !strings.Contains(got, "<br>") {
		t.Errorf("plain-text wrap missing <br> for \\n: %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("plain-text wrap missing HTML escape of &: %q", got)
	}
	if !strings.Contains(got, "&lt;bold&gt;") {
		t.Errorf("plain-text wrap missing HTML escape of <bold>: %q", got)
	}
	// HTML mode must pass content through unchanged.
	html := "<p>raw HTML & stuff</p>"
	if got := applyTemplateBodyWrap(html, false); got != html {
		t.Errorf("HTML mode wrap modified content: got %q want %q", got, html)
	}
}

// TestComposeTemplate_25MBCumulativeRejection covers §2 risk row 4 + KB §2:
// content + inline + non-inline SMALL cumulative ≥ 25 MB must reject pre-
// flight. This exercises the running emlProjectedSize accumulator inside
// the builder.
func TestComposeTemplate_25MBCumulativeRejection(t *testing.T) {
	b := newTemplateAttachmentBuilder(int64(MaxTemplateCumulativeBytes - 100))
	// 4 KB raw file would inflate past 25 MB once base64-encoded.
	if _, err := b.AppendAttachment("fk", "x.bin", 4096); err == nil {
		t.Fatal("expected ErrValidation when builder sum crosses 25 MB; got nil")
	}
}

// TestTemplateRequestBody_AttachmentTypeFieldEncoding verifies the wire
// format keys/values match what the OAPI server expects (S1 contract
// §"Header / RPC contract"). Field names: snake_case; attachment_type is a
// string enum.
func TestTemplateRequestBody_AttachmentTypeFieldEncoding(t *testing.T) {
	c := &composedTemplate{
		Name:    "n",
		Subject: "s",
		Content: "<p>hi</p>",
		Attachments: []TemplateAttachment{
			{ID: "fk1", Filename: "a.bin", IsInline: false, AttachmentType: "SMALL"},
			{ID: "fk2", Filename: "b.png", Cid: "c1", IsInline: true, AttachmentType: "SMALL"},
		},
	}
	body := templateBodyFromBuild(c)
	atts, ok := body["attachments"].([]interface{})
	if !ok {
		t.Fatalf("attachments field missing or wrong type: %T", body["attachments"])
	}
	if len(atts) != 2 {
		t.Fatalf("attachments len = %d, want 2", len(atts))
	}
	first, _ := atts[0].(map[string]interface{})
	if first["id"] != "fk1" {
		t.Errorf("first id = %v want fk1", first["id"])
	}
	if first["attachment_type"] != "SMALL" {
		t.Errorf("first attachment_type = %v want SMALL", first["attachment_type"])
	}
	if first["is_inline"] != false {
		t.Errorf("first is_inline = %v want false", first["is_inline"])
	}
}

// TestParsePatchTemplateSkeleton verifies print-patch-template emits a
// schema close enough to the GET projection that a user can edit and pipe
// it back via --patch-file.
func TestParsePatchTemplateSkeleton(t *testing.T) {
	sk := patchTemplateSkeleton()
	for _, k := range []string{"name", "subject", "content", "to", "cc", "bcc", "attachments"} {
		if _, ok := sk[k]; !ok {
			t.Errorf("patch template skeleton missing field %q", k)
		}
	}
}

// TestApplyPatchOverrides_PrecedenceFlagOverPatchOverExisting wires up the
// merge precedence (flag > patch-file > existing) per S1 contract Cobra
// flag inventory for --set-* in update mode.
func TestApplyPatchOverrides(t *testing.T) {
	cur := templateComposeInput{Name: "old", Subject: "old-s", Content: "<p>old</p>"}
	applyPatchOverrides(&cur, map[string]interface{}{
		"name":    "patched",
		"content": "<p>patched</p>",
	})
	if cur.Name != "patched" {
		t.Errorf("Name = %q, want patched", cur.Name)
	}
	if cur.Subject != "old-s" {
		t.Errorf("Subject = %q, want old-s preserved", cur.Subject)
	}
	if cur.Content != "<p>patched</p>" {
		t.Errorf("Content = %q, want patched", cur.Content)
	}
}

// TestJoinStringList covers the GET-projection list-flatten helper.
func TestJoinStringList(t *testing.T) {
	cases := map[string]struct {
		in   interface{}
		want string
	}{
		"nil":          {nil, ""},
		"string":       {"a@x,b@y", "a@x,b@y"},
		"[]string":     {[]string{"a", "b"}, "a,b"},
		"[]interface":  {[]interface{}{"a", "b", ""}, "a,b"},
		"unsupported":  {42, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := joinStringList(tc.in); got != tc.want {
				t.Fatalf("joinStringList(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseRecipientList ensures the comma-split helper drops whitespace +
// empty entries (templates allow zero recipients).
func TestParseRecipientList(t *testing.T) {
	cases := map[string]struct {
		in   string
		want []string
	}{
		"empty":        {"", nil},
		"whitespace":   {"   ", nil},
		"single":       {"a@x", []string{"a@x"}},
		"multi":        {"a@x, b@y , c@z", []string{"a@x", "b@y", "c@z"}},
		"trailing":     {"a@x,", []string{"a@x"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseRecipientList(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d] = %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// fakeFileInfo is a minimal fileio.FileInfo for dryrunStepsForFile testing —
// constructed via the local FS instead.

// TestExtractTemplateID covers the response-shape projection helper.
func TestExtractTemplateID(t *testing.T) {
	cases := map[string]struct {
		in   map[string]interface{}
		want string
	}{
		"top level":     {map[string]interface{}{"template_id": "123"}, "123"},
		"nested data":   {map[string]interface{}{"data": map[string]interface{}{"template_id": "456"}}, "456"},
		"id fallback":   {map[string]interface{}{"id": "789"}, "789"},
		"missing":       {map[string]interface{}{}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := extractTemplateID(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
