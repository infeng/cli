// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/shortcuts/common"
	draftpkg "github.com/larksuite/cli/shortcuts/mail/draft"
	"github.com/larksuite/cli/shortcuts/mail/filecheck"
)

// stdBase64Enc is a local alias used by the template large-attachment
// header encoder. Keeping it here avoids repeated base64 package lookups
// in hot paths and mirrors the draft package's header handling.
var stdBase64Enc = base64.StdEncoding

// Template attachment_type values, matching v1_data_type.Attachment.attachment_type
// (an IDL i32 enum):
//   - 1 (attachmentTypeSmall): embedded in the EML at send time (base64,
//     counted against the 25 MB limit).
//   - 2 (attachmentTypeLarge): uploaded separately; download URL rendered by
//     the server.
//
// Constants are declared in helpers.go and reused here.

// logTemplateInfo emits a structured "info" line to stderr for template
// shortcuts, matching the existing "tip: ... " / "warning: ... " style used
// elsewhere in this package. Callers pass key=value pairs; sensitive fields
// (template_content / subject / recipient plaintext / file_key plaintext)
// must NOT be passed — only counts, flags, and opaque ids.
func logTemplateInfo(runtime *common.RuntimeContext, phase string, fields map[string]interface{}) {
	if runtime == nil {
		return
	}
	out := runtime.IO().ErrOut
	if out == nil {
		return
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	// Stable key order so log lines are diff-friendly.
	sortStrings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}
	fmt.Fprintf(out, "info: template %s: %s\n", phase, strings.Join(parts, " "))
}

func sortStrings(s []string) {
	// tiny insertion sort to avoid importing sort in hot template path.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// countAddresses returns the recipient count implied by a comma-separated
// address list. Used for key log fields (tos_count/ccs_count/bccs_count).
func countAddresses(raw string) int {
	return len(ParseMailboxList(raw))
}

// countAttachmentsByType returns (inline, large) counts from a template
// attachment slice. Small non-inline entries are derivable as
// len(atts)-inline-large.
func countAttachmentsByType(atts []templateAttachment) (inlineCount, largeCount int) {
	for _, a := range atts {
		if a.IsInline {
			inlineCount++
		}
		if a.AttachmentType == attachmentTypeLarge {
			largeCount++
		}
	}
	return
}

// templateEMLBaseOverhead is the estimated byte cost of template headers and
// address/subject/content envelope when projecting the EML size for LARGE
// attachment switching. Matches desktop's TemplateData base overhead.
const templateEMLBaseOverhead = 2048

// templateLargeSwitchThreshold is the projected EML size (base64) above which
// subsequent template attachments are marked LARGE. Matches the EML 25 MB
// limit used elsewhere and desktop's SMALL_ATTACHMENT_MAX_SIZE.
const templateLargeSwitchThreshold int64 = 25 * 1024 * 1024

// templateAttachment is the OAPI Attachment payload used in the templates
// create/update request body. Fields align with
// mail.open.access.v1_data_type.Attachment (id/filename/cid/is_inline/
// attachment_type/body).
//
// `body` is a required field on the server (omitting it yields errno 99992402
// `template.attachments[*].body is required`). For files the CLI has already
// uploaded to Drive we reuse the Drive file_key as the body value — the
// backend handler treats both `id` and `body` as the same file_key reference,
// so sending the key twice satisfies the required-field check without forcing
// CLI to stream the raw bytes for every inline image / attachment.
type templateAttachment struct {
	ID             string `json:"id,omitempty"` // Drive file_key
	Filename       string `json:"filename,omitempty"`
	CID            string `json:"cid,omitempty"` // only for is_inline=true
	IsInline       bool   `json:"is_inline"`
	AttachmentType int    `json:"attachment_type,omitempty"` // i32 enum: 1=SMALL, 2=LARGE
	Body           string `json:"body"`                      // required: Drive file_key (same as ID) for uploaded content
}

// templatePayload is the Template struct sent to templates.create / update.
// Field names match the spec's snake_case and the note that to/cc/bcc use
// the plural "tos/ccs/bccs" forms.
type templatePayload struct {
	TemplateID      string               `json:"template_id,omitempty"`
	Name            string               `json:"name"`
	Subject         string               `json:"subject,omitempty"`
	TemplateContent string               `json:"template_content,omitempty"`
	IsPlainTextMode bool                 `json:"is_plain_text_mode"`
	Tos             []templateMailAddr   `json:"tos,omitempty"`
	Ccs             []templateMailAddr   `json:"ccs,omitempty"`
	Bccs            []templateMailAddr   `json:"bccs,omitempty"`
	Attachments     []templateAttachment `json:"attachments,omitempty"`
	CreateTime      string               `json:"create_time,omitempty"`
}

// templateMailAddr matches v1_data_type.MailAddress; on the wire only
// mail_address and (optional) name are emitted. No alias fallback is performed.
type templateMailAddr struct {
	Address string `json:"mail_address"`
	Name    string `json:"name,omitempty"`
}

// parsedLocalImage represents one local file reference discovered in the
// template HTML content. Order is preserved in the order of appearance.
type parsedLocalImage struct {
	RawSrc string // original src attribute value
	Path   string // same as RawSrc; kept for clarity
}

// templateImgSrcRegexp mirrors draftpkg.imgSrcRegexp (unexported). Duplicated
// here because ResolveLocalImagePaths is a sibling helper and this regex is
// private to that package.
var templateImgSrcRegexp = regexp.MustCompile(`(?i)<img\s(?:[^>]*?\s)?src\s*=\s*["']([^"']+)["']`)
var templateURISchemeRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*:`)

// parseLocalImgs extracts local-file <img src="..."> references from HTML, in
// document order. Duplicates are preserved to keep the iteration order
// stable; callers that want dedup by path must do so themselves.
func parseLocalImgs(html string) []parsedLocalImage {
	matches := templateImgSrcRegexp.FindAllStringSubmatch(html, -1)
	var out []parsedLocalImage
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		src := strings.TrimSpace(m[1])
		if src == "" {
			continue
		}
		if strings.HasPrefix(src, "//") {
			continue
		}
		if templateURISchemeRegexp.MatchString(src) {
			continue
		}
		out = append(out, parsedLocalImage{RawSrc: src, Path: src})
	}
	return out
}

// templateMailboxPath builds /open-apis/mail/v1/user_mailboxes/:id/templates[/...].
func templateMailboxPath(mailboxID string, segments ...string) string {
	parts := []string{url.PathEscape(mailboxID), "templates"}
	for _, s := range segments {
		if s == "" {
			continue
		}
		parts = append(parts, url.PathEscape(s))
	}
	return "/open-apis/mail/v1/user_mailboxes/" + strings.Join(parts, "/")
}

// validateTemplateID enforces "decimal integer string" per the spec.
func validateTemplateID(tid string) error {
	if tid == "" {
		return nil
	}
	if _, err := strconv.ParseInt(tid, 10, 64); err != nil {
		return output.ErrValidation("--template-id must be a decimal integer string")
	}
	return nil
}

// renderTemplateAddresses converts a comma-separated address list to
// []templateMailAddr. Empty input returns nil so the field is omitted.
func renderTemplateAddresses(raw string) []templateMailAddr {
	boxes := ParseMailboxList(raw)
	if len(boxes) == 0 {
		return nil
	}
	out := make([]templateMailAddr, 0, len(boxes))
	for _, m := range boxes {
		out = append(out, templateMailAddr{Address: m.Email, Name: m.Name})
	}
	return out
}

// joinTemplateAddresses flattens a []templateMailAddr back to the
// comma-separated "Name <email>" form used by compose helpers.
func joinTemplateAddresses(addrs []templateMailAddr) string {
	if len(addrs) == 0 {
		return ""
	}
	var parts []string
	for _, a := range addrs {
		if a.Address == "" {
			continue
		}
		m := Mailbox{Name: a.Name, Email: a.Address}
		parts = append(parts, m.String())
	}
	return strings.Join(parts, ", ")
}

// generateTemplateCID returns a UUID v4 for inline image Content-IDs.
// Matches draftpkg.generateCID behavior; duplicated only because that
// helper is unexported.
func generateTemplateCID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate CID: %w", err)
	}
	return id.String(), nil
}

// uploadToDriveForTemplate uploads a local file to Drive and returns its
// file_key. Files ≤20MB use medias/upload_all; larger files use the
// upload_prepare+upload_part+upload_finish multipart path. parent_type is
// "email" to match the existing large attachment path.
func uploadToDriveForTemplate(ctx context.Context, runtime *common.RuntimeContext, path string) (fileKey string, size int64, err error) {
	info, err := runtime.FileIO().Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat %s: %w", path, err)
	}
	size = info.Size()
	if size > MaxLargeAttachmentSize {
		return "", size, fmt.Errorf("attachment %s (%.1f GB) exceeds the %.0f GB single file limit",
			filepath.Base(path), float64(size)/1024/1024/1024, float64(MaxLargeAttachmentSize)/1024/1024/1024)
	}
	name := filepath.Base(path)
	if err := filecheck.CheckBlockedExtension(name); err != nil {
		return "", size, err
	}
	userOpenId := runtime.UserOpenId()
	if userOpenId == "" {
		return "", size, fmt.Errorf("template attachment upload requires user identity (--as user)")
	}
	if size <= common.MaxDriveMediaUploadSinglePartSize {
		fileKey, err = common.UploadDriveMediaAll(runtime, common.DriveMediaUploadAllConfig{
			FilePath:   path,
			FileName:   name,
			FileSize:   size,
			ParentType: "email",
			ParentNode: &userOpenId,
		})
	} else {
		fileKey, err = common.UploadDriveMediaMultipart(runtime, common.DriveMediaMultipartUploadConfig{
			FilePath:   path,
			FileName:   name,
			FileSize:   size,
			ParentType: "email",
			ParentNode: userOpenId,
		})
	}
	if err != nil {
		return "", size, fmt.Errorf("upload %s to Drive failed: %w", name, err)
	}
	return fileKey, size, nil
}

// templateAttachmentBuilder accumulates attachments while classifying each
// entry SMALL / LARGE according to the projected EML size. Used by both
// +template-create and +template-update so the LARGE-switch decision is
// applied consistently across inline and non-inline entries.
type templateAttachmentBuilder struct {
	projectedSize int64
	largeBucket   bool
	attachments   []templateAttachment
}

func newTemplateAttachmentBuilder(name, subject, content string, tos, ccs, bccs []templateMailAddr) *templateAttachmentBuilder {
	size := int64(templateEMLBaseOverhead)
	// 4/3 base64 overhead for the raw fields.
	bytesEncoded := int64(len(name)+len(subject)+len(content))*4/3 + int64(200)
	size += bytesEncoded
	for _, a := range tos {
		size += int64(len(a.Address) + len(a.Name) + 16)
	}
	for _, a := range ccs {
		size += int64(len(a.Address) + len(a.Name) + 16)
	}
	for _, a := range bccs {
		size += int64(len(a.Address) + len(a.Name) + 16)
	}
	return &templateAttachmentBuilder{projectedSize: size}
}

// append adds one attachment, picking SMALL or LARGE based on the projected
// EML size running total. Once largeBucket flips to true, every subsequent
// attachment is LARGE regardless of size.
func (b *templateAttachmentBuilder) append(fileKey, filename, cid string, isInline bool, fileSize int64) {
	base64Size := estimateBase64EMLSize(fileSize)
	aType := attachmentTypeSmall
	if b.largeBucket || b.projectedSize+base64Size >= templateLargeSwitchThreshold {
		aType = attachmentTypeLarge
		b.largeBucket = true
	} else {
		b.projectedSize += base64Size
	}
	b.attachments = append(b.attachments, templateAttachment{
		ID:             fileKey,
		Filename:       filename,
		CID:            cid,
		IsInline:       isInline,
		AttachmentType: aType,
		// The server marks `body` as required (errno 99992402). Since the
		// file was already uploaded to Drive and the handler resolves
		// Attachment.id as the file_key, mirror the same key into body so
		// the required-field check passes without the CLI re-reading the
		// file bytes.
		Body: fileKey,
	})
}

// wrapTemplateContentIfNeeded mirrors the draft compose flow's plain-text →
// HTML upgrade (shortcuts/mail/mail_quote.go:buildBodyDiv): when the
// template is not marked as pure plain-text mode AND the content is not
// already HTML, HTML-escape the content and convert newlines to <br> so
// the PC client renders line breaks in template preview. Without this, a
// three-line plain body saved verbatim renders as a single run-on line
// because HTML collapses whitespace. The mail compose flow added this
// transform at mail_quote.go:258 so sent emails carry <br>; templates
// need the same treatment so preview matches what sending a draft
// composed from the template would produce.
func wrapTemplateContentIfNeeded(content string, isPlainText bool) string {
	if content == "" {
		return content
	}
	if isPlainText {
		return content
	}
	if bodyIsHTML(content) {
		return content
	}
	return buildBodyDiv(content, false)
}

// buildTemplatePayloadFromFlags processes HTML inline images and non-inline
// attachment flags in the exact order required by the spec: inline images in
// HTML <img> order, non-inline attachments in --attach / --attachment
// flag order. Returns the rewritten template content (cid: refs) plus the
// attachment list.
func buildTemplatePayloadFromFlags(
	ctx context.Context,
	runtime *common.RuntimeContext,
	name, subject, content string,
	tos, ccs, bccs []templateMailAddr,
	attachPaths []string,
) (rewrittenContent string, atts []templateAttachment, err error) {
	builder := newTemplateAttachmentBuilder(name, subject, content, tos, ccs, bccs)

	// 1. Inline images (iterate in the HTML order so cid mapping is stable
	// across CLI versions; duplicates reuse the same file_key/cid).
	imgs := parseLocalImgs(content)
	pathToCID := make(map[string]string)
	pathToFileKey := make(map[string]string)
	pathToSize := make(map[string]int64)
	for _, img := range imgs {
		if cid, ok := pathToCID[img.Path]; ok {
			// Re-write the next occurrence to the same cid.
			content = replaceImgSrcOnce(content, img.RawSrc, "cid:"+cid)
			continue
		}
		fileKey, sz, upErr := uploadToDriveForTemplate(ctx, runtime, img.Path)
		if upErr != nil {
			return "", nil, upErr
		}
		cid, cidErr := generateTemplateCID()
		if cidErr != nil {
			return "", nil, cidErr
		}
		pathToCID[img.Path] = cid
		pathToFileKey[img.Path] = fileKey
		pathToSize[img.Path] = sz
		content = replaceImgSrcOnce(content, img.RawSrc, "cid:"+cid)
		builder.append(fileKey, filepath.Base(img.Path), cid, true, sz)
	}

	// 2. Non-inline --attach paths in the exact order passed.
	for _, p := range attachPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		fileKey, sz, upErr := uploadToDriveForTemplate(ctx, runtime, p)
		if upErr != nil {
			return "", nil, upErr
		}
		builder.append(fileKey, filepath.Base(p), "", false, sz)
	}

	return content, builder.attachments, nil
}

// replaceImgSrcOnce rewrites the first <img src="rawSrc"> occurrence to
// <img src="newSrc">, preserving the quoting style of the original.
func replaceImgSrcOnce(html, rawSrc, newSrc string) string {
	// Find the next <img ...> match whose captured src equals rawSrc.
	indices := templateImgSrcRegexp.FindAllStringSubmatchIndex(html, -1)
	for _, idx := range indices {
		if len(idx) < 4 {
			continue
		}
		if strings.TrimSpace(html[idx[2]:idx[3]]) == rawSrc {
			return html[:idx[2]] + newSrc + html[idx[3]:]
		}
	}
	return html
}

// ── Template fetch / CRUD ────────────────────────────────────────────

// fetchTemplate GETs a single template (full fields) for --template-id
// composition and update patch workflows.
func fetchTemplate(runtime *common.RuntimeContext, mailboxID, templateID string) (*templatePayload, error) {
	data, err := runtime.CallAPI("GET", templateMailboxPath(mailboxID, templateID), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch template %s failed: %w", templateID, err)
	}
	return extractTemplatePayload(data)
}

// extractTemplatePayload decodes the API response, looking inside the common
// "template" wrapper when present.
func extractTemplatePayload(data map[string]interface{}) (*templatePayload, error) {
	raw := data
	if t, ok := data["template"].(map[string]interface{}); ok {
		raw = t
	}
	if raw == nil {
		return nil, fmt.Errorf("API response missing template body")
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-encode template payload failed: %w", err)
	}
	var out templatePayload
	if err := json.Unmarshal(buf, &out); err != nil {
		return nil, fmt.Errorf("decode template payload failed: %w", err)
	}
	return &out, nil
}

// createTemplate POSTs a new template.
func createTemplate(runtime *common.RuntimeContext, mailboxID string, tpl *templatePayload) (map[string]interface{}, error) {
	return runtime.CallAPI("POST", templateMailboxPath(mailboxID), nil, map[string]interface{}{
		"template": tpl,
	})
}

// updateTemplate PUTs a full-replace update.
func updateTemplate(runtime *common.RuntimeContext, mailboxID, templateID string, tpl *templatePayload) (map[string]interface{}, error) {
	return runtime.CallAPI("PUT", templateMailboxPath(mailboxID, templateID), nil, map[string]interface{}{
		"template": tpl,
	})
}

// ── --template-id merge logic (§5.5) ─────────────────────────────────

// templateApplyResult holds the merged compose state produced by
// applyTemplate. Callers consume individual fields and feed them into the
// existing +send / +reply / +forward pipelines.
type templateApplyResult struct {
	To              string
	Cc              string
	Bcc             string
	Subject         string
	Body            string
	IsPlainTextMode bool
	Warnings        []string
	// Attachments carry Drive file_key identifiers; CLI passes them through
	// to the send/draft path via the X-Lms-Large-Attachment-Ids header for
	// LARGE items. SMALL items are downloaded server-side when the draft
	// materializes; we rely on server-side "reuse by file_key" semantics.
	LargeAttachmentIDs []string
}

// templateShortcutKind enumerates the 5 shortcuts that accept --template-id.
type templateShortcutKind string

const (
	templateShortcutSend        templateShortcutKind = "send"
	templateShortcutDraftCreate templateShortcutKind = "draft-create"
	templateShortcutReply       templateShortcutKind = "reply"
	templateShortcutReplyAll    templateShortcutKind = "reply-all"
	templateShortcutForward     templateShortcutKind = "forward"
)

// applyTemplate merges a fetched template with draft-derived and user-flag
// values. draftTo/Cc/Bcc are the addresses already on the draft (from the
// original message for reply/reply-all/forward, or the user flags for send/
// draft-create). userTo/Cc/Bcc/Subject/Body are user-supplied flag values
// (empty string = not provided).
func applyTemplate(
	kind templateShortcutKind,
	tpl *templatePayload,
	draftTo, draftCc, draftBcc string,
	draftSubject string,
	draftBody string,
	userTo, userCc, userBcc, userSubject, userBody string,
) templateApplyResult {
	res := templateApplyResult{}

	// Start with whatever is already in the draft (or the user-explicit
	// draft-to values for send/draft-create).
	effTo := draftTo
	effCc := draftCc
	effBcc := draftBcc
	// User-flag --to/--cc/--bcc values override draft-derived values
	// before template injection.
	if userTo != "" {
		effTo = userTo
	}
	if userCc != "" {
		effCc = userCc
	}
	if userBcc != "" {
		effBcc = userBcc
	}

	tplTo := joinTemplateAddresses(tpl.Tos)
	tplCc := joinTemplateAddresses(tpl.Ccs)
	tplBcc := joinTemplateAddresses(tpl.Bccs)

	// Append template to/cc/bcc into draft to/cc/bcc.
	effTo = appendAddrList(effTo, tplTo)
	effCc = appendAddrList(effCc, tplCc)
	effBcc = appendAddrList(effBcc, tplBcc)

	res.To = effTo
	res.Cc = effCc
	res.Bcc = effBcc

	// Q2: subject merging. User --subject wins, else draft non-empty wins,
	// else template subject.
	switch {
	case strings.TrimSpace(userSubject) != "":
		res.Subject = userSubject
	case strings.TrimSpace(draftSubject) != "":
		res.Subject = draftSubject
	default:
		res.Subject = tpl.Subject
	}

	// Q3: body merging. The shortcut-specific HTML/plain-text injection is
	// handled by the caller; applyTemplate returns a merged body string that
	// the caller can feed back into its compose pipeline.
	res.Body = mergeTemplateBody(kind, tpl, draftBody, userBody)

	// IsPlainTextMode propagation: template value wins.
	res.IsPlainTextMode = tpl.IsPlainTextMode

	// Q4: warn when reply / reply-all + template has to/cc/bcc (likely
	// duplicates against the reply-derived recipients).
	if (kind == templateShortcutReply || kind == templateShortcutReplyAll) &&
		(len(tpl.Tos) > 0 || len(tpl.Ccs) > 0 || len(tpl.Bccs) > 0) {
		res.Warnings = append(res.Warnings,
			"template to/cc/bcc are appended without de-duplication; "+
				"you may see repeated recipients. Use --to/--cc/--bcc to override, "+
				"or run +template-update to clear template addresses.")
	}

	// Collect template attachment ids for the caller (file_keys). The SEND
	// path uses these as the X-Lms-Large-Attachment-Ids header entries for
	// LARGE types; SMALL entries are reused by file_key server-side.
	for _, att := range tpl.Attachments {
		if att.ID == "" {
			continue
		}
		res.LargeAttachmentIDs = append(res.LargeAttachmentIDs, att.ID)
	}

	return res
}

func appendAddrList(base, extra string) string {
	if strings.TrimSpace(extra) == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return extra
	}
	// §5.5 Q1 is explicit: concat without dedup.
	return base + ", " + extra
}

// mergeTemplateBody handles §5.5 Q3 body merging.
//
//   - send / draft-create: empty draft body → use template body; non-empty →
//     append template body after a separator.
//   - reply / reply-all / forward: insert template body before the
//     <blockquote> wrapper (regex), fallback to end-append; plain-text drafts
//     prepend template body + newline before the quoted block.
func mergeTemplateBody(kind templateShortcutKind, tpl *templatePayload, draftBody, userBody string) string {
	tplContent := tpl.TemplateContent
	// If the user explicitly passed --body, that is the composer's own
	// authoring area; we still inject the template content into the same
	// area (draft_body = user_body for send/draft-create).
	if userBody != "" {
		draftBody = userBody
	}

	// Plain-text mode: simple append.
	if tpl.IsPlainTextMode {
		switch kind {
		case templateShortcutSend, templateShortcutDraftCreate:
			if strings.TrimSpace(draftBody) == "" {
				return tplContent
			}
			return draftBody + "\n\n" + tplContent
		default:
			// reply/forward plain-text: prepend template before quote.
			// emlbuilder composes quote separately so the draft body here
			// is pure user-authored content; we just prepend.
			if strings.TrimSpace(draftBody) == "" {
				return tplContent
			}
			return tplContent + "\n\n" + draftBody
		}
	}

	switch kind {
	case templateShortcutSend, templateShortcutDraftCreate:
		if strings.TrimSpace(draftBody) == "" {
			return tplContent
		}
		return draftBody + tplContent
	case templateShortcutReply, templateShortcutReplyAll, templateShortcutForward:
		// At this compose layer, draftBody is the user-authored area only
		// (the caller adds the quote block downstream). Inject template
		// content at the head of that area so it lands above the future
		// quote block.
		if strings.TrimSpace(draftBody) == "" {
			return tplContent
		}
		// Regex replace: if the draft body already contains a quote block
		// (some callers pre-compose it), insert template before it.
		if draftpkg.HTMLContainsLargeAttachment(draftBody) {
			// fall through — no quote heuristic; appending is safe.
		}
		merged := draftpkg.InsertBeforeQuoteOrAppend(draftBody, tplContent)
		if merged != draftBody {
			return merged
		}
		return tplContent + draftBody
	}
	return draftBody
}

// encodeTemplateLargeAttachmentHeader returns the base64-JSON-encoded value
// to add to an X-Lms-Large-Attachment-Ids header when the template supplies
// one or more non-inline file_keys. Returns empty string when the input is
// empty (caller should skip adding the header).
//
// Duplicate IDs are collapsed into a single entry.
func encodeTemplateLargeAttachmentHeader(tplIDs []string) (string, error) {
	if len(tplIDs) == 0 {
		return "", nil
	}
	seen := make(map[string]bool, len(tplIDs))
	var deduped []largeAttID
	for _, id := range tplIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		deduped = append(deduped, largeAttID{ID: id})
	}
	if len(deduped) == 0 {
		return "", nil
	}
	buf, err := json.Marshal(deduped)
	if err != nil {
		return "", err
	}
	return b64StdEncode(buf), nil
}

// b64StdEncode avoids importing encoding/base64 twice.
func b64StdEncode(buf []byte) string { return stdBase64Enc.EncodeToString(buf) }
