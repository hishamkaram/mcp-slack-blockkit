package validator

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// hasErrorWithCode reports whether r contains at least one Error with the
// given code. Test helper for assertion brevity.
func hasErrorWithCode(r Result, code string) bool {
	for _, v := range r.Errors {
		if v.Code == code {
			return true
		}
	}
	return false
}

func hasWarningWithCode(r Result, code string) bool {
	for _, v := range r.Warnings {
		if v.Code == code {
			return true
		}
	}
	return false
}

// --- Surface-aware block ceiling --------------------------------------------

func TestValidateForSurface_ModalAllows100Blocks(t *testing.T) {
	blocks := make([]slack.Block, MaxBlocksPerModal)
	for i := range blocks {
		blocks[i] = slack.NewDividerBlock()
	}
	// 100 blocks is over the 50-block message limit but within a modal's.
	if r := New().Validate(blocks); r.Valid {
		t.Error("100 blocks should be invalid for the default message surface")
	}
	if r := New().ValidateForSurface(blocks, SurfaceModal); !r.Valid {
		t.Errorf("100 blocks should be valid for a modal; errors=%+v", r.Errors)
	}
	if r := New().ValidateForSurface(blocks, SurfaceHomeTab); !r.Valid {
		t.Errorf("100 blocks should be valid for a home tab; errors=%+v", r.Errors)
	}
}

func TestValidateForSurface_ModalOver100_Error(t *testing.T) {
	blocks := make([]slack.Block, MaxBlocksPerModal+1)
	for i := range blocks {
		blocks[i] = slack.NewDividerBlock()
	}
	r := New().ValidateForSurface(blocks, SurfaceModal)
	if !hasErrorWithCode(r, "blocks_per_message_exceeded") {
		t.Errorf("101 blocks should exceed the modal ceiling; errors=%+v", r.Errors)
	}
}

func TestValidateForSurface_EmptySurface_DefaultsToMessage(t *testing.T) {
	blocks := make([]slack.Block, MaxBlocksPerMessage+1)
	for i := range blocks {
		blocks[i] = slack.NewDividerBlock()
	}
	r := New().ValidateForSurface(blocks, Surface(""))
	if !hasErrorWithCode(r, "blocks_per_message_exceeded") {
		t.Errorf("empty surface should fall back to the 50-block limit; errors=%+v", r.Errors)
	}
}

// --- Cross-block rules ------------------------------------------------------

func TestValidate_OverBlocksPerMessage_Error(t *testing.T) {
	blocks := make([]slack.Block, MaxBlocksPerMessage+1)
	for i := range blocks {
		blocks[i] = slack.NewDividerBlock()
	}
	r := New().Validate(blocks)
	if r.Valid {
		t.Error("expected invalid result for >50 blocks")
	}
	if !hasErrorWithCode(r, "blocks_per_message_exceeded") {
		t.Errorf("missing blocks_per_message_exceeded error; errors=%+v", r.Errors)
	}
}

func TestValidate_DuplicateBlockID_Error(t *testing.T) {
	d1 := slack.NewDividerBlock()
	d1.BlockID = "shared"
	d2 := slack.NewDividerBlock()
	d2.BlockID = "shared"
	r := New().Validate([]slack.Block{d1, d2})
	if !hasErrorWithCode(r, "duplicate_block_id") {
		t.Errorf("missing duplicate_block_id error; errors=%+v", r.Errors)
	}
}

func TestValidate_TwoTables_Error(t *testing.T) {
	r := New().Validate([]slack.Block{slack.NewTableBlock(""), slack.NewTableBlock("")})
	if !hasErrorWithCode(r, "multiple_tables") {
		t.Errorf("missing multiple_tables error; errors=%+v", r.Errors)
	}
}

func TestValidate_OneTable_Valid(t *testing.T) {
	r := New().Validate([]slack.Block{slack.NewTableBlock("")})
	if !r.Valid {
		t.Errorf("single table should validate; errors=%+v", r.Errors)
	}
}

func TestValidate_MarkdownTotalOver12k_Error(t *testing.T) {
	mb1 := slack.NewMarkdownBlock("", strings.Repeat("a", MaxMarkdownTotal/2+1))
	mb2 := slack.NewMarkdownBlock("", strings.Repeat("b", MaxMarkdownTotal/2+1))
	r := New().Validate([]slack.Block{mb1, mb2})
	if !hasErrorWithCode(r, "markdown_block_total_exceeded") {
		t.Errorf("missing markdown_block_total_exceeded error")
	}
}

// --- Section validation -----------------------------------------------------

func TestValidate_SectionEmpty_Error(t *testing.T) {
	s := slack.NewSectionBlock(nil, nil, nil)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "section_empty") {
		t.Errorf("missing section_empty error; errors=%+v", r.Errors)
	}
}

func TestValidate_SectionTextOver3000_Error(t *testing.T) {
	long := strings.Repeat("x", MaxSectionTextChars+1)
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, long, false, false), nil, nil,
	)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "section_text_too_long") {
		t.Errorf("missing section_text_too_long error; errors=%+v", r.Errors)
	}
}

func TestValidate_SectionAtExactLimit_Valid(t *testing.T) {
	at := strings.Repeat("y", MaxSectionTextChars)
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, at, false, false), nil, nil,
	)
	r := New().Validate([]slack.Block{s})
	if !r.Valid {
		t.Errorf("3000-char section text should validate; errors=%+v", r.Errors)
	}
}

func TestValidate_SectionTooManyFields_Error(t *testing.T) {
	fields := make([]*slack.TextBlockObject, MaxSectionFieldCount+1)
	for i := range fields {
		fields[i] = slack.NewTextBlockObject(slack.MarkdownType, "x", false, false)
	}
	s := slack.NewSectionBlock(nil, fields, nil)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "too_many_fields") {
		t.Errorf("missing too_many_fields error; errors=%+v", r.Errors)
	}
}

func TestValidate_SectionFieldOver2000_Error(t *testing.T) {
	long := strings.Repeat("a", MaxSectionFieldsLen+1)
	s := slack.NewSectionBlock(nil, []*slack.TextBlockObject{
		slack.NewTextBlockObject(slack.MarkdownType, long, false, false),
	}, nil)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "section_field_too_long") {
		t.Errorf("missing section_field_too_long error; errors=%+v", r.Errors)
	}
}

// --- Header validation ------------------------------------------------------

func TestValidate_HeaderOver150_Error(t *testing.T) {
	long := strings.Repeat("h", MaxHeaderTextChars+1)
	h := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, long, false, false))
	r := New().Validate([]slack.Block{h})
	if !hasErrorWithCode(r, "header_text_too_long") {
		t.Errorf("missing header_text_too_long error; errors=%+v", r.Errors)
	}
}

func TestValidate_HeaderEmpty_Error(t *testing.T) {
	h := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "", false, false))
	r := New().Validate([]slack.Block{h})
	if !hasErrorWithCode(r, "header_text_empty") {
		t.Errorf("missing header_text_empty error; errors=%+v", r.Errors)
	}
}

func TestValidate_HeaderMrkdwn_Error(t *testing.T) {
	// header.text MUST be plain_text
	h := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.MarkdownType, "Title", false, false))
	r := New().Validate([]slack.Block{h})
	if !hasErrorWithCode(r, "header_must_be_plain_text") {
		t.Errorf("missing header_must_be_plain_text error; errors=%+v", r.Errors)
	}
}

func TestValidate_HeaderAtExactLimit_Valid(t *testing.T) {
	at := strings.Repeat("h", MaxHeaderTextChars)
	h := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, at, false, false))
	r := New().Validate([]slack.Block{h})
	if !r.Valid {
		t.Errorf("150-char header should validate; errors=%+v", r.Errors)
	}
}

// --- Image validation -------------------------------------------------------

func TestValidate_ImageMissingSource_Error(t *testing.T) {
	img := slack.NewImageBlock("", "alt", "", nil)
	r := New().Validate([]slack.Block{img})
	if !hasErrorWithCode(r, "image_missing_source") {
		t.Errorf("missing image_missing_source error; errors=%+v", r.Errors)
	}
}

func TestValidate_ImageEmptyAltText_Warning(t *testing.T) {
	img := slack.NewImageBlock("https://example.com/x.png", "", "", nil)
	r := New().Validate([]slack.Block{img})
	if !r.Valid {
		t.Errorf("missing alt_text should be a warning, not an error; result=%+v", r)
	}
	if !hasWarningWithCode(r, "image_missing_alt_text") {
		t.Errorf("missing image_missing_alt_text warning; warnings=%+v", r.Warnings)
	}
}

func TestValidate_ImageAltTextTooLong_Error(t *testing.T) {
	long := strings.Repeat("a", MaxImageAltTextChars+1)
	img := slack.NewImageBlock("https://example.com/x.png", long, "", nil)
	r := New().Validate([]slack.Block{img})
	if !hasErrorWithCode(r, "alt_text_too_long") {
		t.Errorf("missing alt_text_too_long error; errors=%+v", r.Errors)
	}
}

func TestValidate_ImageURLTooLong_Error(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", MaxImageURLChars)
	img := slack.NewImageBlock(long, "alt", "", nil)
	r := New().Validate([]slack.Block{img})
	if !hasErrorWithCode(r, "image_url_too_long") {
		t.Errorf("missing image_url_too_long error; errors=%+v", r.Errors)
	}
}

func TestValidate_ImageTitleTooLong_Error(t *testing.T) {
	long := strings.Repeat("t", MaxImageTitleChars+1)
	title := slack.NewTextBlockObject(slack.PlainTextType, long, false, false)
	img := slack.NewImageBlock("https://example.com/x.png", "alt", "", title)
	r := New().Validate([]slack.Block{img})
	if !hasErrorWithCode(r, "image_title_too_long") {
		t.Errorf("missing image_title_too_long error; errors=%+v", r.Errors)
	}
}

// --- Actions / button validation --------------------------------------------

func btnText(s string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.PlainTextType, s, false, false)
}

func TestValidate_ActionsTooManyElements_Error(t *testing.T) {
	els := make([]slack.BlockElement, MaxActionsElements+1)
	for i := range els {
		els[i] = &slack.ButtonBlockElement{Type: slack.METButton, Text: btnText("ok")}
	}
	a := slack.NewActionBlock("", els...)
	r := New().Validate([]slack.Block{a})
	if !hasErrorWithCode(r, "too_many_actions") {
		t.Errorf("missing too_many_actions error; errors=%+v", r.Errors)
	}
}

func TestValidate_ButtonFieldsTooLong_Error(t *testing.T) {
	cases := []struct {
		name string
		btn  *slack.ButtonBlockElement
		code string
	}{
		{
			"text", &slack.ButtonBlockElement{
				Type: slack.METButton,
				Text: btnText(strings.Repeat("a", MaxButtonTextChars+1)),
			}, "button_text_too_long",
		},
		{
			"value", &slack.ButtonBlockElement{
				Type: slack.METButton, Text: btnText("ok"),
				Value: strings.Repeat("v", MaxButtonValueChars+1),
			}, "button_value_too_long",
		},
		{
			"url", &slack.ButtonBlockElement{
				Type: slack.METButton, Text: btnText("ok"),
				URL: strings.Repeat("u", MaxButtonURLChars+1),
			}, "button_url_too_long",
		},
		{
			"action_id", &slack.ButtonBlockElement{
				Type: slack.METButton, Text: btnText("ok"),
				ActionID: strings.Repeat("i", MaxActionIDChars+1),
			}, "action_id_too_long",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := slack.NewActionBlock("", tc.btn)
			r := New().Validate([]slack.Block{a})
			if !hasErrorWithCode(r, tc.code) {
				t.Errorf("missing %s error; errors=%+v", tc.code, r.Errors)
			}
		})
	}
}

func TestValidate_ButtonWithinLimits_Valid(t *testing.T) {
	a := slack.NewActionBlock("", &slack.ButtonBlockElement{
		Type: slack.METButton, Text: btnText("Click me"), Value: "ok",
	})
	r := New().Validate([]slack.Block{a})
	if !r.Valid {
		t.Errorf("button within limits should validate; errors=%+v", r.Errors)
	}
}

// --- Context validation -----------------------------------------------------

func TestValidate_ContextTooManyElements_Error(t *testing.T) {
	els := make([]slack.MixedElement, MaxContextElements+1)
	for i := range els {
		els[i] = slack.NewTextBlockObject(slack.MarkdownType, "x", false, false)
	}
	c := slack.NewContextBlock("", els...)
	r := New().Validate([]slack.Block{c})
	if !hasErrorWithCode(r, "too_many_context_elements") {
		t.Errorf("missing too_many_context_elements error; errors=%+v", r.Errors)
	}
}

// --- Table validation -------------------------------------------------------

func TestValidate_TableTooManyRows_Error(t *testing.T) {
	tbl := &slack.TableBlock{Rows: make([][]*slack.RichTextBlock, MaxTableRows+1)}
	r := New().Validate([]slack.Block{tbl})
	if !hasErrorWithCode(r, "too_many_table_rows") {
		t.Errorf("missing too_many_table_rows error; errors=%+v", r.Errors)
	}
}

func TestValidate_TableTooManyColumns_Error(t *testing.T) {
	tbl := &slack.TableBlock{Rows: [][]*slack.RichTextBlock{
		make([]*slack.RichTextBlock, MaxTableColumns+1),
	}}
	r := New().Validate([]slack.Block{tbl})
	if !hasErrorWithCode(r, "too_many_table_columns") {
		t.Errorf("missing too_many_table_columns error; errors=%+v", r.Errors)
	}
}

func TestValidate_TableTooManyColumnSettings_Error(t *testing.T) {
	tbl := &slack.TableBlock{
		Rows:           [][]*slack.RichTextBlock{{}},
		ColumnSettings: make([]slack.ColumnSetting, MaxTableColumns+1),
	}
	r := New().Validate([]slack.Block{tbl})
	if !hasErrorWithCode(r, "too_many_column_settings") {
		t.Errorf("missing too_many_column_settings error; errors=%+v", r.Errors)
	}
}

func TestValidate_TableWithinLimits_Valid(t *testing.T) {
	tbl := &slack.TableBlock{Rows: [][]*slack.RichTextBlock{
		make([]*slack.RichTextBlock, 3),
		make([]*slack.RichTextBlock, 3),
	}}
	r := New().Validate([]slack.Block{tbl})
	if !r.Valid {
		t.Errorf("small table should validate; errors=%+v", r.Errors)
	}
}

// --- Section accessory validation -------------------------------------------

func TestValidate_AccessoryImageAltTextTooLong_Error(t *testing.T) {
	acc := slack.NewAccessory(&slack.ImageBlockElement{
		Type:    slack.METImage,
		AltText: strings.Repeat("a", MaxImageAltTextChars+1),
	})
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "hi", false, false), nil, acc,
	)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "alt_text_too_long") {
		t.Errorf("missing alt_text_too_long error for accessory; errors=%+v", r.Errors)
	}
}

func TestValidate_AccessoryButtonTextTooLong_Error(t *testing.T) {
	acc := slack.NewAccessory(&slack.ButtonBlockElement{
		Type: slack.METButton,
		Text: btnText(strings.Repeat("b", MaxButtonTextChars+1)),
	})
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "hi", false, false), nil, acc,
	)
	r := New().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "button_text_too_long") {
		t.Errorf("missing button_text_too_long error for accessory; errors=%+v", r.Errors)
	}
}

// --- Block ID validation ----------------------------------------------------

func TestValidate_BlockIDTooLong_Error(t *testing.T) {
	d := slack.NewDividerBlock()
	d.BlockID = strings.Repeat("x", MaxBlockIDChars+1)
	r := New().Validate([]slack.Block{d})
	if !hasErrorWithCode(r, "block_id_too_long") {
		t.Errorf("missing block_id_too_long error; errors=%+v", r.Errors)
	}
}

// --- Strict mode ------------------------------------------------------------

func TestValidate_Strict_RejectsMrkdwnSection(t *testing.T) {
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "hi", false, false), nil, nil,
	)
	r := NewStrict().Validate([]slack.Block{s})
	if !hasErrorWithCode(r, "deprecated_mrkdwn_section") {
		t.Errorf("strict mode should flag mrkdwn section as deprecated; errors=%+v", r.Errors)
	}
}

func TestValidate_NonStrict_AllowsMrkdwnSection(t *testing.T) {
	s := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, "hi", false, false), nil, nil,
	)
	r := New().Validate([]slack.Block{s})
	if !r.Valid {
		t.Errorf("non-strict should allow mrkdwn section; errors=%+v", r.Errors)
	}
}

// --- Happy paths ------------------------------------------------------------

func TestValidate_SimpleValidPayload_Valid(t *testing.T) {
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Title", false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "body text", false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),
	}
	r := New().Validate(blocks)
	if !r.Valid {
		t.Errorf("expected valid; errors=%+v", r.Errors)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("expected no warnings; got %+v", r.Warnings)
	}
}

func TestValidate_EmptyPayload_Valid(t *testing.T) {
	r := New().Validate(nil)
	if !r.Valid {
		t.Error("empty payload should validate")
	}
}

// --- Path notation ----------------------------------------------------------

func TestValidate_ErrorPath_UsesBracketNotation(t *testing.T) {
	// Make blocks[2] invalid; assert path is `blocks[2]...`.
	blocks := []slack.Block{
		slack.NewDividerBlock(),
		slack.NewDividerBlock(),
		slack.NewSectionBlock(nil, nil, nil), // empty section → error
	}
	r := New().Validate(blocks)
	if !hasErrorWithCode(r, "section_empty") {
		t.Fatalf("missing section_empty; errors=%+v", r.Errors)
	}
	for _, v := range r.Errors {
		if v.Code == "section_empty" && !strings.HasPrefix(v.Path, "blocks[2]") {
			t.Errorf("path = %q, want prefix `blocks[2]`", v.Path)
		}
	}
}
