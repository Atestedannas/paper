package fileprocessor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"

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

	// 节点3c：规则移植路径 — applyStrictRuleFallbacks 后的完整规则
	DiagPrintf(" ====== 节点3c: 规则移植路径 — applyStrictRuleFallbacks ======")
	DiagPrintf(" [移植] 封面标题值：%s", formatSpecCompact(rules.CoverTitleValue))
	DiagPrintf(" [移植] 封面标题标签：%s", formatSpecCompact(rules.CoverTitleLabel))
	DiagPrintf(" [移植] 封面信息值：%s", formatSpecCompact(rules.CoverInfoValue))
	DiagPrintf(" [移植] 封面信息标签：%s", formatSpecCompact(rules.CoverInfoLabel))
	for kind, spec := range rules.Paragraph {
		DiagPrintf(" [移植] 段落[%s]：%s", kind, formatSpecCompact(spec))
	}
	for kind, block := range rules.Inline {
		DiagPrintf(" [移植] 行内[%s]：标签=%s 正文=%s",
			kind, formatSpecCompact(block.LabelSpec), formatSpecCompact(block.BodySpec))
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
	return regexp.MustCompile(`[．.…]{2,}`).MatchString(normalized) ||
		isHeading1Text(normalized) ||
		isHeading2Text(normalized) ||
		isHeading3Text(normalized) ||
		strings.Contains(normalized, "参考文献") ||
		strings.Contains(normalized, "致谢")
}

func isHeading1Text(text string) bool {
	normalized := normalizeVisibleText(text)
	if len([]rune(normalized)) == 0 || len([]rune(normalized)) > 40 {
		return false
	}
	return isHeading1(normalizeSpaces(normalized))
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
	paragraphs := BodyLevelParagraphsOnly(templateDoc)
	sampleStart := 0
	sampleEnd := len(paragraphs)
	foundSample := false
	for index, paragraph := range paragraphs {
		text := normalizeVisibleText(processor.extractParagraphText(paragraph))
		if regexp.MustCompile(`^\d+\s*[-－]\s*\d+.*范本$`).MatchString(text) {
			if !foundSample {
				sampleStart = index + 1
				foundSample = true
			} else {
				sampleEnd = index
				break
			}
		}
	}
	paragraphs = paragraphs[sampleStart:sampleEnd]
	classified := NewV2DeterministicClassifier(processor).Classify(paragraphs)
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

	// 范本文本可能沿用 TOC 样式，状态分类器会把真实标题继续留在目录区。
	// 对标题和正文按文本结构直接取首个真实样本，避免目录样式污染模板规则。
	seenHeading := false
	directPicked := make(map[string]bool)
	for _, paragraph := range paragraphs {
		text := normalizeVisibleText(processor.extractParagraphText(paragraph))
		if text == "" || isTemplateInstructionText(text) || isTOCEntryLikeText(text) {
			continue
		}
		paraType := ""
		switch {
		case isHeading3Text(text):
			paraType = V2Heading3
			seenHeading = true
		case isHeading2Text(text):
			paraType = V2Heading2
			seenHeading = true
		case isHeading1Text(text):
			paraType = V2Heading1
			seenHeading = true
		case seenHeading && isDirectBodySampleText(text):
			paraType = V2Body
		}
		if paraType == "" {
			continue
		}
		if directPicked[paraType] {
			continue
		}
		spec := sanitizeParagraphFormatSpec(extractParaFormatSpec(paragraph))
		if !spec.IsEmpty() {
			specs[paraType] = spec
			directPicked[paraType] = true
		}
	}
	return specs
}

func extractInstructionTemplateSpecs(templateDoc *document.Document, processor *EnhancedProcessor) map[string]ParagraphFormatSpec {
	specs := make(map[string]ParagraphFormatSpec)
	for _, paragraph := range BodyLevelParagraphsOnly(templateDoc) {
		text := normalizeVisibleText(processor.extractParagraphText(paragraph))
		if regexp.MustCompile(`^\d+\s*[-－]\s*\d+.*范本$`).MatchString(text) {
			break
		}
		spec := instructionParagraphFormatSpec(text)
		switch {
		case strings.Contains(text, "字体与间距"):
			specs[V2Body] = spec
		case strings.Contains(text, "中文摘要") && strings.Contains(text, "用"):
			specs[V2Abstract] = spec
		case strings.Contains(text, "目次页") && strings.Contains(text, "字体用"):
			specs[V2TOC] = spec
			specs["toc_entry"] = spec
		case strings.Contains(text, "参考文献") && strings.Contains(text, "用") && strings.Contains(text, "编写"):
			specs[V2References] = spec
		case strings.Contains(text, "表序、表题") && strings.Contains(text, "字体均为"):
			specs[V2TableCaption] = spec
			specs["caption"] = spec
		case strings.Contains(text, "图序、图题") && strings.Contains(text, "字体均为"):
			specs[V2FigureCaption] = spec
			specs["caption"] = spec
		case strings.Contains(text, "页眉字号为"):
			specs["header"] = spec
		}
	}
	return specs
}

func instructionParagraphFormatSpec(text string) ParagraphFormatSpec {
	spec := ParagraphFormatSpec{}
	for _, font := range []string{"Times New Roman", "仿宋", "楷体", "黑体", "宋体"} {
		if strings.Contains(text, font) {
			if font == "Times New Roman" {
				spec.FontAscii = font
			} else {
				spec.FontEastAsia = font
			}
			break
		}
	}
	for _, size := range []struct {
		name   string
		halfPt uint64
	}{
		{"小四号", 24}, {"小四", 24},
		{"小五号", 18}, {"小五", 18},
		{"五号", 21}, {"5号", 21},
	} {
		if strings.Contains(text, size.name) {
			spec.FontSizeHalfPt = size.halfPt
			spec.FontSizeCSHalfPt = size.halfPt
			break
		}
	}
	if match := regexp.MustCompile(`固定值\s*(\d+(?:\.\d+)?)\s*磅`).FindStringSubmatch(text); len(match) == 2 {
		if points, err := strconv.ParseFloat(match[1], 64); err == nil {
			spec.LineSpacingVal = int64(math.Round(points * 20))
			spec.LineSpacingRule = wml.ST_LineSpacingRuleExact
		}
	}
	return spec
}

func isDirectBodySampleText(text string) bool {
	normalized := normalizeVisibleText(text)
	if len([]rune(normalized)) < 10 {
		return false
	}
	return !isHeading1Text(normalized) &&
		!isHeading2Text(normalized) &&
		!isHeading3Text(normalized) &&
		!isReferenceItemText(normalized) &&
		!isReferencesTitleText(normalized) &&
		!isAcknowledgementsTitleText(normalized) &&
		!isMainTextTableCaption(normalized) &&
		!isMainTextFigureCaption(normalized)
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
	// A1: 封面"题目"表格仅做版面 schema 修复，不覆写内容字号/字体/下划线：
	//  ① 统一各行行高（以首行 trHeight 为基准，其余行对齐到同一值，避免 841/668 错行）；
	//  ② 每个单元格补齐垂直居中（vAlign=center，与首行一致）；
	//  ③ 空占位单元格中超大字号空 run 的"垫高"压缩到小四（24 半磅），
	//     消除 row1 左占位 cell 被 36pt 空 run 撑高导致的"题目"标签错行。
	// 最小侵入：不再套用 titleSpec（黑体22pt无下划线会覆写学生宋体24加粗下划线），
	// 题目内容行的对齐与下划线保留学生形态（视觉横线 u=single 对齐模板 tcBorders）。
	if table.X() == nil {
		return
	}
	rows := table.Rows()
	if len(rows) == 0 {
		return
	}
	var targetHeight uint64
	targetHeightSet := false
	for _, row := range rows {
		trPr := row.X().TrPr
		if trPr == nil || len(trPr.TrHeight) == 0 || trPr.TrHeight[0].ValAttr == nil ||
			trPr.TrHeight[0].ValAttr.ST_UnsignedDecimalNumber == nil {
			continue
		}
		if !targetHeightSet {
			targetHeight = *trPr.TrHeight[0].ValAttr.ST_UnsignedDecimalNumber
			targetHeightSet = true
			continue
		}
		if *trPr.TrHeight[0].ValAttr.ST_UnsignedDecimalNumber != targetHeight {
			trPr.TrHeight[0].ValAttr.ST_UnsignedDecimalNumber = &targetHeight
		}
	}
	for _, row := range rows {
		for _, cell := range row.Cells() {
			tcPr := cell.X().TcPr
			if tcPr == nil {
				tcPr = wml.NewCT_TcPr()
				cell.X().TcPr = tcPr
			}
			if tcPr.VAlign == nil {
				tcPr.VAlign = wml.NewCT_VerticalJc()
			}
			tcPr.VAlign.ValAttr = wml.ST_VerticalJcCenter
			for _, para := range cell.Paragraphs() {
				if strings.TrimSpace(processor.extractParagraphText(para)) != "" {
					continue
				}
				// 段落标记 rPr（<w:pPr><w:rPr><w:sz>）的超大字号同样会撑高空
				// 占位 cell——unioffice 的 Runs() 不包含段落标记，故单独压缩。
				if pp := para.X().PPr; pp != nil && pp.RPr != nil && pp.RPr.Sz != nil {
					if pp.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber == nil && pp.RPr.Sz.ValAttr.ST_PositiveUniversalMeasure == nil {
						// 无有效字号值，无需处理
					} else if pp.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil &&
						*pp.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber > 24 {
						twentyFour := uint64(24)
						pp.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &twentyFour
					}
				}
				for _, r := range para.Runs() {
					rPr := r.X().RPr
					if rPr == nil || rPr.Sz == nil ||
						(rPr.Sz.ValAttr.ST_UnsignedDecimalNumber == nil && rPr.Sz.ValAttr.ST_PositiveUniversalMeasure == nil) {
						continue
					}
					if cur := *rPr.Sz.ValAttr.ST_UnsignedDecimalNumber; cur > 24 {
						twentyFour := uint64(24)
						rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &twentyFour
					}
				}
			}
		}
	}
}

// applyCoverTitleSchemaFix 在模板表格格式化入口对首个表格执行 A1 封面题目栏
// schema 修复（行高统一/垂直居中/去垫高）。它复用 formatCoverTitleTable 的
// 保守策略，仅当目标文档首表具备封面题目表特征（首行为"题目/课题名称"标签）时生效。
func (p *EnhancedProcessor) applyCoverTitleSchemaFix(doc *document.Document) {
	tables := doc.Tables()
	if len(tables) == 0 {
		return
	}
	first := tables[0]
	rows := first.Rows()
	if len(rows) == 0 {
		return
	}
	// 首行首个单元格文本用于判定封面题目表
	head := ""
	for _, para := range rows[0].Cells()[0].Paragraphs() {
		head += p.extractParagraphText(para)
	}
	head = strings.TrimSpace(head)
	if head == "" || !(strings.Contains(head, "题目") || strings.Contains(head, "课题名称")) {
		return
	}
	formatCoverTitleTable(first, p)
	log.Printf("[A1] 封面题目表 schema 修复：行高统一 / 垂直居中 / 去除超大字号垫高 run")
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

	outputXML := string(outputEntries["word/document.xml"])
	outputSections := describeStrictSections(outputXML)
	templateSectionRefs := mapTemplateSectionHeaderFooterRefsWithVisibleHeaders(
		outputXML,
		string(templateEntries["word/document.xml"]),
		templateVisibleHeaderRoles(templateEntries),
	)
	// D5: apply a reference-aware per-reference merge instead of replacing the
	// student's header/footer parts wholesale. For every header/footer slot
	// (header/footer x default/first/even) of every section, the student's own
	// reference wins whenever it resolves to a part that carries visible text
	// (supervisor name, thesis title, custom field, TOC, page number). Only
	// empty student placeholders adopt the template part. Fully retained
	// student layouts keep their original part bytes and remain untouched.
	mixedSectionRefs, requiredRelationshipIDs, retainIDs, guardedParts :=
		mergeSectionHeaderFooterRefsPerSlot(outputEntries, outputSections, templateSectionRefs)
	relationshipIDs, copiedParts, err := mergeSelectedDocumentRelationships(
		outputEntries,
		templateEntries,
		requiredRelationshipIDs,
		retainIDs,
	)
	if err != nil {
		return err
	}
	if err := mergeTemplateHeaderFooterStyles(outputEntries, templateEntries, copiedParts); err != nil {
		return err
	}
	mergeCopiedPartContentTypes(outputEntries, templateEntries, copiedParts)
	mergeDocumentSectionHeaderFooterRefsWithPlan(outputEntries, mixedSectionRefs, relationshipIDs)
	if len(copiedParts) > 0 {
		// The package-level even/odd switch follows the template only when
		// template header/footer parts were actually adopted. A fully
		// retained student layout must not be normalized against a template
		// that contributed no header/footer part.
		syncEvenOddHeadersSetting(outputEntries, templateEntries)
	}
	// D5: fail closed instead of silently discarding a retained student part.
	if err := verifyStudentHeaderFooterRetained(outputEntries, guardedParts); err != nil {
		return err
	}
	// A2: 页眉文字右端用 tab 右对齐而非空格撑排 — 对刚采用的模板页眉部件
	// 做空格 run→tab 升级，并在段落 pPr 预制 right tab stop（版心右缘 8820 twips）。
	// 除本次从模板复制的新部件外，学生文档中已存在、内容即模板页眉（校名+
	// 章节名+空格撑排）的 header 部件同样属于空格撑排形态，一并升级；空占位
	// header 与不含特征文字的部件由 normalize 内部白名单过滤，保持不变。
	headerParts := make(map[string]string, len(copiedParts)+8)
	for k, v := range copiedParts {
		headerParts[k] = v
	}
	for entryName := range outputEntries {
		if strings.HasPrefix(entryName, "word/header") && strings.HasSuffix(entryName, ".xml") {
			headerParts[entryName] = string(outputEntries[entryName])
		}
	}
	if n := normalizeHeaderTabRightAlignment(outputEntries, headerParts); n > 0 {
		log.Printf("[A2] 页眉部件空格撑排升级为 tab 右对齐: %d 个部件", n)
	}

	return writeDocxEntries(outputPath, outputEntries)
}

// normalizeHeaderTabRightAlignment 将页眉部件中的"空格撑排"文字升级为
// tab 右对齐：把首个 ≥4 连续半角空格组成的 <w:t> 节点替换为 <w:tab/>，
// 并在所在段落 pPr 注入 right tab 停靠位（8820 twips = 版心右缘），
// 使章节文字（摘要/目录/绪论/参考文献/致谢）右端对齐。仅处理同时含
// 校名/论文标题特征且有空格撑排的页眉部件，最小侵入不碰其他部件字节。
func normalizeHeaderTabRightAlignment(entries map[string][]byte, parts map[string]string) int {
	spaceRun := regexp.MustCompile(`<w:t(?: [^>]*)?>[ ]{4,}</w:t>`)
	pPrTag := regexp.MustCompile(`<w:pPr>[\s\S]*?</w:pPr>`)
	const tabStopXML = `<w:tabs><w:tab w:val="right" w:pos="8820"/></w:tabs>`
	patched := 0
	for part, xmlText := range parts {
		if !strings.Contains(part, "word/header") || !strings.HasSuffix(part, ".xml") {
			continue
		}
		if !spaceRun.MatchString(xmlText) {
			continue
		}
		plain := strings.Join(extractDocxTextNodes(xmlText), "")
		if !strings.Contains(plain, "毕业设计") && !strings.Contains(plain, "论文") {
			continue
		}
		xmlText = pPrTag.ReplaceAllStringFunc(xmlText, func(ppr string) string {
			if strings.Contains(ppr, "<w:tabs>") {
				return ppr
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(ppr, "<w:pPr>"), "</w:pPr>")
			return "<w:pPr>" + tabStopXML + inner + "</w:pPr>"
		})
		xmlText = spaceRun.ReplaceAllString(xmlText, `<w:tab/>`)
		entries[part] = []byte(xmlText)
		patched++
	}
	return patched
}

// resolveDocumentHeaderFooterPartNames resolves header/footer reference tags to
// the concrete part names they point at, so the caller can inspect the bytes
// and decide whether replacing them would discard student-authored content.
func resolveDocumentHeaderFooterPartNames(entries map[string][]byte, refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	relsContent, ok := entries["word/_rels/document.xml.rels"]
	if !ok {
		return nil
	}
	var relationships strictRelationshipSet
	if xml.Unmarshal(relsContent, &relationships) != nil {
		return nil
	}
	byID := make(map[string]string, len(relationships.Relationships))
	for _, rel := range relationships.Relationships {
		if isHeaderFooterRelationship(rel) {
			byID[rel.ID] = resolveRelationshipPart("word/document.xml", rel.Target)
		}
	}
	parts := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id := strictXMLAttributeValue(ref, "r:id"); id != "" {
			if part, ok := byID[id]; ok {
				parts = append(parts, part)
			}
		}
	}
	return parts
}

// headerFooterPartIsEmptyPlaceholder reports whether a header/footer part is an
// empty placeholder (no visible text), i.e. it holds no content that would be
// lost if the template counterpart replaces it.
func headerFooterPartIsEmptyPlaceholder(entries map[string][]byte, partName string) bool {
	content, ok := entries[partName]
	if !ok {
		return true
	}
	for _, text := range extractDocxTextNodes(string(content)) {
		if strings.TrimSpace(text) != "" {
			return false
		}
	}
	return true
}

// mergeSectionHeaderFooterRefsPerSlot merges the template's per-section
// header/footer references with the student's own references slot by slot
// (header/footer x default/first/even). For every slot, the student's own
// reference wins whenever it resolves to a part carrying visible content; only
// empty student placeholders are replaced by the template's part. Student-only
// slots the template does not provide are kept as-is. The returned refs are the
// final per-section ref sets for document.xml, together with the template
// relationship IDs that must be copied in, the student relationship IDs that
// must remain intact, and the student parts whose visible content is guarded.
func mergeSectionHeaderFooterRefsPerSlot(
	entries map[string][]byte,
	sections []strictSectionDescriptor,
	templateSectionRefs [][]string,
) (mixedSectionRefs [][]string, requiredIDs, retainIDs, guardedParts map[string]bool) {
	mixedSectionRefs = make([][]string, len(sections))
	requiredIDs = map[string]bool{}
	retainIDs = map[string]bool{}
	guardedParts = map[string]bool{}
	for index, section := range sections {
		studentByKey := make(map[string]string, len(section.Refs))
		for _, ref := range section.Refs {
			if key := headerFooterReferenceKey(ref); key != "" {
				studentByKey[key] = ref
			}
		}
		selection := make(map[string]string, len(studentByKey))
		if index < len(templateSectionRefs) {
			selection = make(map[string]string, len(templateSectionRefs[index])+len(studentByKey))
			for _, templateRef := range templateSectionRefs[index] {
				key := headerFooterReferenceKey(templateRef)
				if key == "" {
					continue
				}
				// 正文主体节（main body）的默认页眉强制采用模板节页眉。
				// 学生最终文档（如 final.docx）中正文节的"摘要/目录"占位页眉，
				// 本质是从模板拷贝来的章节页眉部件而非学生自撰信息；若按下方
				// "学生可见内容优先"规则保留，正文各页将沿袭摘要/目录页眉，
				// 无法切换到模板正文（绪论）页眉。模板 default header 即为此
				// 类章节唯一正确的页眉，故对 body 节 default slot 直接取模板。
				if section.Role == strictSectionBody && key == "header:default" {
					selection[key] = templateRef
					if id := strictXMLAttributeValue(templateRef, "r:id"); id != "" {
						requiredIDs[id] = true
					}
					continue
				}
				if studentRef, ok := studentByKey[key]; ok {
					// Prefer the student's own part unless it is an empty
					// placeholder; an empty placeholder holds no content that
					// would be lost, so the template part may take its slot.
					id := strictXMLAttributeValue(studentRef, "r:id")
					parts := resolveDocumentHeaderFooterPartNames(entries, []string{studentRef})
					if len(parts) > 0 && !headerFooterPartIsEmptyPlaceholder(entries, parts[0]) {
						selection[key] = studentRef
						if id != "" {
							retainIDs[id] = true
							guardedParts[parts[0]] = true
						}
						continue
					}
				}
				selection[key] = templateRef
				if id := strictXMLAttributeValue(templateRef, "r:id"); id != "" {
					requiredIDs[id] = true
				}
			}
		}
		// Student slots the template does not declare for this section are
		// never touched: replacing them would discard student-authored content.
		for key, studentRef := range studentByKey {
			if _, used := selection[key]; used {
				continue
			}
			selection[key] = studentRef
			id := strictXMLAttributeValue(studentRef, "r:id")
			parts := resolveDocumentHeaderFooterPartNames(entries, []string{studentRef})
			if id != "" {
				retainIDs[id] = true
			}
			if len(parts) > 0 && !headerFooterPartIsEmptyPlaceholder(entries, parts[0]) {
				guardedParts[parts[0]] = true
			}
		}
		mixedSectionRefs[index] = orderedHeaderFooterReferenceTags(selection)
	}
	return mixedSectionRefs, requiredIDs, retainIDs, guardedParts
}

// verifyStudentHeaderFooterRetained asserts that the D5 retention guard held:
// every student header/footer part that carried visible content before the
// merge must still exist, still carry visible content, and still have an
// intact relationship closure afterwards. It fails closed instead of
// silently discarding a retained student part.
func verifyStudentHeaderFooterRetained(entries map[string][]byte, guardedParts map[string]bool) error {
	for part := range guardedParts {
		if _, ok := entries[part]; !ok {
			return fmt.Errorf("retained header/footer part %s went missing after merge", part)
		}
		if headerFooterPartIsEmptyPlaceholder(entries, part) {
			return fmt.Errorf("retained header/footer part %s lost visible content after merge", part)
		}
		if !verifyPartRelationshipClosure(part, entries, map[string]bool{}) {
			return fmt.Errorf("retained header/footer part %s has broken relationship closure", part)
		}
	}
	return nil
}

// ensureEvenOddHeadersSetting keeps the package-level switch consistent with
// the section references we just migrated. Word ignores w:type="even" refs
// when this setting is absent, which makes a visually correct template render
// with the wrong header/footer on alternate pages.
func ensureEvenOddHeadersSetting(entries map[string][]byte) {
	documentXML, ok := entries["word/document.xml"]
	if !ok || !strings.Contains(string(documentXML), `w:type="even"`) {
		return
	}
	settingsXML, ok := entries["word/settings.xml"]
	if !ok || strings.Contains(string(settingsXML), "<w:evenAndOddHeaders") {
		return
	}
	if updated, changed := ooxmlpatch.ApplySettingsProperties(string(settingsXML), ooxmlpatch.SettingsPropertiesSpec{EvenAndOddHeaders: true}); changed {
		entries["word/settings.xml"] = []byte(updated)
	}
}

// syncEvenOddHeadersSetting copies the template's package-level switch rather
// than inferring it from copied references.  A template may contain even refs
// for a cover section while intentionally leaving the switch disabled; turning
// it on globally would make ordinary body sections lose their even-page
// header/footer.
func syncEvenOddHeadersSetting(output, template map[string][]byte) {
	templateSettings := string(template["word/settings.xml"])
	outputSettings, ok := output["word/settings.xml"]
	if !ok {
		return
	}
	if strings.Contains(templateSettings, "<w:evenAndOddHeaders") {
		ensureEvenOddHeadersSetting(output)
		return
	}
	// Do not infer the switch from section references. A template can carry
	// even/first references for a cover sample while deliberately leaving the
	// package-level switch disabled. Enabling it globally changes how every
	// later section resolves headers/footers (and can make body-page headers
	// appear empty). The template's settings part is the sole authority.
	pattern := regexp.MustCompile(`(?s)<w:evenAndOddHeaders\b[^>]*/>|<w:evenAndOddHeaders\b[^>]*>.*?</w:evenAndOddHeaders>`)
	output["word/settings.xml"] = []byte(pattern.ReplaceAllString(string(outputSettings), ""))
	if documentXML, ok := output["word/document.xml"]; ok {
		output["word/document.xml"] = []byte(stripEvenHeaderFooterReferences(string(documentXML)))
	}
}

// stripEvenHeaderFooterReferences keeps a template with the even/odd switch
// disabled internally consistent. Without the switch, even references are
// ignored by Word; leaving them behind also makes verification fail closed.
func stripEvenHeaderFooterReferences(documentXML string) string {
	refs := regexp.MustCompile(`(?s)<w:(?:headerReference|footerReference)\b[^>]*/>`)
	return refs.ReplaceAllStringFunc(documentXML, func(tag string) string {
		if strings.Contains(tag, `w:type="even"`) {
			return ""
		}
		return tag
	})
}

func copyAndMaterializeTemplateHeaderFooter(templatePath, outputPath string, coverInfo map[string]string) error {
	if err := copyTemplateHeaderFooterPackage(templatePath, outputPath); err != nil {
		return err
	}
	// D17: 学校名做成可配置项（从模板 profile 页眉提取，取不到回退默认字符串），
	// 不再硬编码；模板解析成功后按模板路径缓存。
	schoolName := resolveRunningHeaderSchoolName(templatePath)
	entries, err := readDocxEntries(outputPath)
	if err != nil {
		return err
	}
	changed := false
	for name, content := range entries {
		if !strings.HasPrefix(name, "word/header") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		xmlText := string(content)
		texts := extractDocxTextNodes(xmlText)
		if len(texts) == 0 {
			continue
		}
		visible := strings.Join(texts, "")
		updated := xmlText
		if len(coverInfo) > 0 {
			materialized := materializeTemplateHeaderText(visible, coverInfo)
			if materialized != visible {
				updated = replaceDocxTextNodes(updated, distributeHeaderText(texts, materialized))
			}
		}
		if strings.Contains(visible, schoolName) && strings.Contains(visible, "绪论") {
			updated = materializeRunningHeaderStyleRef(updated, schoolName)
		}
		if updated != xmlText {
			entries[name] = []byte(updated)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeDocxEntries(outputPath, entries)
}

// D17：学校名可配置化。
// 默认兜底串保留历史行为（模板解析失败或页眉未能提取到校名时使用）。
const defaultRunningHeaderSchoolName = "重庆工程学院本科生毕业设计（论文）"

var collegeNameCache sync.Map // key: templatePath -> string

// resolveRunningHeaderSchoolName 从模板 profile 的页眉文本提取学校名（可配置），
// 提取不到时回退到 defaultRunningHeaderSchoolName（保留历史行为）。
// 模板解析成功则按模板路径缓存，避免对每份学生文档重复解析同一模板。
func resolveRunningHeaderSchoolName(templatePath string) string {
	if v, ok := collegeNameCache.Load(templatePath); ok {
		return v.(string)
	}
	name := ""
	if profile, err := templateprofile.Extract(templatePath); err == nil {
		name = templateprofile.ExtractCollegeName(profile.Header.Text, "")
	}
	if name == "" {
		name = defaultRunningHeaderSchoolName
	}
	collegeNameCache.Store(templatePath, name)
	return name
}

// materializeRunningHeaderStyleRef keeps the school name and replaces the
// template's sample "1 绪论" with Word's native current Heading 1 field.
// The literal result remains as a safe fallback for renderers that do not
// update fields; Word updates it when the document is opened.
// schoolName 由调用方传入（可配置），空值时按历史默认串处理。
func materializeRunningHeaderStyleRef(headerXML, schoolName string) string {
	if schoolName == "" {
		schoolName = defaultRunningHeaderSchoolName
	}
	if strings.Contains(headerXML, "STYLEREF") {
		return headerXML
	}
	paragraphs := regexp.MustCompile(`(?s)<w:p\b.*?</w:p>`)
	return paragraphs.ReplaceAllStringFunc(headerXML, func(paragraph string) string {
		visible := strings.Join(extractDocxTextNodes(paragraph), "")
		if !strings.Contains(visible, schoolName) || !strings.Contains(visible, "绪论") {
			return paragraph
		}
		schoolAt := strings.Index(paragraph, schoolName)
		runEnd := schoolAt + strings.Index(paragraph[schoolAt:], "</w:r>") + len("</w:r>")
		closeAt := strings.LastIndex(paragraph, "</w:p>")
		if schoolAt < 0 || runEnd < schoolAt || closeAt < runEnd {
			return paragraph
		}
		field := `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> STYLEREF "heading 1" \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1 绪论</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
		return paragraph[:runEnd] + field + paragraph[closeAt:]
	})
}

func CopyTemplateHeaderFooter(templatePath, outputPath string, coverInfo map[string]string) error {
	return copyAndMaterializeTemplateHeaderFooter(templatePath, outputPath, coverInfo)
}

func verifyCopiedHeaderFooterStructure(templatePath, outputPath string) (int, int, bool) {
	_ = templatePath
	outputEntries, err := readDocxEntries(outputPath)
	if err != nil {
		return 0, 0, false
	}

	var relationships strictRelationshipSet
	if err := xml.Unmarshal(outputEntries["word/_rels/document.xml.rels"], &relationships); err != nil {
		return 0, 0, false
	}
	byID := make(map[string]strictRelationshipPart, len(relationships.Relationships))
	for _, rel := range relationships.Relationships {
		byID[rel.ID] = rel
	}

	referencedParts := map[string]bool{}
	headerCount, footerCount := 0, 0
	documentXML := string(outputEntries["word/document.xml"])
	for _, sectPr := range extractSectPrBlocks(documentXML) {
		for _, ref := range extractHeaderFooterReferenceTags(sectPr) {
			id := strictXMLAttributeValue(ref, "r:id")
			rel, ok := byID[id]
			if !ok || !isHeaderFooterRelationship(rel) {
				return 0, 0, false
			}
			// D5: the reference kind must match the relationship type. A
			// headerReference that resolves to a footer part (or vice versa)
			// means the merging step misrouted content, so verification must
			// fail closed instead of treating the swap as valid.
			kind := headerFooterReferenceKind(ref)
			if relIsHeader := strings.Contains(rel.Type, "/header"); relIsHeader != (kind == "header") {
				return 0, 0, false
			}
			partName := resolveRelationshipPart("word/document.xml", rel.Target)
			if _, exists := outputEntries[partName]; !exists {
				return 0, 0, false
			}
			if !referencedParts[partName] {
				referencedParts[partName] = true
				if strings.Contains(rel.Type, "/header") {
					headerCount++
				} else {
					footerCount++
				}
			}
			if !verifyPartRelationshipClosure(partName, outputEntries, map[string]bool{}) {
				return 0, 0, false
			}
		}
	}
	return headerCount, footerCount, true
}

// headerFooterReferenceKind reports whether a header/footer reference tag
// declares a header ("header") or a footer ("footer"); empty for anything that
// is not a well-formed reference.
func headerFooterReferenceKind(ref string) string {
	for _, prefix := range []string{"<w:headerReference", "<w:footerReference"} {
		if strings.Contains(ref, prefix) {
			if strings.Contains(prefix, "header") {
				return "header"
			}
			return "footer"
		}
	}
	return ""
}

func verifyPartRelationshipClosure(partName string, entries map[string][]byte, visiting map[string]bool) bool {
	if visiting[partName] {
		return true
	}
	visiting[partName] = true
	defer delete(visiting, partName)

	relsContent, ok := entries[relationshipPartName(partName)]
	if !ok {
		return true
	}
	var relationships strictRelationshipSet
	if xml.Unmarshal(relsContent, &relationships) != nil {
		return false
	}
	for _, rel := range relationships.Relationships {
		if strings.EqualFold(rel.TargetMode, "External") {
			continue
		}
		targetPart := resolveRelationshipPart(partName, rel.Target)
		if _, exists := entries[targetPart]; !exists {
			return false
		}
		if !verifyPartRelationshipClosure(targetPart, entries, visiting) {
			return false
		}
	}
	return true
}

func materializeTemplateHeaderText(text string, coverInfo map[string]string) string {
	replacements := []string{}
	if college := strings.TrimSpace(coverInfo["学院"]); college != "" {
		replacements = append(replacements, "{学院}", college)
	}
	if title := strings.TrimSpace(coverInfo["题目"]); title != "" {
		replacements = append(replacements, "{题目}", title)
	}
	major := strings.TrimSpace(coverInfo["专业"])
	if major != "" {
		replacements = append(replacements,
			"XXX专业", major+"专业",
			"XX专业", major+"专业",
			"xx专业", major+"专业",
			"{专业}", major,
		)
	}
	if year := graduationYear(coverInfo["班级"]); year != "" {
		replacements = append(replacements,
			"XXX届", year+"届",
			"XX届", year+"届",
			"xx届", year+"届",
			"{届}", year+"届",
		)
	}
	if len(replacements) == 0 {
		return text
	}
	return strings.NewReplacer(replacements...).Replace(text)
}

func graduationYear(className string) string {
	match := regexp.MustCompile(`20\d{2}`).FindString(className)
	if match == "" {
		return ""
	}
	if strings.Contains(className, "届") {
		return match
	}
	year, err := strconv.Atoi(match)
	if err != nil {
		return ""
	}
	return strconv.Itoa(year + 4)
}

func distributeHeaderText(original []string, text string) []string {
	result := make([]string, len(original))
	remaining := []rune(text)
	for index := range original {
		if index == len(original)-1 {
			result[index] = string(remaining)
			break
		}
		width := len([]rune(original[index]))
		if width > len(remaining) {
			width = len(remaining)
		}
		result[index] = string(remaining[:width])
		remaining = remaining[width:]
	}
	return result
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
		if !hasContentTypeIdentity(outputXML, needle) {
			outputXML = strings.Replace(outputXML, "</Types>", needle+"</Types>", 1)
		}
	}
	for _, needle := range extractSelfClosingTags(templateXML, "<Override ", "/>") {
		if strings.Contains(needle, "/word/header") || strings.Contains(needle, "/word/footer") {
			if !hasContentTypeIdentity(outputXML, needle) {
				outputXML = strings.Replace(outputXML, "</Types>", needle+"</Types>", 1)
			}
		}
	}
	outputEntries["[Content_Types].xml"] = []byte(outputXML)
}

func hasContentTypeIdentity(contentTypesXML, tag string) bool {
	for _, match := range strictXMLAttributePattern.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 || (match[1] != "PartName" && match[1] != "Extension") {
			continue
		}
		value := match[3]
		if value == "" {
			value = match[4]
		}
		return strings.Contains(contentTypesXML, match[1]+`="`+value+`"`) ||
			strings.Contains(contentTypesXML, match[1]+`='`+value+`'`)
	}
	return strings.Contains(contentTypesXML, tag)
}

func mergeDocumentRelationships(outputEntries, templateEntries map[string][]byte) map[string]string {
	idMap := map[string]string{}
	outputContent, ok := outputEntries["word/_rels/document.xml.rels"]
	if !ok {
		return idMap
	}
	templateContent, ok := templateEntries["word/_rels/document.xml.rels"]
	if !ok {
		return idMap
	}

	var outputRels strictRelationshipSet
	var templateRels strictRelationshipSet
	if xml.Unmarshal(outputContent, &outputRels) != nil || xml.Unmarshal(templateContent, &templateRels) != nil {
		return idMap
	}

	filtered := make([]strictRelationshipPart, 0, len(outputRels.Relationships))
	usedIDs := map[string]bool{}
	for _, rel := range outputRels.Relationships {
		if isHeaderFooterRelationship(rel) {
			continue
		}
		filtered = append(filtered, rel)
		usedIDs[rel.ID] = true
	}
	for _, rel := range templateRels.Relationships {
		if isHeaderFooterRelationship(rel) {
			originalID := rel.ID
			if usedIDs[rel.ID] {
				rel.ID = nextFreeRelationshipID(usedIDs)
			}
			usedIDs[rel.ID] = true
			idMap[originalID] = rel.ID
			filtered = append(filtered, rel)
		}
	}
	outputRels.Relationships = filtered

	merged, err := xml.Marshal(outputRels)
	if err != nil {
		return map[string]string{}
	}
	outputEntries["word/_rels/document.xml.rels"] = merged
	return idMap
}

func mergeSelectedDocumentRelationships(
	outputEntries,
	templateEntries map[string][]byte,
	requiredIDs map[string]bool,
	retainIDs map[string]bool,
) (map[string]string, map[string]string, error) {
	idMap := map[string]string{}
	copiedParts := map[string]string{}
	if len(requiredIDs) == 0 {
		return idMap, copiedParts, nil
	}
	outputContent, ok := outputEntries["word/_rels/document.xml.rels"]
	if !ok {
		return idMap, copiedParts, fmt.Errorf("missing word/_rels/document.xml.rels")
	}
	templateContent, ok := templateEntries["word/_rels/document.xml.rels"]
	if !ok {
		return idMap, copiedParts, fmt.Errorf("template is missing word/_rels/document.xml.rels")
	}

	var outputRels strictRelationshipSet
	var templateRels strictRelationshipSet
	if err := xml.Unmarshal(outputContent, &outputRels); err != nil {
		return idMap, copiedParts, fmt.Errorf("parse output document relationships: %w", err)
	}
	if err := xml.Unmarshal(templateContent, &templateRels); err != nil {
		return idMap, copiedParts, fmt.Errorf("parse template document relationships: %w", err)
	}

	filtered := make([]strictRelationshipPart, 0, len(outputRels.Relationships)+len(requiredIDs))
	usedIDs := map[string]bool{}
	for _, rel := range outputRels.Relationships {
		// A header/footer relationship referenced by a section that keeps its
		// own part (D5 retention) must not be pruned here; it stays intact so
		// the student's visible content is not dropped. Only references that
		// are about to be replaced by the template are removed.
		if isHeaderFooterRelationship(rel) && !retainIDs[rel.ID] {
			continue
		}
		filtered = append(filtered, rel)
		usedIDs[rel.ID] = true
	}

	for _, rel := range templateRels.Relationships {
		if !isHeaderFooterRelationship(rel) || !requiredIDs[rel.ID] {
			continue
		}
		sourcePart := resolveRelationshipPart("word/document.xml", rel.Target)
		if _, ok := templateEntries[sourcePart]; !ok {
			return idMap, copiedParts, fmt.Errorf("template relationship %s target is missing: %s", rel.ID, sourcePart)
		}
		destinationPart, err := copyTemplatePartClosure(
			sourcePart,
			templateEntries,
			outputEntries,
			copiedParts,
			map[string]bool{},
		)
		if err != nil {
			return idMap, copiedParts, err
		}

		originalID := rel.ID
		if usedIDs[rel.ID] {
			rel.ID = nextFreeRelationshipID(usedIDs)
		}
		usedIDs[rel.ID] = true
		idMap[originalID] = rel.ID
		rel.Target = relativeRelationshipTarget("word/document.xml", destinationPart)
		filtered = append(filtered, rel)
	}

	outputRels.Relationships = filtered
	merged, err := xml.Marshal(outputRels)
	if err != nil {
		return map[string]string{}, map[string]string{}, err
	}
	outputEntries["word/_rels/document.xml.rels"] = merged
	for id := range requiredIDs {
		if _, ok := idMap[id]; !ok {
			return idMap, copiedParts, fmt.Errorf("template section references missing header/footer relationship %s", id)
		}
	}
	return idMap, copiedParts, nil
}

// mergeSelectedPartRelationships copies only the relationships actually used
// by a migrated OOXML fragment, together with each internal target's recursive
// relationship closure. Relationship IDs and colliding part names are remapped.
func mergeSelectedPartRelationships(
	sourcePart,
	destinationPart string,
	requiredIDs map[string]bool,
	templateEntries,
	outputEntries map[string][]byte,
) (map[string]string, map[string]string, error) {
	idMap := map[string]string{}
	copiedParts := map[string]string{}
	if len(requiredIDs) == 0 {
		return idMap, copiedParts, nil
	}

	templateRelationshipsPart := relationshipPartName(sourcePart)
	templateContent, ok := templateEntries[templateRelationshipsPart]
	if !ok {
		return nil, nil, fmt.Errorf("template part %s references relationships, but %s is missing", sourcePart, templateRelationshipsPart)
	}
	var templateRelationships strictRelationshipSet
	if err := xml.Unmarshal(templateContent, &templateRelationships); err != nil {
		return nil, nil, fmt.Errorf("parse template relationships %s: %w", templateRelationshipsPart, err)
	}

	outputRelationshipsPart := relationshipPartName(destinationPart)
	outputRelationships := strictRelationshipSet{Xmlns: "http://schemas.openxmlformats.org/package/2006/relationships"}
	if outputContent, exists := outputEntries[outputRelationshipsPart]; exists {
		if err := xml.Unmarshal(outputContent, &outputRelationships); err != nil {
			return nil, nil, fmt.Errorf("parse output relationships %s: %w", outputRelationshipsPart, err)
		}
		if outputRelationships.Xmlns == "" {
			outputRelationships.Xmlns = "http://schemas.openxmlformats.org/package/2006/relationships"
		}
	}

	usedIDs := map[string]bool{}
	for _, relationship := range outputRelationships.Relationships {
		usedIDs[relationship.ID] = true
	}
	for _, relationship := range templateRelationships.Relationships {
		if !requiredIDs[relationship.ID] {
			continue
		}
		originalID := relationship.ID
		if usedIDs[relationship.ID] {
			relationship.ID = nextFreeRelationshipID(usedIDs)
		}
		usedIDs[relationship.ID] = true
		idMap[originalID] = relationship.ID

		if !strings.EqualFold(relationship.TargetMode, "External") {
			sourceTarget := resolveRelationshipPart(sourcePart, relationship.Target)
			if _, exists := templateEntries[sourceTarget]; !exists {
				return nil, nil, fmt.Errorf("template relationship %s target is missing: %s", originalID, sourceTarget)
			}
			destinationTarget, err := copyTemplatePartClosure(
				sourceTarget,
				templateEntries,
				outputEntries,
				copiedParts,
				map[string]bool{},
			)
			if err != nil {
				return nil, nil, err
			}
			relationship.Target = relativeRelationshipTarget(destinationPart, destinationTarget)
		}
		outputRelationships.Relationships = append(outputRelationships.Relationships, relationship)
	}
	for id := range requiredIDs {
		if _, ok := idMap[id]; !ok {
			return nil, nil, fmt.Errorf("template part %s references missing relationship %s", sourcePart, id)
		}
	}

	merged, err := xml.Marshal(outputRelationships)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal output relationships %s: %w", outputRelationshipsPart, err)
	}
	outputEntries[outputRelationshipsPart] = merged
	return idMap, copiedParts, nil
}

func collectStrictRelationshipReferences(content string) map[string]bool {
	ids := map[string]bool{}
	for _, tag := range strictRelationshipReferenceTagPattern.FindAllString(content, -1) {
		for _, attribute := range []string{"r:id", "r:embed", "r:link"} {
			if id := strictXMLAttributeValue(tag, attribute); id != "" {
				ids[id] = true
			}
		}
	}
	return ids
}

func rewriteStrictRelationshipReferences(content string, idMap map[string]string) string {
	return strictRelationshipReferenceTagPattern.ReplaceAllStringFunc(content, func(tag string) string {
		for _, attribute := range []string{"r:id", "r:embed", "r:link"} {
			if mapped := idMap[strictXMLAttributeValue(tag, attribute)]; mapped != "" {
				tag = replaceStrictXMLAttribute(tag, attribute, mapped)
			}
		}
		return tag
	})
}

func copyTemplatePartClosure(
	sourcePart string,
	templateEntries,
	outputEntries map[string][]byte,
	copiedParts map[string]string,
	visiting map[string]bool,
) (string, error) {
	sourcePart = normalizePackagePartName(sourcePart)
	if destinationPart, ok := copiedParts[sourcePart]; ok {
		return destinationPart, nil
	}
	if visiting[sourcePart] {
		return "", fmt.Errorf("relationship cycle at %s", sourcePart)
	}
	sourceContent, ok := templateEntries[sourcePart]
	if !ok {
		return "", fmt.Errorf("missing relationship target %s", sourcePart)
	}

	visiting[sourcePart] = true
	defer delete(visiting, sourcePart)

	destinationPart := allocateCopiedPartName(sourcePart, sourceContent, templateEntries, outputEntries)
	copiedParts[sourcePart] = destinationPart
	if _, exists := outputEntries[destinationPart]; !exists {
		outputEntries[destinationPart] = append([]byte(nil), sourceContent...)
	}

	sourceRelsPart := relationshipPartName(sourcePart)
	sourceRelsContent, hasRelationships := templateEntries[sourceRelsPart]
	if !hasRelationships {
		return destinationPart, nil
	}

	var relationships strictRelationshipSet
	if err := xml.Unmarshal(sourceRelsContent, &relationships); err != nil {
		return "", fmt.Errorf("parse relationships %s: %w", sourceRelsPart, err)
	}
	for index := range relationships.Relationships {
		rel := &relationships.Relationships[index]
		if strings.EqualFold(rel.TargetMode, "External") {
			continue
		}
		childSourcePart := resolveRelationshipPart(sourcePart, rel.Target)
		childDestinationPart, err := copyTemplatePartClosure(
			childSourcePart,
			templateEntries,
			outputEntries,
			copiedParts,
			visiting,
		)
		if err != nil {
			return "", err
		}
		rel.Target = relativeRelationshipTarget(destinationPart, childDestinationPart)
	}

	destinationRelationships, err := xml.Marshal(relationships)
	if err != nil {
		return "", fmt.Errorf("marshal relationships %s: %w", sourceRelsPart, err)
	}
	outputEntries[relationshipPartName(destinationPart)] = destinationRelationships
	return destinationPart, nil
}

func allocateCopiedPartName(
	sourcePart string,
	sourceContent []byte,
	templateEntries,
	outputEntries map[string][]byte,
) string {
	existingContent, exists := outputEntries[sourcePart]
	if !exists {
		return sourcePart
	}
	sourceRelationships := templateEntries[relationshipPartName(sourcePart)]
	existingRelationships := outputEntries[relationshipPartName(sourcePart)]
	if bytes.Equal(existingContent, sourceContent) && bytes.Equal(existingRelationships, sourceRelationships) {
		return sourcePart
	}

	extension := pathpkg.Ext(sourcePart)
	base := strings.TrimSuffix(sourcePart, extension)
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s_template%d%s", base, suffix, extension)
		if _, exists := outputEntries[candidate]; !exists {
			return candidate
		}
	}
}

func normalizePackagePartName(name string) string {
	return strings.TrimPrefix(pathpkg.Clean(strings.ReplaceAll(name, `\`, "/")), "/")
}

func relationshipPartName(partName string) string {
	partName = normalizePackagePartName(partName)
	return pathpkg.Join(pathpkg.Dir(partName), "_rels", pathpkg.Base(partName)+".rels")
}

func resolveRelationshipPart(sourcePart, target string) string {
	target = strings.ReplaceAll(target, `\`, "/")
	if strings.HasPrefix(target, "/") {
		return normalizePackagePartName(target)
	}
	return normalizePackagePartName(pathpkg.Join(pathpkg.Dir(sourcePart), target))
}

func relativeRelationshipTarget(sourcePart, targetPart string) string {
	relative, err := filepath.Rel(
		filepath.FromSlash(pathpkg.Dir(normalizePackagePartName(sourcePart))),
		filepath.FromSlash(normalizePackagePartName(targetPart)),
	)
	if err != nil {
		return normalizePackagePartName(targetPart)
	}
	return filepath.ToSlash(relative)
}

func mergeCopiedPartContentTypes(
	outputEntries,
	templateEntries map[string][]byte,
	copiedParts map[string]string,
) {
	outputContent, ok := outputEntries["[Content_Types].xml"]
	if !ok || len(copiedParts) == 0 {
		return
	}
	templateContent, ok := templateEntries["[Content_Types].xml"]
	if !ok {
		return
	}

	outputXML := string(outputContent)
	templateXML := string(templateContent)
	requiredExtensions := map[string]bool{}
	for _, destinationPart := range copiedParts {
		extension := strings.TrimPrefix(strings.ToLower(pathpkg.Ext(destinationPart)), ".")
		if extension != "" {
			requiredExtensions[extension] = true
		}
	}

	for _, tag := range extractSelfClosingTags(templateXML, "<Default ", "/>") {
		extension := strings.ToLower(strictXMLAttributeValue(tag, "Extension"))
		if requiredExtensions[extension] && !hasContentTypeIdentity(outputXML, tag) {
			outputXML = strings.Replace(outputXML, "</Types>", tag+"</Types>", 1)
		}
	}
	for sourcePart, destinationPart := range copiedParts {
		sourceName := "/" + normalizePackagePartName(sourcePart)
		for _, tag := range extractSelfClosingTags(templateXML, "<Override ", "/>") {
			if strictXMLAttributeValue(tag, "PartName") != sourceName {
				continue
			}
			mappedTag := replaceStrictXMLAttribute(tag, "PartName", "/"+normalizePackagePartName(destinationPart))
			if !hasContentTypeIdentity(outputXML, mappedTag) {
				outputXML = strings.Replace(outputXML, "</Types>", mappedTag+"</Types>", 1)
			}
			break
		}
	}
	outputEntries["[Content_Types].xml"] = []byte(outputXML)
}

var strictStyleBlockPattern = regexp.MustCompile(`(?s)<w:style\b[^>]*(?:/>|>.*?</w:style>)`)
var strictStyleReferencePattern = regexp.MustCompile(`<w:(?:pStyle|rStyle)\b[^>]*>`)
var strictStyleDependencyPattern = regexp.MustCompile(`<w:(?:basedOn|link|next)\b[^>]*>`)
var strictRunFontsPattern = regexp.MustCompile(`<w:rFonts\b[^>]*>`)
var strictFontBlockPattern = regexp.MustCompile(`(?s)<w:font\b[^>]*(?:/>|>.*?</w:font>)`)
var strictAbstractNumberingPattern = regexp.MustCompile(`(?s)<w:abstractNum\b[^>]*>.*?</w:abstractNum>`)
var strictNumberingInstancePattern = regexp.MustCompile(`(?s)<w:num\b[^>]*>.*?</w:num>`)
var strictNumberingPictureBulletPattern = regexp.MustCompile(`(?s)<w:numPicBullet\b[^>]*>.*?</w:numPicBullet>`)
var strictAbstractNumberingStartPattern = regexp.MustCompile(`<w:abstractNum\b[^>]*>`)
var strictNumberingInstanceStartPattern = regexp.MustCompile(`<w:num\b[^>]*>`)
var strictNumberingPictureBulletStartPattern = regexp.MustCompile(`<w:numPicBullet\b[^>]*>`)
var strictAbstractNumberingIDPattern = regexp.MustCompile(`<w:abstractNumId\b[^>]*>`)
var strictNumberingIDPattern = regexp.MustCompile(`<w:numId\b[^>]*>`)
var strictNumberingPictureBulletIDPattern = regexp.MustCompile(`<w:lvlPicBulletId\b[^>]*>`)
var strictNumberingStyleReferencePattern = regexp.MustCompile(`<w:(?:pStyle|rStyle|numStyleLink|styleLink)\b[^>]*>`)
var strictNumberingRootPattern = regexp.MustCompile(`<w:numbering\b[^>]*>`)
var strictRelationshipReferenceTagPattern = regexp.MustCompile(`<[^>]+\br:(?:id|embed|link)\s*=\s*(?:"[^"]*"|'[^']*')[^>]*>`)

type strictNumberingCatalog struct {
	PictureBullets map[string]string
	Abstract       map[string]string
	Numbers        map[string]string
}

func mergeTemplateHeaderFooterStyles(
	outputEntries,
	templateEntries map[string][]byte,
	copiedParts map[string]string,
) error {
	partNames := make([]string, 0)
	referencedStyles := map[string]bool{}
	for sourcePart, destinationPart := range copiedParts {
		if !isHeaderFooterPart(sourcePart) {
			continue
		}
		partNames = append(partNames, destinationPart)
		for _, id := range collectStrictStyleReferences(string(outputEntries[destinationPart])) {
			referencedStyles[id] = true
		}
	}
	if len(referencedStyles) == 0 {
		return nil
	}

	templateStyles, ok := templateEntries["word/styles.xml"]
	if !ok {
		return fmt.Errorf("template header/footer references styles, but word/styles.xml is missing")
	}
	templateDefinitions := strictStyleDefinitions(string(templateStyles))
	templateNumberingPart := strictDocumentRelationshipPart(templateEntries, "/numbering")
	templateNumberingXML := string(templateEntries[templateNumberingPart])
	closure, err := strictStyleClosureWithNumbering(referencedStyles, templateDefinitions, templateNumberingXML)
	if err != nil {
		return err
	}

	outputStyles := string(outputEntries["word/styles.xml"])
	usedStyleIDs := map[string]bool{}
	for id := range strictStyleDefinitions(outputStyles) {
		usedStyleIDs[id] = true
	}
	idMap := make(map[string]string, len(closure))
	for _, id := range closure {
		destinationID := id
		if usedStyleIDs[destinationID] {
			destinationID = nextTemplateStyleID(id, usedStyleIDs)
		}
		usedStyleIDs[destinationID] = true
		idMap[id] = destinationID
	}
	themeFonts := parseStrictThemeFonts(templateEntries["word/theme/theme1.xml"], string(templateStyles))
	numberingIDMap, numberingXML, err := mergeTemplateStyleNumbering(
		outputEntries,
		templateEntries,
		templateNumberingPart,
		closure,
		templateDefinitions,
		idMap,
		themeFonts,
	)
	if err != nil {
		return err
	}

	migratedStyles := make([]string, 0, len(closure))
	materializedXML := make([]string, 0, len(closure)+len(partNames)+len(numberingXML))
	materializedXML = append(materializedXML, numberingXML...)
	for _, id := range closure {
		definition := replaceStrictXMLAttribute(templateDefinitions[id], "w:styleId", idMap[id])
		definition = rewriteStrictStyleDependencies(definition, idMap)
		definition = rewriteStrictNumberingReferences(definition, numberingIDMap)
		definition, unresolved := materializeStrictThemeFonts(definition, themeFonts)
		if unresolved {
			return fmt.Errorf("cannot resolve template theme font used by style %q", id)
		}
		if ordered, _, orderErr := ooxmlpkg.RepairPropertyBlock([]byte(definition), "style"); orderErr != nil {
			return fmt.Errorf("normalize migrated style %q: %w", id, orderErr)
		} else {
			definition = string(ordered)
		}
		migratedStyles = append(migratedStyles, definition)
		materializedXML = append(materializedXML, definition)
	}
	outputStyles, err = appendStrictStyleDefinitions(outputStyles, migratedStyles)
	if err != nil {
		return err
	}
	outputEntries["word/styles.xml"] = []byte(outputStyles)
	if err := ensureTemplateStylesRelationship(outputEntries, templateEntries); err != nil {
		return err
	}
	mergeCopiedPartContentTypes(outputEntries, templateEntries, map[string]string{
		"word/styles.xml": "word/styles.xml",
	})

	sort.Strings(partNames)
	for _, partName := range partNames {
		content := rewriteStrictStyleReferences(string(outputEntries[partName]), idMap)
		content, unresolved := materializeStrictThemeFonts(content, themeFonts)
		if unresolved {
			return fmt.Errorf("cannot resolve template theme font used by %s", partName)
		}
		outputEntries[partName] = []byte(content)
		materializedXML = append(materializedXML, content)
	}
	mergeRequiredTemplateFonts(outputEntries, templateEntries, materializedXML)
	return nil
}

func ensureTemplateStylesRelationship(outputEntries, templateEntries map[string][]byte) error {
	return ensureTemplateDocumentRelationship(outputEntries, templateEntries, "/styles", "word/styles.xml")
}

func ensureTemplateDocumentRelationship(outputEntries, templateEntries map[string][]byte, relationshipSuffix, destinationPart string) error {
	var outputRelationships strictRelationshipSet
	if err := xml.Unmarshal(outputEntries["word/_rels/document.xml.rels"], &outputRelationships); err != nil {
		return fmt.Errorf("parse output document relationships for %s: %w", pathpkg.Base(relationshipSuffix), err)
	}
	usedIDs := map[string]bool{}
	for _, relationship := range outputRelationships.Relationships {
		usedIDs[relationship.ID] = true
		if strings.HasSuffix(strings.TrimSpace(relationship.Type), relationshipSuffix) {
			return nil
		}
	}

	var templateRelationships strictRelationshipSet
	if err := xml.Unmarshal(templateEntries["word/_rels/document.xml.rels"], &templateRelationships); err != nil {
		return fmt.Errorf("parse template document relationships for %s: %w", pathpkg.Base(relationshipSuffix), err)
	}
	for _, relationship := range templateRelationships.Relationships {
		if !strings.HasSuffix(strings.TrimSpace(relationship.Type), relationshipSuffix) {
			continue
		}
		relationship.ID = nextFreeRelationshipID(usedIDs)
		relationship.Target = relativeRelationshipTarget("word/document.xml", destinationPart)
		relationship.TargetMode = ""
		outputRelationships.Relationships = append(outputRelationships.Relationships, relationship)
		merged, err := xml.Marshal(outputRelationships)
		if err != nil {
			return fmt.Errorf("marshal output document %s relationship: %w", pathpkg.Base(relationshipSuffix), err)
		}
		outputEntries["word/_rels/document.xml.rels"] = merged
		return nil
	}
	return fmt.Errorf("template %s has no document relationship", destinationPart)
}

func isHeaderFooterPart(name string) bool {
	name = normalizePackagePartName(name)
	return (strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer")) &&
		strings.HasSuffix(name, ".xml")
}

func collectStrictStyleReferences(content string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, tag := range strictStyleReferencePattern.FindAllString(content, -1) {
		id := strictXMLAttributeValue(tag, "w:val")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func strictStyleDefinitions(stylesXML string) map[string]string {
	definitions := map[string]string{}
	for _, definition := range strictStyleBlockPattern.FindAllString(stylesXML, -1) {
		if id := strictXMLAttributeValue(definition, "w:styleId"); id != "" {
			definitions[id] = definition
		}
	}
	return definitions
}

func strictStyleClosure(rootIDs map[string]bool, definitions map[string]string) ([]string, error) {
	var ordered []string
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			// Linked paragraph/character styles and next-style references may
			// legally form cycles; the visited set is the closure boundary.
			return nil
		case 2:
			return nil
		}
		definition, ok := definitions[id]
		if !ok {
			// Word documents in the wild often retain references to built-in or
			// latent styles which are not serialized in styles.xml (the Chongqing
			// template uses ids "9" and "14" in page-number text boxes).  These
			// references are valid enough for Word's built-in style fallback and
			// must not make an otherwise usable header/footer package impossible
			// to copy.  Leave the reference untouched instead of inventing a
			// potentially incorrect formatting definition.
			state[id] = 2
			return nil
		}
		state[id] = 1
		for _, tag := range strictStyleDependencyPattern.FindAllString(definition, -1) {
			if dependency := strictXMLAttributeValue(tag, "w:val"); dependency != "" {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		ordered = append(ordered, id)
		return nil
	}

	roots := make([]string, 0, len(rootIDs))
	for id := range rootIDs {
		roots = append(roots, id)
	}
	sort.Strings(roots)
	for _, id := range roots {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func strictStyleClosureWithNumbering(rootIDs map[string]bool, definitions map[string]string, numberingXML string) ([]string, error) {
	roots := make(map[string]bool, len(rootIDs))
	for id := range rootIDs {
		roots[id] = true
	}
	for {
		closure, err := strictStyleClosure(roots, definitions)
		if err != nil {
			return nil, err
		}
		requiredNumbers := map[string]bool{}
		for _, id := range closure {
			for _, numberID := range collectStrictNumberingReferences(definitions[id]) {
				requiredNumbers[numberID] = true
			}
		}
		dependencies, err := strictNumberingStyleDependencies(numberingXML, requiredNumbers)
		if err != nil {
			return nil, err
		}
		changed := false
		for _, id := range dependencies {
			if !roots[id] {
				roots[id] = true
				changed = true
			}
		}
		if !changed {
			return closure, nil
		}
	}
}

func strictNumberingStyleDependencies(numberingXML string, requiredNumbers map[string]bool) ([]string, error) {
	if len(requiredNumbers) == 0 || numberingXML == "" {
		return nil, nil
	}
	catalog := strictNumberingDefinitions(numberingXML)
	seen := map[string]bool{}
	var result []string
	for numberID := range requiredNumbers {
		if numberID == "0" {
			continue
		}
		numberDefinition, ok := catalog.Numbers[numberID]
		if !ok {
			return nil, fmt.Errorf("template style references missing numbering instance %s", numberID)
		}
		abstractID := strictAbstractNumberingID(numberDefinition)
		abstractDefinition, ok := catalog.Abstract[abstractID]
		if !ok {
			return nil, fmt.Errorf("template numbering instance %s references missing abstract numbering %s", numberID, abstractID)
		}
		for _, tag := range strictNumberingStyleReferencePattern.FindAllString(abstractDefinition, -1) {
			id := strictXMLAttributeValue(tag, "w:val")
			if id != "" && !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func mergeTemplateStyleNumbering(
	outputEntries,
	templateEntries map[string][]byte,
	templateNumberingPart string,
	styleClosure []string,
	templateStyles map[string]string,
	styleIDMap map[string]string,
	themeFonts strictThemeFontSet,
) (map[string]string, []string, error) {
	requiredNumbers := make([]string, 0)
	seenNumbers := map[string]bool{}
	for _, styleID := range styleClosure {
		for _, numberID := range collectStrictNumberingReferences(templateStyles[styleID]) {
			if numberID == "0" || seenNumbers[numberID] {
				continue
			}
			seenNumbers[numberID] = true
			requiredNumbers = append(requiredNumbers, numberID)
		}
	}
	if len(requiredNumbers) == 0 {
		return map[string]string{}, nil, nil
	}
	if templateNumberingPart == "" {
		return nil, nil, fmt.Errorf("template styles reference numbering, but the numbering relationship is missing")
	}
	templateNumbering, ok := templateEntries[templateNumberingPart]
	if !ok {
		return nil, nil, fmt.Errorf("template styles reference numbering, but %s is missing", templateNumberingPart)
	}
	templateCatalog := strictNumberingDefinitions(string(templateNumbering))

	outputNumberingPart := strictDocumentRelationshipPart(outputEntries, "/numbering")
	if outputNumberingPart == "" {
		outputNumberingPart = "word/numbering.xml"
	}
	outputNumbering := string(outputEntries[outputNumberingPart])
	if outputNumbering == "" {
		outputNumbering = `<?xml version="1.0" encoding="UTF-8"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:numbering>`
	}
	outputCatalog := strictNumberingDefinitions(outputNumbering)
	usedNumberIDs := make(map[string]bool, len(outputCatalog.Numbers))
	usedAbstractIDs := make(map[string]bool, len(outputCatalog.Abstract))
	usedPictureBulletIDs := make(map[string]bool, len(outputCatalog.PictureBullets))
	for id := range outputCatalog.Numbers {
		usedNumberIDs[id] = true
	}
	for id := range outputCatalog.Abstract {
		usedAbstractIDs[id] = true
	}
	for id := range outputCatalog.PictureBullets {
		usedPictureBulletIDs[id] = true
	}

	numberIDMap := map[string]string{}
	abstractIDMap := map[string]string{}
	pictureBulletIDMap := map[string]string{}
	var pictureBulletDefinitions, abstractDefinitions, numberDefinitions []string
	var materializedXML []string
	for _, sourceNumberID := range requiredNumbers {
		numberDefinition, ok := templateCatalog.Numbers[sourceNumberID]
		if !ok {
			return nil, nil, fmt.Errorf("template style references missing numbering instance %s", sourceNumberID)
		}
		sourceAbstractID := strictAbstractNumberingID(numberDefinition)
		abstractDefinition, ok := templateCatalog.Abstract[sourceAbstractID]
		if !ok {
			return nil, nil, fmt.Errorf("template numbering instance %s references missing abstract numbering %s", sourceNumberID, sourceAbstractID)
		}

		destinationAbstractID := abstractIDMap[sourceAbstractID]
		if destinationAbstractID == "" {
			destinationAbstractID = allocateStrictDecimalID(sourceAbstractID, usedAbstractIDs, 0)
			abstractIDMap[sourceAbstractID] = destinationAbstractID
			abstractDefinition = strictAbstractNumberingStartPattern.ReplaceAllStringFunc(abstractDefinition, func(tag string) string {
				return replaceStrictXMLAttribute(tag, "w:abstractNumId", destinationAbstractID)
			})
			abstractDefinition = rewriteStrictNumberingStyleReferences(abstractDefinition, styleIDMap)
			for _, sourcePictureBulletID := range collectStrictNumberingPictureBulletReferences(abstractDefinition) {
				destinationPictureBulletID := pictureBulletIDMap[sourcePictureBulletID]
				if destinationPictureBulletID == "" {
					pictureBulletDefinition, exists := templateCatalog.PictureBullets[sourcePictureBulletID]
					if !exists {
						return nil, nil, fmt.Errorf("template abstract numbering %s references missing picture bullet %s", sourceAbstractID, sourcePictureBulletID)
					}
					destinationPictureBulletID = allocateStrictDecimalID(sourcePictureBulletID, usedPictureBulletIDs, 0)
					pictureBulletIDMap[sourcePictureBulletID] = destinationPictureBulletID
					pictureBulletDefinition = strictNumberingPictureBulletStartPattern.ReplaceAllStringFunc(pictureBulletDefinition, func(tag string) string {
						return replaceStrictXMLAttribute(tag, "w:numPicBulletId", destinationPictureBulletID)
					})
					pictureBulletDefinitions = append(pictureBulletDefinitions, pictureBulletDefinition)
				}
				abstractDefinition = rewriteStrictNumberingPictureBulletReferences(abstractDefinition, map[string]string{
					sourcePictureBulletID: destinationPictureBulletID,
				})
			}
			var unresolved bool
			abstractDefinition, unresolved = materializeStrictThemeFonts(abstractDefinition, themeFonts)
			if unresolved {
				return nil, nil, fmt.Errorf("cannot resolve template theme font used by abstract numbering %s", sourceAbstractID)
			}
			abstractDefinitions = append(abstractDefinitions, abstractDefinition)
			materializedXML = append(materializedXML, abstractDefinition)
		}

		destinationNumberID := allocateStrictDecimalID(sourceNumberID, usedNumberIDs, 1)
		numberIDMap[sourceNumberID] = destinationNumberID
		numberDefinition = strictNumberingInstanceStartPattern.ReplaceAllStringFunc(numberDefinition, func(tag string) string {
			return replaceStrictXMLAttribute(tag, "w:numId", destinationNumberID)
		})
		numberDefinition = strictAbstractNumberingIDPattern.ReplaceAllStringFunc(numberDefinition, func(tag string) string {
			return replaceStrictXMLAttribute(tag, "w:val", destinationAbstractID)
		})
		var unresolved bool
		numberDefinition, unresolved = materializeStrictThemeFonts(numberDefinition, themeFonts)
		if unresolved {
			return nil, nil, fmt.Errorf("cannot resolve template theme font used by numbering instance %s", sourceNumberID)
		}
		numberDefinitions = append(numberDefinitions, numberDefinition)
		materializedXML = append(materializedXML, numberDefinition)
	}

	relationshipIDMap, copiedParts, err := mergeSelectedPartRelationships(
		templateNumberingPart,
		outputNumberingPart,
		collectStrictRelationshipReferences(strings.Join(pictureBulletDefinitions, "")),
		templateEntries,
		outputEntries,
	)
	if err != nil {
		return nil, nil, err
	}
	for index, definition := range pictureBulletDefinitions {
		definition = rewriteStrictRelationshipReferences(definition, relationshipIDMap)
		var unresolved bool
		definition, unresolved = materializeStrictThemeFonts(definition, themeFonts)
		if unresolved {
			return nil, nil, fmt.Errorf("cannot resolve template theme font used by picture bullet")
		}
		pictureBulletDefinitions[index] = definition
		materializedXML = append(materializedXML, definition)
	}
	outputNumbering = mergeStrictNumberingRootNamespaces(outputNumbering, string(templateNumbering))
	merged, err := appendStrictNumberingDefinitions(outputNumbering, pictureBulletDefinitions, abstractDefinitions, numberDefinitions)
	if err != nil {
		return nil, nil, err
	}
	outputEntries[outputNumberingPart] = []byte(merged)
	if err := ensureTemplateDocumentRelationship(outputEntries, templateEntries, "/numbering", outputNumberingPart); err != nil {
		return nil, nil, err
	}
	copiedParts[templateNumberingPart] = outputNumberingPart
	mergeCopiedPartContentTypes(outputEntries, templateEntries, copiedParts)
	return numberIDMap, materializedXML, nil
}

func strictNumberingDefinitions(numberingXML string) strictNumberingCatalog {
	catalog := strictNumberingCatalog{
		PictureBullets: map[string]string{},
		Abstract:       map[string]string{},
		Numbers:        map[string]string{},
	}
	for _, definition := range strictNumberingPictureBulletPattern.FindAllString(numberingXML, -1) {
		if start := strictNumberingPictureBulletStartPattern.FindString(definition); start != "" {
			if id := strictXMLAttributeValue(start, "w:numPicBulletId"); id != "" {
				catalog.PictureBullets[id] = definition
			}
		}
	}
	for _, definition := range strictAbstractNumberingPattern.FindAllString(numberingXML, -1) {
		if start := strictAbstractNumberingStartPattern.FindString(definition); start != "" {
			if id := strictXMLAttributeValue(start, "w:abstractNumId"); id != "" {
				catalog.Abstract[id] = definition
			}
		}
	}
	for _, definition := range strictNumberingInstancePattern.FindAllString(numberingXML, -1) {
		if start := strictNumberingInstanceStartPattern.FindString(definition); start != "" {
			if id := strictXMLAttributeValue(start, "w:numId"); id != "" {
				catalog.Numbers[id] = definition
			}
		}
	}
	return catalog
}

func strictAbstractNumberingID(numberDefinition string) string {
	tag := strictAbstractNumberingIDPattern.FindString(numberDefinition)
	return strictXMLAttributeValue(tag, "w:val")
}

func collectStrictNumberingReferences(content string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, tag := range strictNumberingIDPattern.FindAllString(content, -1) {
		id := strictXMLAttributeValue(tag, "w:val")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func rewriteStrictNumberingReferences(content string, idMap map[string]string) string {
	return strictNumberingIDPattern.ReplaceAllStringFunc(content, func(tag string) string {
		if mapped := idMap[strictXMLAttributeValue(tag, "w:val")]; mapped != "" {
			return replaceStrictXMLAttribute(tag, "w:val", mapped)
		}
		return tag
	})
}

func rewriteStrictNumberingStyleReferences(content string, idMap map[string]string) string {
	return strictNumberingStyleReferencePattern.ReplaceAllStringFunc(content, func(tag string) string {
		if mapped := idMap[strictXMLAttributeValue(tag, "w:val")]; mapped != "" {
			return replaceStrictXMLAttribute(tag, "w:val", mapped)
		}
		return tag
	})
}

func collectStrictNumberingPictureBulletReferences(content string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, tag := range strictNumberingPictureBulletIDPattern.FindAllString(content, -1) {
		id := strictXMLAttributeValue(tag, "w:val")
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func rewriteStrictNumberingPictureBulletReferences(content string, idMap map[string]string) string {
	return strictNumberingPictureBulletIDPattern.ReplaceAllStringFunc(content, func(tag string) string {
		if mapped := idMap[strictXMLAttributeValue(tag, "w:val")]; mapped != "" {
			return replaceStrictXMLAttribute(tag, "w:val", mapped)
		}
		return tag
	})
}

func allocateStrictDecimalID(source string, used map[string]bool, first int) string {
	if _, err := strconv.Atoi(source); err == nil && !used[source] {
		used[source] = true
		return source
	}
	for candidate := first; ; candidate++ {
		id := strconv.Itoa(candidate)
		if !used[id] {
			used[id] = true
			return id
		}
	}
}

func appendStrictNumberingDefinitions(numberingXML string, pictureBullets, abstracts, numbers []string) (string, error) {
	closing := strings.LastIndex(numberingXML, "</w:numbering>")
	if closing < 0 {
		return "", fmt.Errorf("output numbering part is malformed")
	}
	if len(pictureBullets) > 0 {
		insertAt := closing
		if firstAbstract := strictAbstractNumberingPattern.FindStringIndex(numberingXML); firstAbstract != nil {
			insertAt = firstAbstract[0]
		} else if firstNumber := strictNumberingInstancePattern.FindStringIndex(numberingXML); firstNumber != nil {
			insertAt = firstNumber[0]
		}
		numberingXML = numberingXML[:insertAt] + strings.Join(pictureBullets, "") + numberingXML[insertAt:]
	}
	if len(abstracts) > 0 {
		insertAt := closing
		if firstNumber := strictNumberingInstancePattern.FindStringIndex(numberingXML); firstNumber != nil {
			insertAt = firstNumber[0]
		}
		numberingXML = numberingXML[:insertAt] + strings.Join(abstracts, "") + numberingXML[insertAt:]
	}
	closing = strings.LastIndex(numberingXML, "</w:numbering>")
	return numberingXML[:closing] + strings.Join(numbers, "") + numberingXML[closing:], nil
}

func mergeStrictNumberingRootNamespaces(outputXML, templateXML string) string {
	outputRoot := strictNumberingRootPattern.FindString(outputXML)
	templateRoot := strictNumberingRootPattern.FindString(templateXML)
	if outputRoot == "" || templateRoot == "" {
		return outputXML
	}
	updatedRoot := outputRoot
	for _, match := range strictXMLAttributePattern.FindAllStringSubmatch(templateRoot, -1) {
		if len(match) < 5 || !strings.HasPrefix(match[1], "xmlns") || strictXMLAttributeValueFold(updatedRoot, match[1]) != "" {
			continue
		}
		value := match[3]
		if value == "" {
			value = match[4]
		}
		updatedRoot = addStrictXMLAttribute(updatedRoot, match[1], value)
	}
	return strings.Replace(outputXML, outputRoot, updatedRoot, 1)
}

func strictDocumentRelationshipPart(entries map[string][]byte, relationshipSuffix string) string {
	var relationships strictRelationshipSet
	if xml.Unmarshal(entries["word/_rels/document.xml.rels"], &relationships) != nil {
		return ""
	}
	for _, relationship := range relationships.Relationships {
		if strings.HasSuffix(strings.TrimSpace(relationship.Type), relationshipSuffix) &&
			!strings.EqualFold(relationship.TargetMode, "External") {
			return resolveRelationshipPart("word/document.xml", relationship.Target)
		}
	}
	return ""
}

func nextTemplateStyleID(sourceID string, used map[string]bool) string {
	base := sourceID + "_template"
	for suffix := 1; ; suffix++ {
		candidate := base + strconv.Itoa(suffix)
		if !used[candidate] {
			return candidate
		}
	}
}

func rewriteStrictStyleReferences(content string, idMap map[string]string) string {
	return strictStyleReferencePattern.ReplaceAllStringFunc(content, func(tag string) string {
		id := strictXMLAttributeValue(tag, "w:val")
		if mapped := idMap[id]; mapped != "" {
			return replaceStrictXMLAttribute(tag, "w:val", mapped)
		}
		return tag
	})
}

func rewriteStrictStyleDependencies(definition string, idMap map[string]string) string {
	return strictStyleDependencyPattern.ReplaceAllStringFunc(definition, func(tag string) string {
		id := strictXMLAttributeValue(tag, "w:val")
		if mapped := idMap[id]; mapped != "" {
			return replaceStrictXMLAttribute(tag, "w:val", mapped)
		}
		return tag
	})
}

func appendStrictStyleDefinitions(stylesXML string, definitions []string) (string, error) {
	if len(definitions) == 0 {
		return stylesXML, nil
	}
	if strings.TrimSpace(stylesXML) == "" {
		return `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			strings.Join(definitions, "") + `</w:styles>`, nil
	}
	closeAt := strings.LastIndex(stylesXML, "</w:styles>")
	if closeAt == -1 {
		return "", fmt.Errorf("output word/styles.xml is malformed")
	}
	return stylesXML[:closeAt] + strings.Join(definitions, "") + stylesXML[closeAt:], nil
}

type strictThemeFontFamily struct {
	Latin, EastAsia, Complex string
	Scripts                  map[string]string
}

type strictThemeFontSet struct {
	Major, Minor   strictThemeFontFamily
	EastAsiaScript string
	ComplexScript  string
}

func parseStrictThemeFonts(themeXML []byte, stylesXML string) strictThemeFontSet {
	result := strictThemeFontSet{
		Major: strictThemeFontFamily{Scripts: map[string]string{}},
		Minor: strictThemeFontFamily{Scripts: map[string]string{}},
	}
	result.EastAsiaScript, result.ComplexScript = strictThemeScripts(stylesXML)
	decoder := xml.NewDecoder(bytes.NewReader(themeXML))
	var family *strictThemeFontFamily
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "majorFont":
				family = &result.Major
			case "minorFont":
				family = &result.Minor
			case "latin", "ea", "cs", "font":
				if family == nil {
					continue
				}
				typeface := strictXMLLocalAttribute(value.Attr, "typeface")
				switch value.Name.Local {
				case "latin":
					family.Latin = typeface
				case "ea":
					family.EastAsia = typeface
				case "cs":
					family.Complex = typeface
				case "font":
					if script := strictXMLLocalAttribute(value.Attr, "script"); script != "" {
						family.Scripts[script] = typeface
					}
				}
			}
		case xml.EndElement:
			if value.Name.Local == "majorFont" || value.Name.Local == "minorFont" {
				family = nil
			}
		}
	}
	return result
}

func strictXMLLocalAttribute(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func strictThemeScripts(stylesXML string) (string, string) {
	eastAsiaLanguage, bidiLanguage := "", ""
	for _, tag := range regexp.MustCompile(`<w:lang\b[^>]*>`).FindAllString(stylesXML, -1) {
		if eastAsiaLanguage == "" {
			eastAsiaLanguage = strictXMLAttributeValue(tag, "w:eastAsia")
		}
		if bidiLanguage == "" {
			bidiLanguage = strictXMLAttributeValue(tag, "w:bidi")
		}
	}
	return strictLanguageThemeScript(eastAsiaLanguage), strictLanguageThemeScript(bidiLanguage)
}

func strictLanguageThemeScript(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	switch {
	case strings.HasPrefix(language, "zh-cn"), strings.HasPrefix(language, "zh-sg"):
		return "Hans"
	case strings.HasPrefix(language, "zh"):
		return "Hant"
	case strings.HasPrefix(language, "ja"):
		return "Jpan"
	case strings.HasPrefix(language, "ko"):
		return "Hang"
	case strings.HasPrefix(language, "ar"), strings.HasPrefix(language, "fa"), strings.HasPrefix(language, "ur"):
		return "Arab"
	case strings.HasPrefix(language, "he"):
		return "Hebr"
	case strings.HasPrefix(language, "th"):
		return "Thai"
	default:
		return ""
	}
}

func (fonts strictThemeFontSet) resolve(reference string) string {
	reference = strings.ToLower(strings.TrimSpace(reference))
	var family strictThemeFontFamily
	switch {
	case strings.HasPrefix(reference, "major"):
		family = fonts.Major
	case strings.HasPrefix(reference, "minor"):
		family = fonts.Minor
	default:
		return ""
	}
	switch {
	case strings.Contains(reference, "eastasia"):
		if family.EastAsia != "" {
			return family.EastAsia
		}
		if font := family.Scripts[fonts.EastAsiaScript]; font != "" {
			return font
		}
	case strings.Contains(reference, "bidi"):
		if family.Complex != "" {
			return family.Complex
		}
		if font := family.Scripts[fonts.ComplexScript]; font != "" {
			return font
		}
	default:
		return family.Latin
	}
	return family.Latin
}

func materializeStrictThemeFonts(content string, fonts strictThemeFontSet) (string, bool) {
	unresolved := false
	content = strictRunFontsPattern.ReplaceAllStringFunc(content, func(tag string) string {
		slots := []struct {
			themeNames []string
			fontName   string
		}{
			{[]string{"w:asciiTheme"}, "w:ascii"},
			{[]string{"w:hAnsiTheme"}, "w:hAnsi"},
			{[]string{"w:eastAsiaTheme"}, "w:eastAsia"},
			{[]string{"w:cstheme", "w:csTheme"}, "w:cs"},
		}
		for _, slot := range slots {
			reference := ""
			for _, themeName := range slot.themeNames {
				reference = strictXMLAttributeValueFold(tag, themeName)
				if reference != "" {
					break
				}
			}
			if reference == "" {
				continue
			}
			font := fonts.resolve(reference)
			if font == "" {
				unresolved = true
				continue
			}
			if strictXMLAttributeValueFold(tag, slot.fontName) == "" {
				tag = addStrictXMLAttribute(tag, slot.fontName, font)
			}
			for _, themeName := range slot.themeNames {
				tag = removeStrictXMLAttributeFold(tag, themeName)
			}
		}
		return tag
	})
	return content, unresolved
}

func strictXMLAttributeValueFold(tag, name string) string {
	for _, match := range strictXMLAttributePattern.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 || !strings.EqualFold(match[1], name) {
			continue
		}
		if match[3] != "" {
			return match[3]
		}
		return match[4]
	}
	return ""
}

func addStrictXMLAttribute(tag, name, value string) string {
	insertAt := strings.LastIndex(tag, "/>")
	if insertAt == -1 {
		insertAt = strings.LastIndex(tag, ">")
	}
	if insertAt == -1 {
		return tag
	}
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	return tag[:insertAt] + ` ` + name + `="` + escaped.String() + `"` + tag[insertAt:]
}

func removeStrictXMLAttributeFold(tag, name string) string {
	pattern := regexp.MustCompile(`(?i)\s+` + regexp.QuoteMeta(name) + `\s*=\s*("[^"]*"|'[^']*')`)
	return pattern.ReplaceAllString(tag, "")
}

func mergeRequiredTemplateFonts(
	outputEntries,
	templateEntries map[string][]byte,
	materializedXML []string,
) {
	outputFontTable, ok := outputEntries["word/fontTable.xml"]
	if !ok {
		return
	}
	templateFontTable, ok := templateEntries["word/fontTable.xml"]
	if !ok {
		return
	}

	required := map[string]bool{}
	for _, content := range materializedXML {
		for _, tag := range strictRunFontsPattern.FindAllString(content, -1) {
			for _, attribute := range []string{"w:ascii", "w:hAnsi", "w:eastAsia", "w:cs"} {
				if font := strictXMLAttributeValue(tag, attribute); font != "" {
					required[font] = true
				}
			}
		}
	}
	existing := map[string]bool{}
	for _, block := range strictFontBlockPattern.FindAllString(string(outputFontTable), -1) {
		existing[strictXMLAttributeValue(block, "w:name")] = true
	}
	var additions []string
	for _, block := range strictFontBlockPattern.FindAllString(string(templateFontTable), -1) {
		name := strictXMLAttributeValue(block, "w:name")
		if required[name] && !existing[name] {
			existing[name] = true
			additions = append(additions, block)
		}
	}
	if len(additions) == 0 {
		return
	}
	content := string(outputFontTable)
	if closeAt := strings.LastIndex(content, "</w:fonts>"); closeAt != -1 {
		outputEntries["word/fontTable.xml"] = []byte(content[:closeAt] + strings.Join(additions, "") + content[closeAt:])
	}
}

func strictXMLAttributeValue(tag, name string) string {
	for _, match := range strictXMLAttributePattern.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 || match[1] != name {
			continue
		}
		if match[3] != "" {
			return match[3]
		}
		return match[4]
	}
	return ""
}

func replaceStrictXMLAttribute(tag, name, value string) string {
	pattern := regexp.MustCompile(`(\s+` + regexp.QuoteMeta(name) + `\s*=\s*)("[^"]*"|'[^']*')`)
	return pattern.ReplaceAllString(tag, `${1}"`+value+`"`)
}

func nextFreeRelationshipID(used map[string]bool) string {
	for number := 1; ; number++ {
		candidate := "rId" + strconv.Itoa(number)
		if !used[candidate] {
			return candidate
		}
	}
}

func isHeaderFooterRelationship(rel strictRelationshipPart) bool {
	return strings.Contains(rel.Type, "/header") ||
		strings.Contains(rel.Type, "/footer") ||
		strings.HasPrefix(rel.Target, "header") ||
		strings.HasPrefix(rel.Target, "footer")
}

func mergeDocumentSectionHeaderFooterRefs(outputEntries, templateEntries map[string][]byte, relationshipIDMaps ...map[string]string) {
	relationshipIDs := map[string]string{}
	if len(relationshipIDMaps) > 0 {
		relationshipIDs = relationshipIDMaps[0]
	}
	outputContent, ok := outputEntries["word/document.xml"]
	if !ok {
		return
	}
	templateContent, ok := templateEntries["word/document.xml"]
	if !ok {
		return
	}

	sectionRefs := mapTemplateSectionHeaderFooterRefs(string(outputContent), string(templateContent))
	mergeDocumentSectionHeaderFooterRefsWithPlan(outputEntries, sectionRefs, relationshipIDs)
}

func mergeDocumentSectionHeaderFooterRefsWithPlan(
	outputEntries map[string][]byte,
	sectionRefs [][]string,
	relationshipIDs map[string]string,
) {
	outputContent, ok := outputEntries["word/document.xml"]
	if !ok {
		return
	}
	outputXML := string(outputContent)
	sectPrPattern := regexp.MustCompile(`<w:sectPr(?:[^>]*/>|[\s\S]*?</w:sectPr>)`)
	matches := sectPrPattern.FindAllStringIndex(outputXML, -1)
	if len(matches) == 0 {
		return
	}

	var updated strings.Builder
	lastEnd := 0
	for index, match := range matches {
		updated.WriteString(outputXML[lastEnd:match[0]])
		sectPr := outputXML[match[0]:match[1]]
		refs := []string(nil)
		if index < len(sectionRefs) {
			refs = sectionRefs[index]
		}
		mappedRefs := make([]string, 0, len(refs))
		for _, ref := range refs {
			mappedRefs = append(mappedRefs, remapRelationshipIDs(ref, relationshipIDs))
		}
		replacement, inserted := insertHeaderFooterRefsIntoSectPr(
			stripHeaderFooterReferenceTags(sectPr),
			mappedRefs,
		)
		if !inserted {
			replacement = sectPr
		}
		updated.WriteString(replacement)
		lastEnd = match[1]
	}
	updated.WriteString(outputXML[lastEnd:])
	outputEntries["word/document.xml"] = []byte(updated.String())
}

type strictSectionRole string

const (
	strictSectionUnknown      strictSectionRole = ""
	strictSectionCover        strictSectionRole = "cover"
	strictSectionAbstractCN   strictSectionRole = "abstract_cn"
	strictSectionAbstractEN   strictSectionRole = "abstract_en"
	strictSectionTOC          strictSectionRole = "toc"
	strictSectionBody         strictSectionRole = "body"
	strictSectionReferences   strictSectionRole = "references"
	strictSectionAcknowledged strictSectionRole = "acknowledgements"
	strictSectionAppendix     strictSectionRole = "appendix"
)

type strictSectionDescriptor struct {
	Role strictSectionRole
	Refs []string
}

func mapTemplateSectionHeaderFooterRefs(outputXML, templateXML string) [][]string {
	return mapTemplateSectionHeaderFooterRefsWithVisibleHeaders(outputXML, templateXML, nil)
}

// mapTemplateSectionHeaderFooterRefsWithVisibleHeaders keeps the template's
// section semantics, but never selects an intentionally blank cover/first-page
// header when a visible header exists for the same semantic section.
func mapTemplateSectionHeaderFooterRefsWithVisibleHeaders(outputXML, templateXML string, visibleHeaders map[string]strictSectionRole) [][]string {
	outputSections := describeStrictSections(outputXML)
	templateSections := describeStrictSections(templateXML)
	if len(outputSections) == 0 || len(templateSections) == 0 {
		return nil
	}

	firstByRole := make(map[strictSectionRole]int)
	firstVisibleByRole := make(map[strictSectionRole]int)
	for index, section := range templateSections {
		if _, exists := firstByRole[section.Role]; !exists && section.Role != strictSectionUnknown {
			firstByRole[section.Role] = index
		}
		for _, role := range []strictSectionRole{strictSectionAbstractCN, strictSectionAbstractEN, strictSectionTOC, strictSectionBody, strictSectionReferences, strictSectionAcknowledged, strictSectionAppendix} {
			if _, exists := firstVisibleByRole[role]; !exists && sectionHasHeaderRole(section, visibleHeaders, role) {
				firstVisibleByRole[role] = index
			}
		}
	}
	// The template can contain an instructional/front-matter section and one or
	// more actual body examples.  Picking the first section with role "body"
	// sends the student's post-TOC pages to the front-matter header/footer.
	// Resolve the body section relative to the TOC instead of by a fixed index.
	templateTOCIndex := -1
	for index, section := range templateSections {
		if section.Role == strictSectionTOC {
			templateTOCIndex = index
			break
		}
	}
	frontMatterBodyIndex, hasFrontMatterBody := -1, false
	mainBodyIndex, hasMainBody := -1, false
	for index, section := range templateSections {
		if section.Role != strictSectionBody {
			continue
		}
		if templateTOCIndex >= 0 && index > templateTOCIndex {
			if !hasMainBody {
				mainBodyIndex, hasMainBody = index, true
			}
			continue
		}
		if !hasFrontMatterBody {
			frontMatterBodyIndex, hasFrontMatterBody = index, true
		}
	}
	if !hasMainBody {
		mainBodyIndex, hasMainBody = frontMatterBodyIndex, hasFrontMatterBody
	}
	if visibleIndex, ok := firstVisibleByRole[strictSectionBody]; ok {
		mainBodyIndex, hasMainBody = visibleIndex, true
	}
	outputTOCIndex := -1
	for index, section := range outputSections {
		if section.Role == strictSectionTOC {
			outputTOCIndex = index
			break
		}
	}

	result := make([][]string, len(outputSections))
	frontMatterStage := 0
	for index, section := range outputSections {
		templateIndex, found := firstByRole[section.Role]
		if visibleIndex, ok := firstVisibleByRole[section.Role]; ok && section.Role != strictSectionCover {
			templateIndex, found = visibleIndex, true
		}
		semanticFrontMatter := false
		// Student files often put a section break immediately after the cover
		// and classify the Chinese abstract block as generic body because it
		// contains dates/numbers.  Consume front matter in semantic order
		// before falling back to the template's generic body sample.
		if index > 0 && frontMatterStage == 0 && section.Role == strictSectionBody &&
			(outputTOCIndex < 0 || index < outputTOCIndex) {
			if candidate, ok := firstByRole[strictSectionAbstractCN]; ok {
				templateIndex, found = candidate, true
				frontMatterStage = 1
				semanticFrontMatter = true
			}
		} else if frontMatterStage == 1 && section.Role == strictSectionAbstractCN {
			if candidate, ok := firstByRole[strictSectionAbstractEN]; ok {
				templateIndex, found = candidate, true
				frontMatterStage = 2
				semanticFrontMatter = true
			}
		}
		if !semanticFrontMatter && (section.Role == strictSectionBody || section.Role == strictSectionUnknown) {
			// Before the output TOC, preserve front-matter/cover conventions;
			// after it, use the template's first real body section.
			if outputTOCIndex >= 0 && index > outputTOCIndex {
				templateIndex, found = mainBodyIndex, hasMainBody
			} else {
				templateIndex, found = frontMatterBodyIndex, hasFrontMatterBody
			}
		}
		if !found {
			switch section.Role {
			case strictSectionAbstractEN:
				templateIndex, found = firstByRole[strictSectionAbstractCN]
			case strictSectionReferences, strictSectionAcknowledged, strictSectionAppendix:
				templateIndex, found = mainBodyIndex, hasMainBody
			}
		}
		if !found && index == 0 {
			templateIndex, found = 0, true
		}
		if !found && hasMainBody {
			templateIndex, found = mainBodyIndex, true
		}
		if !found {
			templateIndex = len(templateSections) - 1
		}
		result[index] = append([]string(nil), templateSections[templateIndex].Refs...)
	}
	return result
}

func sectionHasHeaderRole(section strictSectionDescriptor, headerRoles map[string]strictSectionRole, want strictSectionRole) bool {
	if len(headerRoles) == 0 {
		return false
	}
	for _, ref := range section.Refs {
		if !strings.Contains(ref, "<w:headerReference") {
			continue
		}
		if headerRoles[strictXMLAttributeValue(ref, "r:id")] == want {
			return true
		}
	}
	return false
}

func templateVisibleHeaderRoles(entries map[string][]byte) map[string]strictSectionRole {
	rels, ok := entries["word/_rels/document.xml.rels"]
	if !ok {
		return nil
	}
	var relationships strictRelationshipSet
	if xml.Unmarshal(rels, &relationships) != nil {
		return nil
	}
	visible := make(map[string]strictSectionRole)
	for _, rel := range relationships.Relationships {
		if !strings.Contains(rel.Type, "/header") {
			continue
		}
		part, exists := entries[resolveRelationshipPart("word/document.xml", rel.Target)]
		if !exists || strings.TrimSpace(strings.Join(extractDocxTextNodes(string(part)), "")) == "" {
			continue
		}
		visible[rel.ID] = classifyTemplateHeaderRole(strings.Join(extractDocxTextNodes(string(part)), " "))
	}
	return visible
}

func classifyTemplateHeaderRole(text string) strictSectionRole {
	compact := strings.ToLower(strings.Join(strings.Fields(text), ""))
	switch {
	case strings.Contains(compact, "目录") || strings.Contains(compact, "contents"):
		return strictSectionTOC
	case strings.Contains(compact, "摘要"):
		return strictSectionAbstractCN
	case strings.Contains(compact, "abstract"):
		return strictSectionAbstractEN
	case strings.Contains(compact, "参考文献") || strings.Contains(compact, "references"):
		return strictSectionReferences
	case strings.Contains(compact, "致谢") || strings.Contains(compact, "acknowledg"):
		return strictSectionAcknowledged
	case strings.Contains(compact, "附录") || strings.Contains(compact, "appendix"):
		return strictSectionAppendix
	default:
		return strictSectionBody
	}
}

func describeStrictSections(documentXML string) []strictSectionDescriptor {
	sectPrPattern := regexp.MustCompile(`<w:sectPr(?:[^>]*/>|[\s\S]*?</w:sectPr>)`)
	paragraphOpenPattern := regexp.MustCompile(`<w:p[ >]`)
	matches := sectPrPattern.FindAllStringIndex(documentXML, -1)
	if len(matches) == 0 {
		return nil
	}

	sections := make([]strictSectionDescriptor, 0, len(matches))
	previousEnd := 0
	for index, match := range matches {
		contextXML := documentXML[previousEnd:match[0]]
		visibleText := strings.Join(extractDocxTextNodes(contextXML), " ")
		// 段落级分节符（paragraph-level sectPr）的身份判定按来源逐级回退：
		// 1) 同一段落内、sectPr 之后的文字（"参考文献/致谢"等标题段首部 pPr
		//    内嵌 sectPr，标题紧随其后的结构）→ 该标题即新节身份；
		// 2) 同一段落内、sectPr 之前（pPr 前）的文字（"正文在前、sectPr 段尾"
		//    的既有文档结构）→ 该段正文属新节开头；
		// 3) 纯分节符空段（段落自身无文字）→ 新节身份取决于后续节开头文字
		//    （如独立的分节段后紧随"1 绪论"标题段），取 sectPr 之后到下一
		//    sectPr 之前的开头 1920 字。
		// 三种结构覆盖面不同，逐级回退避免把真实文档的纯分节段误判为 body，
		// 也不会把"标题在 pPr 前"的节身份错位到下一节。
		if sectPrSitsInsideParagraphPr(documentXML, match[0]) {
			parStart := -1
			if opens := paragraphOpenPattern.FindAllStringIndex(documentXML[:match[0]], -1); len(opens) > 0 {
				parStart = opens[len(opens)-1][0]
			}
			paraText := ""
			if parStart >= 0 {
				paraText = strings.Join(extractDocxTextNodes(documentXML[parStart:match[0]]), " ")
			}
			tailPara := ""
			if tail := documentXML[match[1]:]; strings.Contains(tail, "</w:p>") {
				tailPara = tail[:strings.Index(tail, "</w:p>")]
			}
			postPara := strings.Join(extractDocxTextNodes(tailPara), " ")
			switch {
			case strings.TrimSpace(postPara) != "":
				visibleText = postPara
			case strings.TrimSpace(paraText) != "":
				visibleText = paraText
			default:
				postEnd := len(documentXML)
				if index+1 < len(matches) {
					postEnd = matches[index+1][0]
				}
				postXML := documentXML[match[1]:postEnd]
				if len(postXML) > 1920 {
					postXML = postXML[:1920]
				}
				if postVisible := strings.Join(extractDocxTextNodes(postXML), " "); strings.TrimSpace(postVisible) != "" {
					visibleText = postVisible
				}
			}
		}
		sectPr := documentXML[match[0]:match[1]]
		sections = append(sections, strictSectionDescriptor{
			Role: classifyStrictSectionRole(visibleText, index),
			Refs: extractHeaderFooterReferenceTags(sectPr),
		})
		previousEnd = match[1]
	}
	return sections
}

// sectPrSitsInsideParagraphPr reports whether the [start,end) sectPr region is
// nested inside a paragraph's w:pPr (i.e. it is a paragraph-level section
// break declaring a new section beginning right after the sectPr content),
// rather than a body-level section properties element.
func sectPrSitsInsideParagraphPr(documentXML string, start int) bool {
	head := documentXML[:start]
	pprStart := strings.LastIndex(head, "<w:pPr>")
	pprEnd := strings.LastIndex(head, "</w:pPr>")
	return pprStart > pprEnd
}

func classifyStrictSectionRole(text string, index int) strictSectionRole {
	if index == 0 {
		return strictSectionCover
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	// A section may contain an English reference title or the word "abstract"
	// many pages after its opening heading.  Section identity belongs to its
	// opening title, not to arbitrary later body text.
	lead := []rune(normalized)
	if len(lead) > 320 {
		lead = lead[:320]
	}
	compact := strings.ReplaceAll(string(lead), " ", "")
	// Match real Unicode section labels before the legacy encoded aliases
	// below. The previous aliases were mojibake and caused all body sections
	// to fall back to the wrong header/footer relationship.
	// 章节身份优先按最明确的专名标题识别：参考文献 / 致谢 / 附录的判据是
	// 章专名本身，比下方"编号+空格+汉字"的宽松章节编号规则更精确。若仍把
	// body 编号规则放在前面，致谢正文开头的自然语言（如"四年 的"）会被当
	// 作数字章节编号误判为正文节，使该节页眉落到绪论而非致谢。
	switch {
	case strings.Contains(compact, "\u76ee\u5f55"):
		return strictSectionTOC
	case strings.Contains(compact, "\u6458\u8981"):
		return strictSectionAbstractCN
	case strings.Contains(compact, "\u53c2\u8003\u6587\u732e"):
		return strictSectionReferences
	case strings.Contains(compact, "\u81f4\u8c22"):
		return strictSectionAcknowledged
	case strings.Contains(compact, "\u9644\u5f55"):
		return strictSectionAppendix
	case strings.Contains(compact, "\u7eea\u8bba") ||
		regexp.MustCompile(`(?m)(^|\s)[1-9]\d?(?:\.\d+)*\s+[\p{Han}]`).MatchString(string(lead)):
		return strictSectionBody
	}
	switch {
	case strings.Contains(compact, "目录") ||
		strings.Contains(string(lead), "table of contents") ||
		regexp.MustCompile(`\bcontents\b`).MatchString(string(lead)):
		return strictSectionTOC
	case strings.Contains(compact, "摘要"):
		return strictSectionAbstractCN
	case regexp.MustCompile(`\babstract\b`).MatchString(string(lead)):
		return strictSectionAbstractEN
	case strings.Contains(compact, "参考文献") ||
		regexp.MustCompile(`\b(references|bibliography)\b`).MatchString(string(lead)):
		return strictSectionReferences
	case strings.Contains(compact, "致谢") ||
		strings.Contains(string(lead), "acknowledg"):
		return strictSectionAcknowledged
	case strings.Contains(compact, "附录") ||
		regexp.MustCompile(`\bappendix\b`).MatchString(string(lead)):
		return strictSectionAppendix
	case strings.Contains(compact, "第") && strings.Contains(compact, "章") ||
		strings.Contains(compact, "绪论") ||
		regexp.MustCompile(`(?m)(^|\s)[1-9]\d?(?:\.\d+)*\s+[\p{Han}A-Za-z]`).MatchString(string(lead)) ||
		regexp.MustCompile(`\b(chapter|body)\b`).MatchString(string(lead)):
		return strictSectionBody
	default:
		return strictSectionUnknown
	}
}

func referencedRelationshipIDs(sectionRefs [][]string) map[string]bool {
	ids := make(map[string]bool)
	for _, refs := range sectionRefs {
		for _, ref := range refs {
			id := strictXMLAttributeValue(ref, "r:id")
			if id != "" {
				ids[id] = true
			}
		}
	}
	return ids
}

func remapRelationshipIDs(xmlText string, relationshipIDs map[string]string) string {
	// Replace each attribute value once. Sequential string replacement is
	// incorrect when the mapping contains a chain such as rId6 -> rId12 and
	// rId12 -> rId19: the first replacement would be remapped a second time,
	// causing a footer reference to resolve to a header relationship.
	quoted := regexp.MustCompile(`r:id="([^"]+)"`)
	xmlText = quoted.ReplaceAllStringFunc(xmlText, func(attribute string) string {
		match := quoted.FindStringSubmatch(attribute)
		if len(match) != 2 {
			return attribute
		}
		if mapped, ok := relationshipIDs[match[1]]; ok {
			return `r:id="` + mapped + `"`
		}
		return attribute
	})
	singleQuoted := regexp.MustCompile(`r:id='([^']+)'`)
	return singleQuoted.ReplaceAllStringFunc(xmlText, func(attribute string) string {
		match := singleQuoted.FindStringSubmatch(attribute)
		if len(match) != 2 {
			return attribute
		}
		if mapped, ok := relationshipIDs[match[1]]; ok {
			return `r:id='` + mapped + `'`
		}
		return attribute
	})
}

func lastSectPrBlock(docXML string) string {
	blocks := extractSectPrBlocks(docXML)
	if len(blocks) == 0 {
		return ""
	}
	return blocks[len(blocks)-1]
}

func extractSectPrBlocks(docXML string) []string {
	re := regexp.MustCompile(`<w:sectPr(?:[^>]*/>|[\s\S]*?</w:sectPr>)`)
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
