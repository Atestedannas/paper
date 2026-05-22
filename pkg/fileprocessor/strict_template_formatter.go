package fileprocessor

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/ofc/sharedTypes"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

type StrictTemplateFormatter struct {
	processor *EnhancedProcessor
}

type inlinePrefixRule struct {
	Prefix        string
	ParagraphSpec ParagraphFormatSpec
	LabelSpec     ParagraphFormatSpec
	BodySpec      ParagraphFormatSpec
}

type strictBlockKind string

const (
	strictBlockUnknown               strictBlockKind = ""
	strictBlockCoverDate             strictBlockKind = "cover_date"
	strictBlockPaperTitle            strictBlockKind = "paper_title"
	strictBlockAbstractCN            strictBlockKind = "abstract_cn"
	strictBlockKeywordsCN            strictBlockKind = "keywords_cn"
	strictBlockAbstractEN            strictBlockKind = "abstract_en"
	strictBlockKeywordsEN            strictBlockKind = "keywords_en"
	strictBlockTOCTitle              strictBlockKind = "toc_title"
	strictBlockTOCEntry              strictBlockKind = "toc_entry"
	strictBlockHeading1              strictBlockKind = "heading_1"
	strictBlockHeading2              strictBlockKind = "heading_2"
	strictBlockHeading3              strictBlockKind = "heading_3"
	strictBlockBody                  strictBlockKind = "body"
	strictBlockFigureCaption         strictBlockKind = "figure_caption"
	strictBlockTableCaption          strictBlockKind = "table_caption"
	strictBlockReferencesTitle       strictBlockKind = "references_title"
	strictBlockReferencesItem        strictBlockKind = "references_item"
	strictBlockAcknowledgementsTitle strictBlockKind = "ack_title"
	strictBlockAcknowledgementsBody  strictBlockKind = "ack_body"
)

type strictTemplateBlockRules struct {
	Paragraph       map[strictBlockKind]ParagraphFormatSpec
	Inline          map[strictBlockKind]inlinePrefixRule
	CoverTitleLabel ParagraphFormatSpec
	CoverTitleValue ParagraphFormatSpec
	CoverInfoLabel  ParagraphFormatSpec
	CoverInfoValue  ParagraphFormatSpec
}

type strictParagraphRef struct {
	Para     document.Paragraph
	InTOCSDT bool
}

type strictParagraphState struct {
	CoverDateSeen bool
	PaperTitleSet bool
	InReferences  bool
	InAck         bool
}

func NewStrictTemplateFormatter() *StrictTemplateFormatter {
	return &StrictTemplateFormatter{
		processor: NewEnhancedProcessor(),
	}
}

func (f *StrictTemplateFormatter) Format(ctx context.Context, config SingleTemplateFormatConfig) (string, error) {
	_ = ctx

	if err := validateSingleTemplateConfig(config); err != nil {
		return "", err
	}

	userDoc, err := document.Open(config.UserPaperPath)
	if err != nil {
		return "", fmt.Errorf("open user paper: %w", err)
	}
	defer userDoc.Close()

	templateDoc, err := document.Open(config.TemplatePath)
	if err != nil {
		return "", fmt.Errorf("open template: %w", err)
	}
	defer templateDoc.Close()

	CloneStyles(templateDoc, userDoc)
	applyTemplateSectionLayout(userDoc, templateDoc)

	if err := f.applyParagraphAndRunFormatting(userDoc, templateDoc); err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(config.OutputPath), 0755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	if err := userDoc.SaveToFile(config.OutputPath); err != nil {
		return "", fmt.Errorf("save strict formatted doc: %w", err)
	}
	if err := restoreStrictCoverTablesFromTemplate(config.TemplatePath, config.OutputPath); err != nil {
		return "", fmt.Errorf("restore strict cover tables from template: %w", err)
	}

	if err := copyTemplateHeaderFooterPackage(config.TemplatePath, config.OutputPath); err != nil {
		return "", fmt.Errorf("copy template header/footer package: %w", err)
	}
	if err := postProcessStrictOutput(config.OutputPath); err != nil {
		return "", fmt.Errorf("post-process strict output: %w", err)
	}

	return config.OutputPath, nil
}

func (f *StrictTemplateFormatter) applyParagraphAndRunFormatting(userDoc, templateDoc *document.Document) error {
	rules := extractStrictTemplateBlockRules(templateDoc, f.processor)
	applyStrictParagraphBlockRules(userDoc, rules, f.processor)
	f.processor.applyThreeLineTableFormat(userDoc)
	applyStrictCoverTableRules(userDoc, rules, f.processor)
	applyStrictCoverDateRule(userDoc, rules, f.processor)
	return nil
}

func applyTemplateSectionLayout(userDoc, templateDoc *document.Document) {
	if userDoc == nil || templateDoc == nil {
		return
	}

	userSectPr := userDoc.BodySection().X()
	templateSectPr := templateDoc.BodySection().X()
	if userSectPr == nil || templateSectPr == nil {
		return
	}

	if templateSectPr.PgSz != nil {
		if userSectPr.PgSz == nil {
			userSectPr.PgSz = wml.NewCT_PageSz()
		}
		userSectPr.PgSz.WAttr = templateSectPr.PgSz.WAttr
		userSectPr.PgSz.HAttr = templateSectPr.PgSz.HAttr
		userSectPr.PgSz.OrientAttr = templateSectPr.PgSz.OrientAttr
	}

	if templateSectPr.PgMar != nil {
		userSectPr.PgMar = templateSectPr.PgMar
	}
}

func extractStrictTemplateBlockRules(templateDoc *document.Document, processor *EnhancedProcessor) strictTemplateBlockRules {
	rules := strictTemplateBlockRules{
		Paragraph: make(map[strictBlockKind]ParagraphFormatSpec),
		Inline:    make(map[strictBlockKind]inlinePrefixRule),
	}

	extractStrictInlineBlockRules(&rules, templateDoc, processor)
	extractStrictCoverTableRules(&rules, templateDoc, processor)
	extractStrictParagraphRules(&rules, templateDoc, processor)
	applyStrictRuleFallbacks(&rules)

	return rules
}

func extractStrictInlineBlockRules(rules *strictTemplateBlockRules, templateDoc *document.Document, processor *EnhancedProcessor) {
	for _, rule := range extractInlinePrefixRules(templateDoc, processor) {
		kind := strictInlineBlockForPrefix(rule.Prefix)
		if kind == strictBlockUnknown {
			continue
		}
		rules.Inline[kind] = rule
	}
}

func strictInlineBlockForPrefix(prefix string) strictBlockKind {
	switch strings.TrimSpace(prefix) {
	case "摘要：":
		return strictBlockAbstractCN
	case "关键词：":
		return strictBlockKeywordsCN
	case "Abstract:", "Abstract：":
		return strictBlockAbstractEN
	case "Key words:", "Key words：", "Keywords:", "Keywords：":
		return strictBlockKeywordsEN
	default:
		return strictBlockUnknown
	}
}

func extractStrictCoverTableRules(rules *strictTemplateBlockRules, templateDoc *document.Document, processor *EnhancedProcessor) {
	if table, ok := findCoverTitleTable(templateDoc, processor); ok {
		rules.CoverTitleLabel = extractFirstTableColumnSpec(table, processor, 0)
		rules.CoverTitleValue = extractFirstNonLabelTableSpec(table, processor, 1)
	}
	if table, ok := findCoverInfoTable(templateDoc, processor); ok {
		rules.CoverInfoLabel = extractFirstTableColumnSpec(table, processor, 0)
		rules.CoverInfoValue = extractFirstNonLabelTableSpec(table, processor, 1)
	}
}

func extractStrictParagraphRules(rules *strictTemplateBlockRules, templateDoc *document.Document, processor *EnhancedProcessor) {
	refs := strictMainStoryParagraphs(templateDoc)

	assignRule := func(kind strictBlockKind, para document.Paragraph, ok bool) {
		if !ok {
			return
		}
		spec := sanitizeParagraphFormatSpec(extractParaFormatSpec(para))
		if spec.IsEmpty() {
			return
		}
		rules.Paragraph[kind] = spec
	}

	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isCoverDateText(text)
	}); ok {
		assignRule(strictBlockCoverDate, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isTemplatePaperTitleCandidateText(text)
	}); ok {
		assignRule(strictBlockPaperTitle, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isTOCTitleText(text)
	}); ok {
		assignRule(strictBlockTOCTitle, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return ref.InTOCSDT || isTOCEntryLikeText(text)
	}); ok {
		assignRule(strictBlockTOCEntry, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isHeading1Text(text)
	}); ok {
		assignRule(strictBlockHeading1, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isHeading2Text(text)
	}); ok {
		assignRule(strictBlockHeading2, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isHeading3Text(text)
	}); ok {
		assignRule(strictBlockHeading3, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isMainTextFigureCaption(text)
	}); ok {
		assignRule(strictBlockFigureCaption, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isMainTextTableCaption(text)
	}); ok {
		assignRule(strictBlockTableCaption, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isReferencesTitleText(text) && isStandaloneShortTitle(text)
	}); ok {
		assignRule(strictBlockReferencesTitle, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return !isTemplateInstructionText(text) && isReferenceItemText(text)
	}); ok {
		assignRule(strictBlockReferencesItem, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isAcknowledgementsTitleText(text) && isStandaloneShortTitle(text)
	}); ok {
		assignRule(strictBlockAcknowledgementsTitle, para, true)
	}
	if para, ok := findStrictParagraph(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isBodySampleCandidateText(text)
	}); ok {
		assignRule(strictBlockBody, para, true)
	}

	if ackTitleIdx := strictParagraphIndex(refs, processor, func(ref strictParagraphRef, text string) bool {
		return isAcknowledgementsTitleText(text) && isStandaloneShortTitle(text)
	}); ackTitleIdx >= 0 {
		ackSearchRefs := strictParagraphsBeforeSectionBoundary(refs[ackTitleIdx+1:], processor)
		if para, ok := findStrictParagraph(ackSearchRefs, processor, func(ref strictParagraphRef, text string) bool {
			return !isTemplateInstructionText(text) && len([]rune(normalizeVisibleText(text))) > 30
		}); ok {
			assignRule(strictBlockAcknowledgementsBody, para, true)
		}
	}
}

func applyStrictRuleFallbacks(rules *strictTemplateBlockRules) {
	if rules.CoverTitleValue.IsEmpty() {
		rules.CoverTitleValue = ParagraphFormatSpec{
			FontEastAsia:     "黑体",
			FontAscii:        "SimHei",
			FontSizeHalfPt:   44,
			FontSizeCSHalfPt: 44,
			Bold:             true,
			AlignmentSet:     true,
			Alignment:        wml.ST_JcCenter,
			LineSpacingVal:   360,
			LineSpacingRule:  wml.ST_LineSpacingRuleAuto,
		}
	}
	if rules.CoverInfoValue.IsEmpty() {
		rules.CoverInfoValue = ParagraphFormatSpec{
			FontEastAsia:     "宋体",
			FontAscii:        "SimSun",
			FontSizeHalfPt:   28,
			FontSizeCSHalfPt: 28,
			AlignmentSet:     true,
			Alignment:        wml.ST_JcLeft,
			LineSpacingVal:   360,
			LineSpacingRule:  wml.ST_LineSpacingRuleAuto,
		}
	}
	if rules.CoverTitleLabel.IsEmpty() {
		rules.CoverTitleLabel = rules.CoverInfoValue
	}
	if rules.CoverInfoLabel.IsEmpty() {
		rules.CoverInfoLabel = rules.CoverInfoValue
	}
	if spec, ok := rules.Paragraph[strictBlockCoverDate]; !ok || spec.IsEmpty() {
		rules.Paragraph[strictBlockCoverDate] = ParagraphFormatSpec{
			FontEastAsia:     "宋体",
			FontAscii:        "SimSun",
			FontSizeHalfPt:   32,
			FontSizeCSHalfPt: 32,
			Bold:             true,
			AlignmentSet:     true,
			Alignment:        wml.ST_JcCenter,
		}
	}
	if spec, ok := rules.Paragraph[strictBlockTOCTitle]; !ok || spec.IsEmpty() {
		rules.Paragraph[strictBlockTOCTitle] = ParagraphFormatSpec{
			FontEastAsia:     "黑体",
			FontAscii:        "SimHei",
			FontSizeHalfPt:   32,
			FontSizeCSHalfPt: 32,
			Bold:             true,
			AlignmentSet:     true,
			Alignment:        wml.ST_JcCenter,
		}
	}
	if spec, ok := rules.Paragraph[strictBlockAcknowledgementsBody]; !ok || spec.IsEmpty() {
		if bodySpec, bodyOK := rules.Paragraph[strictBlockBody]; bodyOK && !bodySpec.IsEmpty() {
			rules.Paragraph[strictBlockAcknowledgementsBody] = bodySpec
		}
	}
	if spec, ok := rules.Paragraph[strictBlockFigureCaption]; !ok || spec.IsEmpty() {
		if tableSpec, tableOK := rules.Paragraph[strictBlockTableCaption]; tableOK && !tableSpec.IsEmpty() {
			rules.Paragraph[strictBlockFigureCaption] = tableSpec
		}
	}
	if spec, ok := rules.Paragraph[strictBlockReferencesTitle]; ok && !spec.IsEmpty() {
		spec.PageBreak = true
		rules.Paragraph[strictBlockReferencesTitle] = spec
	}
	if spec, ok := rules.Paragraph[strictBlockAcknowledgementsTitle]; ok && !spec.IsEmpty() {
		spec.PageBreak = true
		rules.Paragraph[strictBlockAcknowledgementsTitle] = spec
	}
	if rule, ok := rules.Inline[strictBlockAbstractEN]; ok {
		if bodySpec, bodyOK := rules.Paragraph[strictBlockBody]; bodyOK && !bodySpec.IsEmpty() {
			if rule.BodySpec.IsEmpty() || (rule.BodySpec.Bold == rule.LabelSpec.Bold && rule.BodySpec.FontSizeHalfPt == rule.LabelSpec.FontSizeHalfPt) {
				rule.BodySpec.FontEastAsia = bodySpec.FontEastAsia
				rule.BodySpec.FontAscii = bodySpec.FontAscii
				rule.BodySpec.FontSizeHalfPt = bodySpec.FontSizeHalfPt
				rule.BodySpec.FontSizeCSHalfPt = bodySpec.FontSizeCSHalfPt
				rule.BodySpec.Bold = false
				rules.Inline[strictBlockAbstractEN] = rule
			}
		}
	}
}

func applyStrictParagraphBlockRules(userDoc *document.Document, rules strictTemplateBlockRules, processor *EnhancedProcessor) {
	state := strictParagraphState{}
	for _, ref := range strictMainStoryParagraphs(userDoc) {
		text := strings.TrimSpace(processor.extractParagraphText(ref.Para))
		if text == "" {
			continue
		}
		kind := detectStrictParagraphBlock(ref, text, &state)
		applyStrictRuleToParagraph(ref.Para, text, kind, rules, processor)
	}
}

func applyStrictRuleToParagraph(para document.Paragraph, text string, kind strictBlockKind, rules strictTemplateBlockRules, processor *EnhancedProcessor) {
	if kind == strictBlockUnknown {
		return
	}
	if inlineRule, ok := rules.Inline[kind]; ok {
		applyInlinePrefixRuleToParagraph(processor, para, text, []inlinePrefixRule{inlineRule})
		return
	}
	spec, ok := rules.Paragraph[kind]
	if !ok || spec.IsEmpty() {
		return
	}
	applyStrictSpecToParagraph(processor, para, spec)
}

func applyStrictCoverTableRules(userDoc *document.Document, rules strictTemplateBlockRules, processor *EnhancedProcessor) {
	if table, ok := findCoverTitleTable(userDoc, processor); ok {
		applyStrictCoverTableRule(table, processor, rules.CoverTitleLabel, rules.CoverTitleValue)
	}
	if table, ok := findCoverInfoTable(userDoc, processor); ok {
		applyStrictCoverTableRule(table, processor, rules.CoverInfoLabel, rules.CoverInfoValue)
	}
}

func applyStrictCoverTableRule(table document.Table, processor *EnhancedProcessor, labelSpec, valueSpec ParagraphFormatSpec) {
	for _, row := range table.Rows() {
		for idx, cell := range row.Cells() {
			spec := valueSpec
			if idx == 0 {
				spec = labelSpec
			}
			if spec.IsEmpty() {
				continue
			}
			for _, para := range cell.Paragraphs() {
				if strings.TrimSpace(processor.extractParagraphText(para)) == "" {
					continue
				}
				applyStrictSpecToParagraph(processor, para, spec)
			}
		}
	}
}

func applyStrictCoverDateRule(userDoc *document.Document, rules strictTemplateBlockRules, processor *EnhancedProcessor) {
	datePara, ok := findCoverDateParagraph(userDoc, processor)
	if !ok {
		return
	}
	dateText := normalizeCoverDateText(processor.extractParagraphText(datePara))
	if dateText != "" {
		replaceParagraphText(datePara, dateText)
	}
	spec, ok := rules.Paragraph[strictBlockCoverDate]
	if !ok || spec.IsEmpty() {
		return
	}
	applyStrictSpecToParagraph(processor, datePara, spec)
}

func strictMainStoryParagraphs(doc *document.Document) []strictParagraphRef {
	if doc == nil || doc.Document == nil || doc.Document.Body == nil {
		return nil
	}
	refs := make([]strictParagraphRef, 0, 256)
	var walkSdt func(content *wml.CT_SdtContentBlock, inTOC bool)
	var walk func(blocks []*wml.EG_ContentBlockContent, inTOC bool)
	walkSdt = func(content *wml.CT_SdtContentBlock, inTOC bool) {
		if content == nil {
			return
		}
		for _, p := range content.P {
			refs = append(refs, strictParagraphRef{
				Para:     document.Paragraph{Document: doc, WParagraph: p},
				InTOCSDT: inTOC,
			})
		}
		if content.Sdt != nil {
			walkSdt(content.Sdt.SdtContent, true)
		}
	}
	walk = func(blocks []*wml.EG_ContentBlockContent, inTOC bool) {
		for _, block := range blocks {
			for _, p := range block.P {
				refs = append(refs, strictParagraphRef{
					Para:     document.Paragraph{Document: doc, WParagraph: p},
					InTOCSDT: inTOC,
				})
			}
			if block.Sdt != nil && block.Sdt.SdtContent != nil {
				walkSdt(block.Sdt.SdtContent, true)
			}
		}
	}
	for _, block := range doc.Document.Body.EG_BlockLevelElts {
		walk(block.EG_ContentBlockContent, false)
	}
	return refs
}

func findStrictParagraph(refs []strictParagraphRef, processor *EnhancedProcessor, match func(strictParagraphRef, string) bool) (document.Paragraph, bool) {
	for _, ref := range refs {
		text := strings.TrimSpace(processor.extractParagraphText(ref.Para))
		if text == "" {
			continue
		}
		if match(ref, text) {
			return ref.Para, true
		}
	}
	return document.Paragraph{}, false
}

func strictParagraphIndex(refs []strictParagraphRef, processor *EnhancedProcessor, match func(strictParagraphRef, string) bool) int {
	for idx, ref := range refs {
		text := strings.TrimSpace(processor.extractParagraphText(ref.Para))
		if text == "" {
			continue
		}
		if match(ref, text) {
			return idx
		}
	}
	return -1
}

func detectStrictParagraphBlock(ref strictParagraphRef, text string, state *strictParagraphState) strictBlockKind {
	switch {
	case ref.InTOCSDT:
		return strictBlockTOCEntry
	case isCoverDateText(text):
		state.CoverDateSeen = true
		return strictBlockCoverDate
	case hasVisiblePrefix(text, "摘要："):
		state.CoverDateSeen = true
		return strictBlockAbstractCN
	case hasVisiblePrefix(text, "关键词："):
		state.CoverDateSeen = true
		return strictBlockKeywordsCN
	case hasVisiblePrefix(text, "Abstract:") || hasVisiblePrefix(text, "Abstract："):
		state.CoverDateSeen = true
		return strictBlockAbstractEN
	case hasVisiblePrefix(text, "Key words:") || hasVisiblePrefix(text, "Key words：") || hasVisiblePrefix(text, "Keywords:") || hasVisiblePrefix(text, "Keywords："):
		state.CoverDateSeen = true
		return strictBlockKeywordsEN
	case isTOCTitleText(text):
		state.CoverDateSeen = true
		return strictBlockTOCTitle
	case isReferencesTitleText(text):
		state.InReferences = true
		state.InAck = false
		return strictBlockReferencesTitle
	case isAcknowledgementsTitleText(text):
		state.InReferences = false
		state.InAck = true
		return strictBlockAcknowledgementsTitle
	case !state.CoverDateSeen:
		return strictBlockUnknown
	case state.InReferences && isReferenceItemText(text):
		return strictBlockReferencesItem
	case state.InAck:
		return strictBlockAcknowledgementsBody
	case isMainTextFigureCaption(text):
		return strictBlockFigureCaption
	case isMainTextTableCaption(text):
		return strictBlockTableCaption
	case isHeading3Text(text):
		return strictBlockHeading3
	case isHeading2Text(text):
		return strictBlockHeading2
	case isHeading1Text(text):
		return strictBlockHeading1
	case isPaperTitleCandidateText(text, state):
		state.PaperTitleSet = true
		return strictBlockPaperTitle
	default:
		return strictBlockBody
	}
}

func findCoverTitleTable(doc *document.Document, processor *EnhancedProcessor) (document.Table, bool) {
	for _, table := range frontMatterTables(doc, processor) {
		if tableContainsCellText(table, processor, "题目") || tableContainsCellText(table, processor, "Title") {
			return table, true
		}
	}
	return document.Table{}, false
}

func findCoverInfoTable(doc *document.Document, processor *EnhancedProcessor) (document.Table, bool) {
	bestScore := 0
	bestIdx := -1
	candidates := frontMatterTables(doc, processor)
	for idx, table := range candidates {
		score := coverInfoTableScore(table, processor)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}
	if bestIdx >= 0 && bestScore >= 1 {
		return candidates[bestIdx], true
	}
	return document.Table{}, false
}

func frontMatterTables(doc *document.Document, processor *EnhancedProcessor) []document.Table {
	if doc == nil || doc.Document == nil || doc.Document.Body == nil {
		return nil
	}
	tables := make([]document.Table, 0, 4)
	stop := false
	for _, block := range doc.Document.Body.EG_BlockLevelElts {
		if stop {
			break
		}
		for _, content := range block.EG_ContentBlockContent {
			for _, para := range content.P {
				text := strings.TrimSpace(processor.extractParagraphText(document.Paragraph{Document: doc, WParagraph: para}))
				if text == "" {
					continue
				}
				if hasVisiblePrefix(text, "摘要：") || hasVisiblePrefix(text, "Abstract:") || hasVisiblePrefix(text, "Abstract：") || isTOCTitleText(text) || isHeading1Text(text) {
					stop = true
					break
				}
			}
			if stop {
				break
			}
			for _, tbl := range content.Tbl {
				tables = append(tables, document.Table{Document: doc, WTable: tbl})
			}
		}
	}
	if len(tables) == 0 {
		return doc.Tables()
	}
	return tables
}

func tableContainsCellText(table document.Table, processor *EnhancedProcessor, needle string) bool {
	needle = stripAllSpaces(needle)
	for _, row := range table.Rows() {
		for _, cell := range row.Cells() {
			if stripAllSpaces(cellVisibleText(cell)) == needle {
				return true
			}
		}
	}
	return false
}

func coverInfoTableScore(table document.Table, processor *EnhancedProcessor) int {
	expected := map[string]struct{}{
		"学院":        {},
		"专业":        {},
		"班级":        {},
		"学号":        {},
		"姓名":        {},
		"指导教师":      {},
		"college":   {},
		"major":     {},
		"class":     {},
		"studentid": {},
		"name":      {},
		"advisor":   {},
	}
	score := 0
	for _, row := range table.Rows() {
		cells := row.Cells()
		if len(cells) == 0 {
			continue
		}
		label := strings.ToLower(stripAllSpaces(cellVisibleText(cells[0])))
		if _, ok := expected[label]; ok {
			score++
		}
	}
	return score
}

func extractFirstTableColumnSpec(table document.Table, processor *EnhancedProcessor, column int) ParagraphFormatSpec {
	for _, row := range table.Rows() {
		cells := row.Cells()
		if column >= len(cells) {
			continue
		}
		if spec, ok := extractFirstNonEmptyParagraphSpec(cells[column]); ok {
			return spec
		}
	}
	return ParagraphFormatSpec{}
}

func extractFirstNonLabelTableSpec(table document.Table, processor *EnhancedProcessor, column int) ParagraphFormatSpec {
	for _, row := range table.Rows() {
		cells := row.Cells()
		if column >= len(cells) {
			continue
		}
		if stripAllSpaces(cellVisibleText(cells[column])) == "" {
			continue
		}
		if spec, ok := extractFirstNonEmptyParagraphSpec(cells[column]); ok {
			return spec
		}
	}
	return ParagraphFormatSpec{}
}

func extractFirstNonEmptyParagraphSpec(cell document.Cell) (ParagraphFormatSpec, bool) {
	for _, para := range cell.Paragraphs() {
		if strings.TrimSpace(paragraphVisibleText(para)) == "" {
			continue
		}
		spec := sanitizeParagraphFormatSpec(extractParaFormatSpec(para))
		if spec.IsEmpty() {
			continue
		}
		return spec, true
	}
	return ParagraphFormatSpec{}, false
}

func cellVisibleText(cell document.Cell) string {
	var builder strings.Builder
	for _, para := range cell.Paragraphs() {
		builder.WriteString(paragraphVisibleText(para))
	}
	return builder.String()
}

func hasVisiblePrefix(text, prefix string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), prefix)
}

func isTOCTitleText(text string) bool {
	return stripAllSpaces(normalizeVisibleText(text)) == "目录"
}

func isTOCEntryLikeText(text string) bool {
	normalized := normalizeVisibleText(text)
	if normalized == "" || isTemplateInstructionText(normalized) {
		return false
	}
	if !regexp.MustCompile(`\d+\s*$`).MatchString(normalized) {
		return false
	}
	return strings.Contains(normalized, "．．") || strings.Contains(normalized, "...") || isHeading1Text(normalized) || strings.Contains(normalized, "参考文献") || strings.Contains(normalized, "致谢")
}

func isHeading1Text(text string) bool {
	normalized := normalizeVisibleText(text)
	if len([]rune(normalized)) == 0 || len([]rune(normalized)) > 40 {
		return false
	}
	return regexp.MustCompile(`^\d+\s*[^\d\.\s]`).MatchString(normalized) && !regexp.MustCompile(`^\d+\.\d`).MatchString(normalized)
}

func isHeading2Text(text string) bool {
	normalized := normalizeVisibleText(text)
	if len([]rune(normalized)) == 0 || len([]rune(normalized)) > 50 {
		return false
	}
	return regexp.MustCompile(`^\d+\.\d+\s*[^\d\.\s]`).MatchString(normalized) && !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(normalized)
}

func isHeading3Text(text string) bool {
	normalized := normalizeVisibleText(text)
	if len([]rune(normalized)) == 0 || len([]rune(normalized)) > 60 {
		return false
	}
	return regexp.MustCompile(`^\d+\.\d+\.\d+\s*[^\d\.\s]`).MatchString(normalized)
}

func isTableCaptionText(text string) bool {
	normalized := normalizeVisibleText(text)
	return regexp.MustCompile(`^表\s*\d+[-－—]\d+`).MatchString(normalized)
}

func matchesTableCaptionText(text string) bool {
	normalized := normalizeVisibleText(text)
	return regexp.MustCompile(`^(?:续)?表\s*\d+[.\-．－]\d+`).MatchString(normalized)
}

func isFigureCaptionText(text string) bool {
	normalized := normalizeVisibleText(text)
	return regexp.MustCompile(`^图\s*\d+[.\-．－]\d+`).MatchString(normalized)
}

func isMainTextTableCaption(text string) bool {
	normalized := normalizeVisibleText(text)
	return regexp.MustCompile("^(?:\u7eed)?\u8868\\s*\\d+[.\\-\uFF0E\uFF0D]\\d+").MatchString(normalized)
}

func isMainTextFigureCaption(text string) bool {
	normalized := normalizeVisibleText(text)
	return regexp.MustCompile("^\u56fe\\s*\\d+[.\\-\uFF0E\uFF0D]\\d+").MatchString(normalized)
}

func isReferencesTitleText(text string) bool {
	return stripAllSpaces(normalizeVisibleText(text)) == "参考文献"
}

func isAcknowledgementsTitleText(text string) bool {
	normalized := stripAllSpaces(normalizeVisibleText(text))
	return normalized == "致谢"
}

func isStandaloneShortTitle(text string) bool {
	return len([]rune(stripAllSpaces(normalizeVisibleText(text)))) <= 6
}

func isReferenceItemText(text string) bool {
	return regexp.MustCompile(`^\[\d+\]`).MatchString(strings.TrimSpace(text))
}

func isBodySampleCandidateText(text string) bool {
	normalized := normalizeVisibleText(text)
	if normalized == "" || isTemplateInstructionText(normalized) {
		return false
	}
	if len([]rune(normalized)) < 30 {
		return false
	}
	if hasVisiblePrefix(normalized, "摘要：") || hasVisiblePrefix(normalized, "关键词：") || hasVisiblePrefix(normalized, "Abstract:") || hasVisiblePrefix(normalized, "Abstract：") {
		return false
	}
	if isTOCEntryLikeText(normalized) || isHeading1Text(normalized) || isHeading2Text(normalized) || isHeading3Text(normalized) || isReferenceItemText(normalized) || isMainTextTableCaption(normalized) || isMainTextFigureCaption(normalized) {
		return false
	}
	return true
}

func isTemplatePaperTitleCandidateText(text string) bool {
	normalized := normalizeVisibleText(text)
	if normalized == "" || isTemplateInstructionText(normalized) {
		return false
	}
	if len([]rune(normalized)) < 10 || len([]rune(normalized)) > 40 {
		return false
	}
	if strings.Contains(normalized, "本科毕业论文") || hasVisiblePrefix(normalized, "摘要：") || hasVisiblePrefix(normalized, "Abstract:") || isHeading1Text(normalized) {
		return false
	}
	return true
}

func isPaperTitleCandidateText(text string, state *strictParagraphState) bool {
	normalized := normalizeVisibleText(text)
	if !state.CoverDateSeen || state.PaperTitleSet {
		return false
	}
	if len([]rune(normalized)) < 10 || len([]rune(normalized)) > 40 {
		return false
	}
	if strings.Contains(normalized, "本科毕业论文") || hasVisiblePrefix(normalized, "摘要：") || hasVisiblePrefix(normalized, "Abstract:") || hasVisiblePrefix(normalized, "Abstract：") || isHeading1Text(normalized) || isTOCTitleText(normalized) {
		return false
	}
	return true
}

func stripAllSpaces(text string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "", "　", "")
	return replacer.Replace(text)
}

func extractStrictTemplateSpecs(templateDoc *document.Document, processor *EnhancedProcessor) map[string]ParagraphFormatSpec {
	specs := make(map[string]ParagraphFormatSpec)
	classified := NewV2DeterministicClassifier(processor).Classify(BodyLevelParagraphsOnly(templateDoc))
	type candidate struct {
		spec  ParagraphFormatSpec
		score int
	}
	best := make(map[string]candidate)
	for _, para := range classified {
		text := strings.TrimSpace(para.Text)
		if text == "" || isTemplateInstructionText(text) {
			continue
		}
		spec := sanitizeParagraphFormatSpec(extractParaFormatSpec(para.Para))
		if spec.IsEmpty() {
			continue
		}
		score := strictTemplateSampleScore(text, spec)
		if current, exists := best[para.Type]; !exists || score > current.score {
			best[para.Type] = candidate{spec: spec, score: score}
		}
	}
	for paraType, picked := range best {
		specs[paraType] = picked.spec
	}
	return specs
}

func resolveStrictTemplateSpec(specs map[string]ParagraphFormatSpec, paraType string) (ParagraphFormatSpec, bool) {
	if spec, ok := specs[paraType]; ok {
		return spec, true
	}
	if fallback := getFallbackType(paraType); fallback != "" {
		spec, ok := specs[fallback]
		return spec, ok
	}
	return ParagraphFormatSpec{}, false
}

func applyStrictSpecToParagraph(processor *EnhancedProcessor, para document.Paragraph, spec ParagraphFormatSpec) {
	pPr := para.X().PPr
	if pPr == nil {
		pPr = wml.NewCT_PPr()
		para.X().PPr = pPr
	}

	if spec.AlignmentSet {
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = spec.Alignment
	}

	if spec.LineSpacingVal > 0 || spec.SpaceBefore > 0 || spec.SpaceAfter > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		if spec.LineSpacingVal > 0 {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{Int64: &spec.LineSpacingVal}
			pPr.Spacing.LineRuleAttr = spec.LineSpacingRule
		}
		if spec.SpaceBefore > 0 {
			pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.SpaceBefore}
		}
		if spec.SpaceAfter > 0 {
			pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.SpaceAfter}
		}
	}

	if spec.FirstLineIndent > 0 || spec.IndentLeft > 0 || spec.IndentRight > 0 {
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		if spec.FirstLineIndent > 0 {
			pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.FirstLineIndent}
		}
		if spec.IndentLeft > 0 {
			left := int64(spec.IndentLeft)
			pPr.Ind.LeftAttr = &wml.ST_SignedTwipsMeasure{Int64: &left}
		}
		if spec.IndentRight > 0 {
			right := int64(spec.IndentRight)
			pPr.Ind.RightAttr = &wml.ST_SignedTwipsMeasure{Int64: &right}
		}
	}

	if spec.PageBreak {
		pPr.PageBreakBefore = wml.NewCT_OnOff()
	} else {
		pPr.PageBreakBefore = nil
	}
	if spec.KeepWithNext {
		pPr.KeepNext = wml.NewCT_OnOff()
	}
	if spec.KeepLines {
		pPr.KeepLines = wml.NewCT_OnOff()
	}
	if spec.OutlineLevel > 0 {
		pPr.OutlineLvl = wml.NewCT_DecimalNumber()
		level := int64(spec.OutlineLevel - 1)
		pPr.OutlineLvl.ValAttr = level
	}

	for _, run := range para.Runs() {
		if strings.TrimSpace(run.Text()) == "" {
			continue
		}
		rPr := run.X().RPr
		if rPr == nil {
			rPr = wml.NewCT_RPr()
			run.X().RPr = rPr
		}
		applyCompleteFontSpecToRunProperties(rPr, spec, processor)
		if spec.FontSizeHalfPt > 0 {
			rPr.Sz = wml.NewCT_HpsMeasure()
			rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeHalfPt
		}
		if spec.FontSizeCSHalfPt > 0 {
			rPr.SzCs = wml.NewCT_HpsMeasure()
			rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeCSHalfPt
		} else if spec.FontSizeHalfPt > 0 {
			rPr.SzCs = wml.NewCT_HpsMeasure()
			rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeHalfPt
		}
		if spec.Bold {
			rPr.B = wml.NewCT_OnOff()
			rPr.BCs = wml.NewCT_OnOff()
		} else {
			rPr.B = nil
			rPr.BCs = nil
		}
		if spec.Italic {
			rPr.I = wml.NewCT_OnOff()
			rPr.ICs = wml.NewCT_OnOff()
		} else {
			rPr.I = nil
			rPr.ICs = nil
		}
		if spec.Underline {
			rPr.U = wml.NewCT_Underline()
			rPr.U.ValAttr = wml.ST_UnderlineSingle
		} else {
			rPr.U = nil
		}
		if spec.ColorHex != "" {
			rPr.Color = wml.NewCT_Color()
			rPr.Color.ValAttr.ST_HexColorRGB = &spec.ColorHex
		} else {
			// Strict format-only mode should remove user-authored direct colors
			// when the template does not define an explicit font color.
			rPr.Color = nil
		}
	}
}

func sanitizeParagraphFormatSpec(spec ParagraphFormatSpec) ParagraphFormatSpec {
	color := strings.ToUpper(strings.TrimSpace(spec.ColorHex))
	if color != "" && color != "000000" && color != "AUTO" {
		spec.ColorHex = ""
	}
	return spec
}

func extractInlinePrefixRules(templateDoc *document.Document, processor *EnhancedProcessor) []inlinePrefixRule {
	prefixes := []string{"摘要：", "关键词：", "Abstract:", "Abstract：", "Key words:", "Keywords:"}
	rules := make([]inlinePrefixRule, 0, len(prefixes))
	for _, prefix := range prefixes {
		rule, ok := extractInlinePrefixRule(templateDoc, processor, prefix)
		if ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

func extractInlinePrefixRule(templateDoc *document.Document, processor *EnhancedProcessor, prefix string) (inlinePrefixRule, bool) {
	for _, para := range BodyLevelParagraphsOnly(templateDoc) {
		text := strings.TrimSpace(processor.extractParagraphText(para))
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		labelSpec, bodySpec := extractInlineRunSpecs(para, prefix)
		if labelSpec.IsEmpty() {
			labelSpec = sanitizeParagraphFormatSpec(extractParaFormatSpec(para))
		}
		if bodySpec.IsEmpty() {
			bodySpec = labelSpec
		}
		if prefix == "摘要：" && (labelSpec.FontSizeHalfPt == 0 || labelSpec.FontSizeHalfPt <= bodySpec.FontSizeHalfPt) {
			labelSpec.FontEastAsia = "黑体"
			labelSpec.FontAscii = "SimHei"
			labelSpec.FontSizeHalfPt = 30
			labelSpec.FontSizeCSHalfPt = 30
			labelSpec.Bold = true
			bodySpec.FontEastAsia = "宋体"
			bodySpec.FontAscii = "SimSun"
			bodySpec.FontSizeHalfPt = 24
			bodySpec.FontSizeCSHalfPt = 24
			bodySpec.Bold = false
		}
		return inlinePrefixRule{
			Prefix:        prefix,
			ParagraphSpec: extractParagraphLayoutSpec(para),
			LabelSpec:     sanitizeParagraphFormatSpec(labelSpec),
			BodySpec:      sanitizeParagraphFormatSpec(bodySpec),
		}, true
	}
	return inlinePrefixRule{}, false
}

func extractInlineRunSpecs(para document.Paragraph, prefix string) (ParagraphFormatSpec, ParagraphFormatSpec) {
	var labelSpec ParagraphFormatSpec
	var bodySpec ParagraphFormatSpec
	prefixRuneLen := len([]rune(prefix))
	consumed := 0
	prefixReached := false

	for _, run := range para.Runs() {
		runText := run.Text()
		if strings.TrimSpace(runText) == "" {
			continue
		}
		runSpec := extractRunFormatSpec(run)
		if labelSpec.IsEmpty() {
			labelSpec = runSpec
		}
		if prefixReached && bodySpec.IsEmpty() {
			bodySpec = runSpec
			break
		}
		consumed += len([]rune(runText))
		if consumed >= prefixRuneLen {
			prefixReached = true
			if consumed > prefixRuneLen && bodySpec.IsEmpty() {
				bodySpec = runSpec
				break
			}
		}
	}
	return labelSpec, bodySpec
}

func extractParagraphLayoutSpec(para document.Paragraph) ParagraphFormatSpec {
	spec := extractParaFormatSpec(para)
	spec.FontEastAsia = ""
	spec.FontAscii = ""
	spec.FontSizeHalfPt = 0
	spec.FontSizeCSHalfPt = 0
	spec.Bold = false
	spec.Italic = false
	spec.Underline = false
	spec.ColorHex = ""
	return spec
}

func extractRunFormatSpec(run document.Run) ParagraphFormatSpec {
	spec := ParagraphFormatSpec{}
	rPr := run.X().RPr
	if rPr == nil {
		return spec
	}
	if rPr.RFonts != nil {
		spec.FontEastAsia = resolveTemplateEastAsiaFont(
			fontAttrValue(rPr.RFonts.EastAsiaAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.AsciiAttr),
		)
		spec.FontAscii = resolveTemplateAsciiFont(
			fontAttrValue(rPr.RFonts.AsciiAttr),
			fontAttrValue(rPr.RFonts.HAnsiAttr),
			fontAttrValue(rPr.RFonts.CsAttr),
			spec.FontEastAsia,
		)
	}
	if rPr.Sz != nil && rPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeHalfPt = *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber
	}
	if rPr.SzCs != nil && rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber != nil {
		spec.FontSizeCSHalfPt = *rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber
	}
	spec.Bold = rPr.B != nil
	spec.Italic = rPr.I != nil
	spec.Underline = rPr.U != nil
	if rPr.Color != nil && rPr.Color.ValAttr.ST_HexColorRGB != nil {
		spec.ColorHex = *rPr.Color.ValAttr.ST_HexColorRGB
	}
	return completeParagraphFontSpec(spec)
}

func applyInlinePrefixRuleToParagraph(processor *EnhancedProcessor, para document.Paragraph, text string, rules []inlinePrefixRule) bool {
	for _, rule := range rules {
		if !strings.HasPrefix(text, rule.Prefix) {
			continue
		}
		bodyText := strings.TrimPrefix(text, rule.Prefix)
		applyParagraphLayoutSpecToParagraph(para, rule.ParagraphSpec)
		rewriteParagraphRunsWithSpecs(para, []styledRunSegment{
			{Text: rule.Prefix, Spec: rule.LabelSpec},
			{Text: bodyText, Spec: rule.BodySpec},
		}, processor)
		return true
	}
	return false
}

func applyParagraphLayoutSpecToParagraph(para document.Paragraph, spec ParagraphFormatSpec) {
	if para.X().PPr == nil {
		para.X().PPr = wml.NewCT_PPr()
	}
	pPr := para.X().PPr
	if spec.AlignmentSet {
		pPr.Jc = wml.NewCT_Jc()
		pPr.Jc.ValAttr = spec.Alignment
	}
	if spec.LineSpacingVal > 0 || spec.SpaceBefore > 0 || spec.SpaceAfter > 0 {
		if pPr.Spacing == nil {
			pPr.Spacing = wml.NewCT_Spacing()
		}
		if spec.LineSpacingVal > 0 {
			pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{Int64: &spec.LineSpacingVal}
			pPr.Spacing.LineRuleAttr = spec.LineSpacingRule
		}
		if spec.SpaceBefore > 0 {
			pPr.Spacing.BeforeAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.SpaceBefore}
		}
		if spec.SpaceAfter > 0 {
			pPr.Spacing.AfterAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.SpaceAfter}
		}
	}
	if spec.FirstLineIndent > 0 || spec.IndentLeft > 0 || spec.IndentRight > 0 {
		if pPr.Ind == nil {
			pPr.Ind = wml.NewCT_Ind()
		}
		if spec.FirstLineIndent > 0 {
			pPr.Ind.FirstLineAttr = &sharedTypes.ST_TwipsMeasure{ST_UnsignedDecimalNumber: &spec.FirstLineIndent}
		}
		if spec.IndentLeft > 0 {
			left := int64(spec.IndentLeft)
			pPr.Ind.LeftAttr = &wml.ST_SignedTwipsMeasure{Int64: &left}
		}
		if spec.IndentRight > 0 {
			right := int64(spec.IndentRight)
			pPr.Ind.RightAttr = &wml.ST_SignedTwipsMeasure{Int64: &right}
		}
	}
}

func isCoverDateText(text string) bool {
	normalized := stripAllSpaces(text)
	return regexp.MustCompile(`^\d{2,4}年\d{1,2}月(\d{1,2}日)?$`).MatchString(normalized)
}

func normalizeInlinePrefixedParagraphs(userDoc, templateDoc *document.Document, processor *EnhancedProcessor) {
	rules := extractInlinePrefixRules(templateDoc, processor)
	if len(rules) == 0 {
		return
	}
	for _, para := range BodyLevelParagraphsOnly(userDoc) {
		text := strings.TrimSpace(processor.extractParagraphText(para))
		if text == "" {
			continue
		}
		applyInlinePrefixRuleToParagraph(processor, para, text, rules)
	}
}

func isTemplateInstructionText(text string) bool {
	compact := strings.TrimSpace(text)
	if compact == "" {
		return true
	}
	if strings.Contains(compact, "XXXXXXXX") || strings.Contains(compact, "xxxxxx") {
		return true
	}
	for _, marker := range []string{
		"封面格式不要调整",
		"要求：",
		"要求:",
		"填写",
		"删除",
		"即可",
		"格式",
		"字数",
		"用黑体",
		"用宋体",
		"小三号",
		"小四号",
		"段前",
		"段后",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func strictTemplateSampleScore(text string, spec ParagraphFormatSpec) int {
	score := 0
	if spec.ColorHex == "" || strings.EqualFold(spec.ColorHex, "000000") {
		score += 10
	}
	if len([]rune(text)) >= 4 {
		score += 2
	}
	if !strings.Contains(text, "：") && !strings.Contains(text, ":") {
		score++
	}
	return score
}

func applyTemplateTOCFormatting(userDoc, templateDoc *document.Document, processor *EnhancedProcessor) {
	templateTitle, ok := findTOCTitleParagraph(templateDoc, processor)
	if ok {
		userTitle, found := findTOCTitleParagraph(userDoc, processor)
		if found {
			cloneParagraphFormatting(userTitle, templateTitle)
			spec := sanitizeParagraphFormatSpec(extractParaFormatSpec(templateTitle))
			applyStrictSpecToParagraph(processor, userTitle, spec)
		}
	}

	templateEntry, ok := findFirstTOCEntryParagraph(templateDoc, processor)
	if !ok {
		return
	}
	entrySpec := sanitizeParagraphFormatSpec(extractParaFormatSpec(templateEntry))
	for _, para := range BodyLevelParagraphsOnly(userDoc) {
		if !isTOCEntryParagraph(para, processor) {
			continue
		}
		cloneParagraphFormatting(para, templateEntry)
		applyStrictSpecToParagraph(processor, para, entrySpec)
	}
}

func findTOCTitleParagraph(doc *document.Document, processor *EnhancedProcessor) (document.Paragraph, bool) {
	for _, para := range BodyLevelParagraphsOnly(doc) {
		text := normalizeVisibleText(processor.extractParagraphText(para))
		if text == "目录" || text == "目 录" {
			return para, true
		}
	}
	return document.Paragraph{}, false
}

func findFirstTOCEntryParagraph(doc *document.Document, processor *EnhancedProcessor) (document.Paragraph, bool) {
	for _, para := range BodyLevelParagraphsOnly(doc) {
		if isTOCEntryParagraph(para, processor) {
			return para, true
		}
	}
	return document.Paragraph{}, false
}

func isTOCEntryParagraph(para document.Paragraph, processor *EnhancedProcessor) bool {
	text := strings.TrimSpace(processor.extractParagraphText(para))
	if text == "" {
		return false
	}
	if para.X().PPr != nil && para.X().PPr.PStyle != nil {
		style := strings.ToUpper(strings.TrimSpace(para.X().PPr.PStyle.ValAttr))
		if strings.HasPrefix(style, "TOC") {
			return true
		}
	}
	return strings.Contains(text, "\t") && regexp.MustCompile(`\d+\s*$`).MatchString(text)
}

func applyCoverTableFormatting(userDoc, templateDoc *document.Document, processor *EnhancedProcessor) {
	userTables := userDoc.Tables()
	_ = templateDoc

	if len(userTables) > 0 {
		formatCoverTitleTable(userTables[0], processor)
	}
	if len(userTables) > 1 {
		formatCoverInfoTable(userTables[1], processor)
	}
}

func copyTableLayoutFromTemplate(userTable, templateTable document.Table) {
	if userTable.X() == nil || templateTable.X() == nil {
		return
	}
	userTable.X().TblPr = cloneTblPr(templateTable.X().TblPr)
	userTable.X().TblGrid = cloneTblGrid(templateTable.X().TblGrid)

	userRows := userTable.Rows()
	templateRows := templateTable.Rows()
	for rowIdx := 0; rowIdx < minInt(len(userRows), len(templateRows)); rowIdx++ {
		userRows[rowIdx].X().TrPr = cloneTrPr(templateRows[rowIdx].X().TrPr)
		userCells := userRows[rowIdx].Cells()
		templateCells := templateRows[rowIdx].Cells()
		for cellIdx := 0; cellIdx < minInt(len(userCells), len(templateCells)); cellIdx++ {
			userCells[cellIdx].X().TcPr = cloneTcPr(templateCells[cellIdx].X().TcPr)
			cloneCellParagraphFormatting(userCells[cellIdx], templateCells[cellIdx])
		}
	}
}

func cloneCellParagraphFormatting(userCell, templateCell document.Cell) {
	templateParagraphs := templateCell.Paragraphs()
	if len(templateParagraphs) == 0 {
		return
	}
	templatePara := templateParagraphs[0]
	for _, para := range templateParagraphs {
		if strings.TrimSpace(paragraphVisibleText(para)) != "" {
			templatePara = para
			break
		}
	}
	for _, para := range userCell.Paragraphs() {
		cloneParagraphFormatting(para, templatePara)
	}
}

func formatCoverTitleTable(table document.Table, processor *EnhancedProcessor) {
	titleSpec := ParagraphFormatSpec{
		FontEastAsia:     "黑体",
		FontAscii:        "SimHei",
		FontSizeHalfPt:   44,
		FontSizeCSHalfPt: 44,
		Bold:             true,
		AlignmentSet:     true,
		Alignment:        wml.ST_JcCenter,
		LineSpacingVal:   360,
		LineSpacingRule:  wml.ST_LineSpacingRuleAuto,
		ColorHex:         "",
		Underline:        false,
		FirstLineIndent:  0,
		IndentLeft:       0,
		IndentRight:      0,
	}

	for _, row := range table.Rows() {
		cells := row.Cells()
		for idx, cell := range cells {
			for _, para := range cell.Paragraphs() {
				if strings.TrimSpace(processor.extractParagraphText(para)) == "" {
					continue
				}
				if idx == 0 {
					cloneParagraphFormattingToAlignment(para, wml.ST_JcCenter)
				} else {
					cloneParagraphFormattingToAlignment(para, wml.ST_JcCenter)
					applyStrictSpecToParagraph(processor, para, titleSpec)
				}
			}
		}
	}
}

func formatCoverInfoTable(table document.Table, processor *EnhancedProcessor) {
	infoSpec := ParagraphFormatSpec{
		FontEastAsia:     "宋体",
		FontAscii:        "SimSun",
		FontSizeHalfPt:   28,
		FontSizeCSHalfPt: 28,
		Bold:             false,
		AlignmentSet:     true,
		Alignment:        wml.ST_JcLeft,
		LineSpacingVal:   360,
		LineSpacingRule:  wml.ST_LineSpacingRuleAuto,
		ColorHex:         "",
		Underline:        false,
		FirstLineIndent:  0,
		IndentLeft:       0,
		IndentRight:      0,
	}

	for _, row := range table.Rows() {
		for _, cell := range row.Cells() {
			for _, para := range cell.Paragraphs() {
				if strings.TrimSpace(processor.extractParagraphText(para)) == "" {
					continue
				}
				cloneParagraphFormattingToAlignment(para, wml.ST_JcLeft)
				applyStrictSpecToParagraph(processor, para, infoSpec)
			}
		}
	}
}

func normalizeCoverDateFormatting(userDoc, templateDoc *document.Document, processor *EnhancedProcessor) {
	userDate, ok := findCoverDateParagraph(userDoc, processor)
	if !ok {
		return
	}
	templateDate, hasTemplateDate := findCoverDateParagraph(templateDoc, processor)
	if hasTemplateDate {
		cloneParagraphFormatting(userDate, templateDate)
	}
	dateText := normalizeCoverDateText(processor.extractParagraphText(userDate))
	if dateText != "" {
		replaceParagraphText(userDate, dateText)
	}
	var dateSpec ParagraphFormatSpec
	if hasTemplateDate {
		dateSpec = sanitizeParagraphFormatSpec(extractParaFormatSpec(templateDate))
	}
	if dateSpec.IsEmpty() {
		dateSpec = ParagraphFormatSpec{
			FontEastAsia:     "宋体",
			FontAscii:        "SimSun",
			FontSizeHalfPt:   32,
			FontSizeCSHalfPt: 32,
			Bold:             true,
			AlignmentSet:     true,
			Alignment:        wml.ST_JcCenter,
			ColorHex:         "",
		}
	}
	applyStrictSpecToParagraph(processor, userDate, dateSpec)
}

func findCoverDateParagraph(doc *document.Document, processor *EnhancedProcessor) (document.Paragraph, bool) {
	for _, para := range BodyLevelParagraphsOnly(doc) {
		text := strings.TrimSpace(processor.extractParagraphText(para))
		if text == "" {
			continue
		}
		if strings.Contains(text, "摘要") {
			break
		}
		if isCoverDateText(text) {
			return para, true
		}
	}
	return document.Paragraph{}, false
}

func normalizeCoverDateText(text string) string {
	compact := strings.Join(strings.Fields(text), "")
	re := regexp.MustCompile(`(\d{2,4})年(\d{1,2})月(?:([0-9]{1,2})日?)?`)
	match := re.FindStringSubmatch(compact)
	if len(match) == 0 {
		return ""
	}
	if len(match) > 3 && match[3] != "" {
		return fmt.Sprintf("%s年%s月%s日", match[1], match[2], match[3])
	}
	return fmt.Sprintf("%s年%s月", match[1], match[2])
}

func replaceParagraphText(para document.Paragraph, text string) {
	runs := para.Runs()
	if len(runs) == 0 {
		para.AddRun().AddText(text)
		return
	}
	written := false
	for _, run := range runs {
		if !written {
			run.ClearContent()
			run.AddText(text)
			written = true
			continue
		}
		run.ClearContent()
	}
}

type styledRunSegment struct {
	Text string
	Spec ParagraphFormatSpec
}

func rewriteParagraphRunsWithSpecs(para document.Paragraph, segments []styledRunSegment, processor *EnhancedProcessor) {
	existingRuns := para.Runs()
	neededRuns := len(segments)
	for len(existingRuns) < neededRuns {
		para.AddRun()
		existingRuns = para.Runs()
	}
	for idx, segment := range segments {
		run := existingRuns[idx]
		run.ClearContent()
		if segment.Text != "" {
			run.AddText(segment.Text)
		}
		applyRunSpecToRun(run, segment.Spec, processor)
	}
	for idx := neededRuns; idx < len(existingRuns); idx++ {
		existingRuns[idx].ClearContent()
	}
}

func applyRunSpecToRun(run document.Run, spec ParagraphFormatSpec, processor *EnhancedProcessor) {
	rPr := run.X().RPr
	if rPr == nil {
		rPr = wml.NewCT_RPr()
		run.X().RPr = rPr
	}
	applyCompleteFontSpecToRunProperties(rPr, spec, processor)
	if spec.FontSizeHalfPt > 0 {
		rPr.Sz = wml.NewCT_HpsMeasure()
		rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeHalfPt
	}
	if spec.FontSizeCSHalfPt > 0 {
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeCSHalfPt
	} else if spec.FontSizeHalfPt > 0 {
		rPr.SzCs = wml.NewCT_HpsMeasure()
		rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &spec.FontSizeHalfPt
	}
	if spec.Bold {
		rPr.B = wml.NewCT_OnOff()
		rPr.BCs = wml.NewCT_OnOff()
	} else {
		rPr.B = nil
		rPr.BCs = nil
	}
	if spec.Italic {
		rPr.I = wml.NewCT_OnOff()
		rPr.ICs = wml.NewCT_OnOff()
	} else {
		rPr.I = nil
		rPr.ICs = nil
	}
	if spec.Underline {
		rPr.U = wml.NewCT_Underline()
		rPr.U.ValAttr = wml.ST_UnderlineSingle
	} else {
		rPr.U = nil
	}
	if spec.ColorHex != "" {
		rPr.Color = wml.NewCT_Color()
		rPr.Color.ValAttr.ST_HexColorRGB = &spec.ColorHex
	} else {
		rPr.Color = nil
	}
}

func applyCompleteFontSpecToRunProperties(rPr *wml.CT_RPr, spec ParagraphFormatSpec, processor *EnhancedProcessor) {
	spec = completeParagraphFontSpec(spec)
	if spec.FontEastAsia == "" && spec.FontAscii == "" {
		return
	}

	rFonts := wml.NewCT_Fonts()
	if spec.FontEastAsia != "" {
		rFonts.EastAsiaAttr = processor.getCachedFontName(spec.FontEastAsia)
	}
	if spec.FontAscii != "" {
		ascii := processor.getCachedFontName(spec.FontAscii)
		rFonts.AsciiAttr = ascii
		rFonts.HAnsiAttr = ascii
		rFonts.CsAttr = ascii
	}
	rPr.RFonts = rFonts
}

func cloneParagraphFormatting(dst, src document.Paragraph) {
	if src.X() == nil || src.X().PPr == nil {
		return
	}
	dst.X().PPr = clonePPr(src.X().PPr)
}

func paragraphVisibleText(para document.Paragraph) string {
	var builder strings.Builder
	for _, run := range para.Runs() {
		builder.WriteString(run.Text())
	}
	return builder.String()
}

func strictParagraphsBeforeSectionBoundary(refs []strictParagraphRef, processor *EnhancedProcessor) []strictParagraphRef {
	for idx, ref := range refs {
		text := strings.TrimSpace(processor.extractParagraphText(ref.Para))
		if text == "" {
			continue
		}
		normalized := stripAllSpaces(normalizeVisibleText(text))
		if isReferencesTitleText(normalized) || isAcknowledgementsTitleText(normalized) || isAppendixTitleKW(normalized) {
			return refs[:idx]
		}
	}
	return refs
}

func cloneParagraphFormattingToAlignment(para document.Paragraph, align wml.ST_Jc) {
	if para.X().PPr == nil {
		para.X().PPr = wml.NewCT_PPr()
	}
	para.X().PPr.Ind = nil
	para.X().PPr.Spacing = nil
	para.X().PPr.Jc = wml.NewCT_Jc()
	para.X().PPr.Jc.ValAttr = align
}

func cloneTblPr(src *wml.CT_TblPr) *wml.CT_TblPr {
	if src == nil {
		return nil
	}
	cloned := wml.NewCT_TblPr()
	raw, err := xml.Marshal(src)
	if err != nil {
		return src
	}
	if xml.Unmarshal(raw, cloned) != nil {
		return src
	}
	return cloned
}

func cloneTblGrid(src *wml.CT_TblGrid) *wml.CT_TblGrid {
	if src == nil {
		return nil
	}
	cloned := wml.NewCT_TblGrid()
	raw, err := xml.Marshal(src)
	if err != nil {
		return src
	}
	if xml.Unmarshal(raw, cloned) != nil {
		return src
	}
	return cloned
}

func cloneTrPr(src *wml.CT_TrPr) *wml.CT_TrPr {
	if src == nil {
		return nil
	}
	cloned := wml.NewCT_TrPr()
	raw, err := xml.Marshal(src)
	if err != nil {
		return src
	}
	if xml.Unmarshal(raw, cloned) != nil {
		return src
	}
	return cloned
}

func cloneTcPr(src *wml.CT_TcPr) *wml.CT_TcPr {
	if src == nil {
		return nil
	}
	cloned := wml.NewCT_TcPr()
	raw, err := xml.Marshal(src)
	if err != nil {
		return src
	}
	if xml.Unmarshal(raw, cloned) != nil {
		return src
	}
	return cloned
}

func restoreStrictCoverTablesFromTemplate(templatePath, outputPath string) error {
	templateEntries, err := readDocxEntries(templatePath)
	if err != nil {
		return err
	}
	outputEntries, err := readDocxEntries(outputPath)
	if err != nil {
		return err
	}

	templateDocXML, ok := templateEntries["word/document.xml"]
	if !ok {
		return fmt.Errorf("template missing word/document.xml")
	}
	outputDocXML, ok := outputEntries["word/document.xml"]
	if !ok {
		return fmt.Errorf("output missing word/document.xml")
	}

	templateBodyXML := extractDocxBodyXML(string(templateDocXML))
	outputBodyXML := extractDocxBodyXML(string(outputDocXML))
	templateTables := extractDocxElements(templateBodyXML, "w:tbl")
	outputTables := extractDocxElements(outputBodyXML, "w:tbl")
	if len(templateTables) < 2 || len(outputTables) < 2 {
		return nil
	}

	for idx := 0; idx < 2; idx++ {
		restoredTableXML, err := replaceDocxTableCellTextGrid(templateTables[idx], extractDocxTableCellTextGrid(outputTables[idx]))
		if err != nil {
			return fmt.Errorf("restore cover table %d: %w", idx, err)
		}
		outputBodyXML, err = replaceNthDocxTable(outputBodyXML, idx, restoredTableXML)
		if err != nil {
			return fmt.Errorf("replace cover table %d: %w", idx, err)
		}
	}

	outputEntries["word/document.xml"] = []byte(replaceDocxBodyXML(string(outputDocXML), outputBodyXML))
	return writeDocxEntries(outputPath, outputEntries)
}

func replaceNthDocxTable(xmlText string, occurrence int, replacement string) (string, error) {
	pattern := regexp.MustCompile(`(?s)<w:tbl\b[^>]*>.*?</w:tbl>`)
	matches := pattern.FindAllStringIndex(xmlText, -1)
	if occurrence < 0 || occurrence >= len(matches) {
		return "", fmt.Errorf("missing w:tbl occurrence %d", occurrence)
	}
	start, end := matches[occurrence][0], matches[occurrence][1]
	return xmlText[:start] + replacement + xmlText[end:], nil
}

func normalizeVisibleText(text string) string {
	compact := strings.Join(strings.Fields(text), " ")
	compact = strings.ReplaceAll(compact, "　　　", " ")
	compact = strings.ReplaceAll(compact, "　", " ")
	return strings.TrimSpace(compact)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type strictRelationshipSet struct {
	XMLName       xml.Name                 `xml:"Relationships"`
	Xmlns         string                   `xml:"xmlns,attr,omitempty"`
	Relationships []strictRelationshipPart `xml:"Relationship"`
}

type strictRelationshipPart struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr,omitempty"`
}

func copyTemplateHeaderFooterPackage(templatePath, outputPath string) error {
	templateEntries, err := readDocxEntries(templatePath)
	if err != nil {
		return err
	}
	outputEntries, err := readDocxEntries(outputPath)
	if err != nil {
		return err
	}

	copyMatchingEntries(outputEntries, templateEntries, func(name string) bool {
		return strings.HasPrefix(name, "word/header") ||
			strings.HasPrefix(name, "word/footer") ||
			strings.HasPrefix(name, "word/_rels/header") ||
			strings.HasPrefix(name, "word/_rels/footer") ||
			strings.HasPrefix(name, "word/media/")
	})

	mergeContentTypes(outputEntries, templateEntries)
	mergeDocumentRelationships(outputEntries, templateEntries)
	mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries)

	return writeDocxEntries(outputPath, outputEntries)
}

func postProcessStrictOutput(outputPath string) error {
	entries, err := readDocxEntries(outputPath)
	if err != nil {
		return err
	}

	if documentXML, ok := entries["word/document.xml"]; ok {
		xmlText := sanitizeStrictDocumentXML(string(documentXML))
		xmlText = strings.ReplaceAll(xmlText, `w:color w:val="FF0000"`, `w:color w:val="000000"`)
		xmlText = strings.ReplaceAll(xmlText, `w:color w:val="ff0000"`, `w:color w:val="000000"`)
		entries["word/document.xml"] = []byte(xmlText)
	}

	return writeDocxEntries(outputPath, entries)
}

var strictFallbackStartTagPattern = regexp.MustCompile(`<mc:Fallback\b[^>]*>`)
var strictXMLAttributePattern = regexp.MustCompile(`\s+([A-Za-z_][A-Za-z0-9_.:-]*)\s*=\s*("([^"]*)"|'([^']*)')`)

func sanitizeStrictDocumentXML(xmlText string) string {
	return strictFallbackStartTagPattern.ReplaceAllStringFunc(xmlText, deduplicateStrictXMLStartTagAttributes)
}

func deduplicateStrictXMLStartTagAttributes(tag string) string {
	if !strings.HasPrefix(tag, "<") || strings.HasPrefix(tag, "</") || strings.HasPrefix(tag, "<?") || strings.HasPrefix(tag, "<!") {
		return tag
	}

	selfClosing := strings.HasSuffix(tag, "/>")
	closeToken := ">"
	if selfClosing {
		closeToken = "/>"
	}

	inner := strings.TrimSuffix(strings.TrimPrefix(tag, "<"), closeToken)
	nameEnd := len(inner)
	for i, r := range inner {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || r == '/' {
			nameEnd = i
			break
		}
	}
	if nameEnd == len(inner) {
		return tag
	}

	var b strings.Builder
	b.WriteString("<")
	b.WriteString(inner[:nameEnd])

	seen := make(map[string]struct{})
	for _, match := range strictXMLAttributePattern.FindAllStringSubmatch(inner[nameEnd:], -1) {
		if len(match) < 3 {
			continue
		}
		attrName := match[1]
		if _, exists := seen[attrName]; exists {
			continue
		}
		seen[attrName] = struct{}{}
		b.WriteString(match[0])
	}

	if selfClosing {
		b.WriteString("/>")
	} else {
		b.WriteString(">")
	}
	return b.String()
}

func readDocxEntries(path string) (map[string][]byte, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", path, err)
	}
	defer reader.Close()

	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %q: %w", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %q: %w", file.Name, err)
		}
		entries[file.Name] = content
	}
	return entries, nil
}

func writeDocxEntries(path string, entries map[string][]byte) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create zip %q: %w", path, err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		w, err := writer.Create(name)
		if err != nil {
			writer.Close()
			return fmt.Errorf("create zip entry %q: %w", name, err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			writer.Close()
			return fmt.Errorf("write zip entry %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close zip writer: %w", err)
	}
	return nil
}

func copyMatchingEntries(dst, src map[string][]byte, match func(string) bool) {
	for name, content := range src {
		if match(name) {
			dst[name] = append([]byte(nil), content...)
		}
	}
}

func mergeContentTypes(outputEntries, templateEntries map[string][]byte) {
	outputContent, ok := outputEntries["[Content_Types].xml"]
	if !ok {
		return
	}
	templateContent, ok := templateEntries["[Content_Types].xml"]
	if !ok {
		return
	}

	outputXML := string(outputContent)
	templateXML := string(templateContent)
	for _, needle := range extractSelfClosingTags(templateXML, "<Default ", "/>") {
		if !strings.Contains(outputXML, needle) {
			outputXML = strings.Replace(outputXML, "</Types>", needle+"</Types>", 1)
		}
	}
	for _, needle := range extractSelfClosingTags(templateXML, "<Override ", "/>") {
		if strings.Contains(needle, "/word/header") || strings.Contains(needle, "/word/footer") {
			if !strings.Contains(outputXML, needle) {
				outputXML = strings.Replace(outputXML, "</Types>", needle+"</Types>", 1)
			}
		}
	}
	outputEntries["[Content_Types].xml"] = []byte(outputXML)
}

func mergeDocumentRelationships(outputEntries, templateEntries map[string][]byte) {
	outputContent, ok := outputEntries["word/_rels/document.xml.rels"]
	if !ok {
		return
	}
	templateContent, ok := templateEntries["word/_rels/document.xml.rels"]
	if !ok {
		return
	}

	var outputRels strictRelationshipSet
	var templateRels strictRelationshipSet
	if xml.Unmarshal(outputContent, &outputRels) != nil || xml.Unmarshal(templateContent, &templateRels) != nil {
		return
	}

	filtered := make([]strictRelationshipPart, 0, len(outputRels.Relationships))
	for _, rel := range outputRels.Relationships {
		if isHeaderFooterRelationship(rel) {
			continue
		}
		filtered = append(filtered, rel)
	}
	for _, rel := range templateRels.Relationships {
		if isHeaderFooterRelationship(rel) {
			filtered = append(filtered, rel)
		}
	}
	outputRels.Relationships = filtered

	merged, err := xml.Marshal(outputRels)
	if err != nil {
		return
	}
	outputEntries["word/_rels/document.xml.rels"] = merged
}

func isHeaderFooterRelationship(rel strictRelationshipPart) bool {
	return strings.Contains(rel.Type, "/header") ||
		strings.Contains(rel.Type, "/footer") ||
		strings.HasPrefix(rel.Target, "header") ||
		strings.HasPrefix(rel.Target, "footer")
}

func mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries map[string][]byte) {
	outputContent, ok := outputEntries["word/document.xml"]
	if !ok {
		return
	}
	templateContent, ok := templateEntries["word/document.xml"]
	if !ok {
		return
	}

	outputXML := string(outputContent)
	templateXML := string(templateContent)

	templateSectPrBlocks := extractSectPrBlocks(templateXML)
	if len(templateSectPrBlocks) == 0 {
		return
	}

	sectPrBlocks := extractSectPrBlocks(outputXML)
	for idx, sectPr := range sectPrBlocks {
		templateIdx := idx
		if templateIdx >= len(templateSectPrBlocks) {
			templateIdx = len(templateSectPrBlocks) - 1
		}
		replacement := templateSectPrBlocks[templateIdx]
		outputXML = strings.Replace(outputXML, sectPr, replacement, 1)
	}
	outputEntries["word/document.xml"] = []byte(outputXML)
}

func lastSectPrBlock(docXML string) string {
	blocks := extractSectPrBlocks(docXML)
	if len(blocks) == 0 {
		return ""
	}
	return blocks[len(blocks)-1]
}

func extractSectPrBlocks(docXML string) []string {
	re := regexp.MustCompile(`<w:sectPr(?:[\s\S]*?</w:sectPr>|[^>]*/>)`)
	return re.FindAllString(docXML, -1)
}

func extractHeaderFooterReferenceTags(sectPrXML string) []string {
	var tags []string
	tags = append(tags, extractSelfClosingTags(sectPrXML, "<w:headerReference", "/>")...)
	tags = append(tags, extractSelfClosingTags(sectPrXML, "<w:footerReference", "/>")...)
	return tags
}

func collectTemplateSectionHeaderFooterReferenceTags(templateXML string) [][]string {
	sectPrBlocks := extractSectPrBlocks(templateXML)
	sections := make([][]string, 0, len(sectPrBlocks))
	for _, sectPr := range sectPrBlocks {
		selected := make(map[string]string)
		for _, tag := range extractHeaderFooterReferenceTags(sectPr) {
			key := headerFooterReferenceKey(tag)
			if key == "" {
				continue
			}
			selected[key] = tag
		}
		sections = append(sections, orderedHeaderFooterReferenceTags(selected))
	}
	return sections
}

func collectTemplateHeaderFooterReferenceTags(templateXML string) []string {
	selected := make(map[string]string)
	for _, sectPr := range extractSectPrBlocks(templateXML) {
		for _, tag := range extractHeaderFooterReferenceTags(sectPr) {
			key := headerFooterReferenceKey(tag)
			if key == "" {
				continue
			}
			selected[key] = tag
		}
	}
	return orderedHeaderFooterReferenceTags(selected)
}

func orderedHeaderFooterReferenceTags(selected map[string]string) []string {
	orderedKeys := []string{
		"header:default",
		"header:first",
		"header:even",
		"footer:default",
		"footer:first",
		"footer:even",
	}
	refs := make([]string, 0, len(selected))
	for _, key := range orderedKeys {
		if tag, ok := selected[key]; ok {
			refs = append(refs, tag)
		}
	}
	return refs
}

func insertHeaderFooterRefsIntoSectPr(sectPrXML string, refs []string) (string, bool) {
	if strings.HasSuffix(strings.TrimSpace(sectPrXML), "/>") {
		if len(refs) == 0 {
			return sectPrXML, true
		}
		return strings.TrimSuffix(sectPrXML, "/>") + ">" + strings.Join(refs, "") + "</w:sectPr>", true
	}
	insertAt := strings.Index(sectPrXML, ">")
	if insertAt == -1 {
		return "", false
	}
	return sectPrXML[:insertAt+1] + strings.Join(refs, "") + sectPrXML[insertAt+1:], true
}

func headerFooterReferenceKey(tag string) string {
	kind := ""
	switch {
	case strings.Contains(tag, "<w:headerReference"):
		kind = "header"
	case strings.Contains(tag, "<w:footerReference"):
		kind = "footer"
	default:
		return ""
	}

	refType := "default"
	if match := regexp.MustCompile(`w:type="([^"]+)"`).FindStringSubmatch(tag); len(match) > 1 {
		refType = match[1]
	}
	return kind + ":" + refType
}

func stripHeaderFooterReferenceTags(sectPrXML string) string {
	for _, tag := range extractHeaderFooterReferenceTags(sectPrXML) {
		sectPrXML = strings.ReplaceAll(sectPrXML, tag, "")
	}
	return sectPrXML
}

func extractSelfClosingTags(xmlText, prefix, suffix string) []string {
	var tags []string
	searchStart := 0
	for {
		start := strings.Index(xmlText[searchStart:], prefix)
		if start == -1 {
			return tags
		}
		start += searchStart
		end := strings.Index(xmlText[start:], suffix)
		if end == -1 {
			return tags
		}
		end += start + len(suffix)
		tags = append(tags, xmlText[start:end])
		searchStart = end
	}
}
