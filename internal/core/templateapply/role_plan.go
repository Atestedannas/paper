package templateapply

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/roleclassify"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

var rolePlanParaID = regexp.MustCompile(`(?:w14:|w:)?paraId="([A-Fa-f0-9]+)"`)

type FormatPlanItem struct {
	NodeID     string    `json:"nodeId"`
	Role       string    `json:"role"`
	RuleKey    string    `json:"ruleKey,omitempty"`
	Confidence float64   `json:"confidence"`
	Trusted    bool      `json:"trusted"`
	Apply      bool      `json:"apply"`
	Reason     string    `json:"reason,omitempty"`
	Evidence   []string  `json:"evidence,omitempty"`
	Flow       *FlowPlan `json:"flow,omitempty"`
}

type FlowPlan struct {
	RuleKey               string   `json:"ruleKey,omitempty"`
	PageBreakBefore       bool     `json:"pageBreakBefore,omitempty"`
	BlankParagraphsBefore int      `json:"blankParagraphsBefore,omitempty"`
	SectionBreakType      string   `json:"sectionBreakType,omitempty"`
	ReviewRequired        bool     `json:"reviewRequired,omitempty"`
	Evidence              []string `json:"evidence,omitempty"`
}

type RolePlanValidationIssue struct {
	NodeID   string `json:"nodeId"`
	Role     string `json:"role"`
	Property string `json:"property"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

func BuildRoleFormatPlan(profile *templateprofile.Profile, assignments []roleclassify.Assignment) []FormatPlanItem {
	plan := make([]FormatPlanItem, 0, len(assignments))
	firstBodyHeadingSeen := false
	for _, assignment := range assignments {
		item := FormatPlanItem{
			NodeID: assignment.NodeID, Role: assignment.Role, Confidence: assignment.Confidence, Trusted: assignment.Trusted,
			Evidence: append([]string(nil), assignment.Evidence...),
		}
		if assignment.ReviewReason != "" {
			item.Reason = assignment.ReviewReason
			plan = append(plan, item)
			continue
		}
		if (!assignment.Trusted && assignment.Confidence < 0.85) || assignment.Role == "unknown" {
			item.Reason = "unknown_or_low_confidence_preserve_original"
			plan = append(plan, item)
			continue
		}
		if profile == nil {
			item.Reason = "no_role_rule_preserve_original"
			plan = append(plan, item)
			continue
		}
		item.RuleKey = RoleStyleKey(assignment.Role, assignment.Text)
		if item.RuleKey != "" {
			rule, found := rolePlanStyleRule(profile, assignment.Role, item.RuleKey)
			if found && rule.ReviewRequired {
				item.Reason = "template_style_conflict_preserve_original"
				plan = append(plan, item)
				continue
			}
			item.Apply = found
		}
		sectionKey := assignment.Role
		switch assignment.Role {
		case "heading_1":
			if !firstBodyHeadingSeen {
				if _, exists := profile.Sections["body_start"]; exists {
					sectionKey = "body_start"
				}
				firstBodyHeadingSeen = true
			}
		case "references_title":
			sectionKey = "references_title"
		case "acknowledgements_title":
			sectionKey = "acknowledgements_title"
		case "appendix_title":
			sectionKey = "appendix_title"
		}
		if section, ok := profile.Sections[sectionKey]; ok {
			flow := &FlowPlan{
				RuleKey:               sectionKey,
				PageBreakBefore:       section.PageBreakBefore,
				BlankParagraphsBefore: section.BlankParagraphsBefore,
				SectionBreakType:      section.SectionBreakType,
			}
			if section.DetectedFrom != "" {
				flow.Evidence = append(flow.Evidence, section.DetectedFrom)
			}
			if section.SectionBreak {
				// A section break carries headers, footers, margins and numbering.
				// The profile currently has only its type, so synthesizing it would
				// be destructive. Defer only section cloning; the independently
				// evidenced page break remains safe to apply and mandatory to check.
				flow.ReviewRequired = true
				item.Reason = "section_break_requires_relationship_safe_clone"
			}
			item.Flow = flow
			item.Apply = item.Apply || flow.PageBreakBefore
		}
		if !item.Apply {
			if item.Flow != nil && item.Flow.ReviewRequired {
				item.Reason = "section_break_requires_relationship_safe_clone"
			} else {
				item.Reason = "template_rule_missing_preserve_original"
			}
		}
		plan = append(plan, item)
	}
	return plan
}

// ApplyRoleFormatPlan applies only rules selected by a validated semantic role.
// It addresses paragraphs by paraId; unknown or low-confidence roles are left
// untouched rather than receiving a generic body style.
func ApplyRoleFormatPlan(ctx context.Context, path string, profile *templateprofile.Profile, assignments []roleclassify.Assignment) (int, error) {
	if profile == nil || len(assignments) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	updated, count := applyRoleFormatPlanToDocumentXML(string(content), profile, assignments)
	pkg.Set(documentTarget, []byte(updated))
	if styles, ok := pkg.Get("word/styles.xml"); ok {
		cleaned := removeHeading1AutoNumbering(string(styles))
		if cleaned != string(styles) {
			pkg.Set("word/styles.xml", []byte(cleaned))
		}
	}
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

// CheckRoleFormatPlan is the idempotence gate for paragraph formatting.
// A non-zero result means the first pass did not fully satisfy its own plan.
func CheckRoleFormatPlan(ctx context.Context, path string, profile *templateprofile.Profile, assignments []roleclassify.Assignment) (int, error) {
	if profile == nil || len(assignments) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return 0, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("missing %s", documentTarget)
	}
	_, count := applyRoleFormatPlanToDocumentXML(string(content), profile, assignments)
	return count, nil
}

// ValidateRoleFormatPlan re-reads the saved DOCX and compares every explicit
// template property. Undefined template properties are intentionally ignored.
func ValidateRoleFormatPlan(ctx context.Context, path string, profile *templateprofile.Profile, assignments []roleclassify.Assignment) ([]RolePlanValidationIssue, error) {
	if profile == nil || len(assignments) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	styles, err := templateprofile.ResolveDocumentEffectiveStyles(path)
	if err != nil {
		return nil, err
	}
	pkg, err := ooxmlpkg.Open(path)
	if err != nil {
		return nil, err
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return nil, fmt.Errorf("missing %s", documentTarget)
	}
	paragraphs, indexByID := map[int]string{}, map[string]int{}
	assignments = remapRoleAssignments(string(content), assignments)
	index := -1
	templateProfileDocElementPattern.ReplaceAllStringFunc(string(content), func(raw string) string {
		index++
		if strings.HasPrefix(raw, "<w:p") {
			paragraphs[index] = raw
			if match := rolePlanParaID.FindStringSubmatch(raw); len(match) >= 2 {
				indexByID["P:"+strings.ToUpper(match[1])] = index
			}
		}
		return raw
	})
	plan := BuildRoleFormatPlan(profile, assignments)
	issues := []RolePlanValidationIssue{}
	for i, assignment := range assignments {
		if i >= len(plan) || !plan[i].Apply {
			continue
		}
		item := plan[i]
		actualIndex := assignment.Index
		if stable, found := indexByID[strings.ToUpper(assignment.NodeID)]; found {
			actualIndex = stable
		}
		if item.RuleKey != "" {
			if expected, found := RoleStyleRequirementForText(profile, item.Role, paragraphPlainText(paragraphs[actualIndex])); found {
				issues = append(issues, compareRoleStyle(item, expected, styles[actualIndex])...)
			}
		}
		pageBreak := ooxmlpatch.ParagraphPageBreakBefore(paragraphs[actualIndex])
		if item.Role == "heading_1" && profile.HeadingBlankBefore != nil && isHeadingBlank(paragraphs[actualIndex-1]) {
			pageBreak = pageBreak || ooxmlpatch.ParagraphPageBreakBefore(paragraphs[actualIndex-1])
		}
		if item.Flow != nil && item.Flow.PageBreakBefore && !pageBreak {
			issues = append(issues, validationIssue(item, "pageBreakBefore", "true", "false"))
		}
		if item.Role == "heading_1" && item.Trusted {
			if profile.HeadingBlankBefore != nil && !isHeadingBlank(paragraphs[actualIndex-1]) {
				issues = append(issues, validationIssue(item, "blankParagraphBefore", "1", "0"))
			}
			if profile.HeadingBlankAfter != nil && !isHeadingBlank(paragraphs[actualIndex+1]) {
				issues = append(issues, validationIssue(item, "blankParagraphAfter", "1", "0"))
			}
		}
	}
	return issues, nil
}

func compareRoleStyle(item FormatPlanItem, expected, actual templateprofile.StyleRule) []RolePlanValidationIssue {
	issues := []RolePlanValidationIssue{}
	// English abstract paragraphs are a merged label+body layout handled by
	// splitEnglishAbstractLabel. The whole-paragraph abstract_en rule (bold,
	// 16pt, centered) no longer describes them, so comparing the aggregate
	// would raise a false review_required. Label and body properties are
	// asserted separately by the B4 acceptance check.

	compare := func(property, want, got string) {
		if strings.TrimSpace(want) != "" && !strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(got)) {
			issues = append(issues, validationIssue(item, property, want, got))
		}
	}
	compare("font.eastAsia", expected.FontEastAsia, actual.FontEastAsia)
	compare("font.ascii", expected.FontASCII, actual.FontASCII)
	compare("font.hAnsi", expected.FontHAnsi, actual.FontHAnsi)
	compare("font.cs", expected.FontCS, actual.FontCS)
	compare("fontSizeHalfPt", expected.FontSizeHalfPt, actual.FontSizeHalfPt)
	compare("complexSizeHalfPt", expected.ComplexSizeHalfPt, actual.ComplexSizeHalfPt)
	compare("alignment", expected.Alignment, actual.Alignment)
	compare("line", expected.Line, actual.Line)
	compare("lineRule", expected.LineRule, actual.LineRule)
	compare("beforeTwips", expected.BeforeTwips, actual.BeforeTwips)
	compare("afterTwips", expected.AfterTwips, actual.AfterTwips)
	compare("beforeLines", expected.BeforeLines, actual.BeforeLines)
	compare("afterLines", expected.AfterLines, actual.AfterLines)
	compare("firstLineChars", expected.FirstLineChars, actual.FirstLineChars)
	if expected.FirstLineChars == "" {
		compare("firstLineTwips", expected.FirstLineTwips, actual.FirstLineTwips)
	}
	compareBool := func(property string, set, want, actualSet, got bool) {
		if set && (!actualSet || want != got) {
			issues = append(issues, validationIssue(item, property, fmt.Sprint(want), fmt.Sprint(got)))
		}
	}
	compareBool("bold", expected.BoldSet, expected.Bold, actual.BoldSet, actual.Bold)
	compareBool("italic", expected.ItalicSet, expected.Italic, actual.ItalicSet, actual.Italic)
	compareBool("keepNext", expected.KeepNextSet, expected.KeepNext, actual.KeepNextSet, actual.KeepNext)
	compareBool("keepLines", expected.KeepLinesSet, expected.KeepLines, actual.KeepLinesSet, actual.KeepLines)
	compareBool("widowControl", expected.WidowControlSet, expected.WidowControl, actual.WidowControlSet, actual.WidowControl)
	return issues
}

func validationIssue(item FormatPlanItem, property, expected, actual string) RolePlanValidationIssue {
	return RolePlanValidationIssue{NodeID: item.NodeID, Role: item.Role, Property: property, Expected: expected, Actual: actual}
}

func applyRoleFormatPlanToDocumentXML(documentXML string, profile *templateprofile.Profile, assignments []roleclassify.Assignment) (string, int) {
	assignments = remapRoleAssignments(documentXML, assignments)
	plan := BuildRoleFormatPlan(profile, assignments)
	byID := map[string]FormatPlanItem{}
	byIndex := map[int]FormatPlanItem{}
	for i, assignment := range assignments {
		if i >= len(plan) {
			break
		}
		byID[strings.ToUpper(assignment.NodeID)] = plan[i]
		if assignment.Trusted {
			byIndex[assignment.Index] = plan[i]
		}
	}
	count := 0
	elementIndex := -1
	// bodyStartIndex marks the first trusted chapter heading: 1 绪论 already
	// starts on a fresh page through the TOC section break, so it is the only
	// heading that must not carry an explicit page break of its own.
	bodyStartIndex := -1
	for i, assignment := range assignments {
		if i >= len(plan) {
			break
		}
		if plan[i].Role == "heading_1" && plan[i].Trusted {
			bodyStartIndex = assignment.Index
			break
		}
	}
	updated := templateProfileDocElementPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		elementIndex++
		if strings.HasPrefix(paragraph, "<w:tbl") {
			return paragraph
		}
		item, ok := byIndex[elementIndex]
		if match := rolePlanParaID.FindStringSubmatch(paragraph); len(match) >= 2 {
			if stable, found := byID["P:"+strings.ToUpper(match[1])]; found {
				item, ok = stable, true
			}
		}
		if !ok || !item.Apply {
			return paragraph
		}
		next := paragraph
		// Add the semantic style before the property writer, so its canonical
		// OOXML ordering is produced in the first pass as well as later passes.
		if item.Role == "heading_1" && item.Trusted {
			next = applyHeading1Style(next)
			next = removeHeadingNumbering(next)
			// CQIE 基本要求：一级标题（章）之间应换页。首个正文标题
			// （1 绪论）由目录节的分节符保证另起页，其余章标题在此显式
			// 声明 pageBreakBefore，不再依赖模板手工空段或正文自然流分页。
			// 幂等：属性写入器对已存在的 <w:pageBreakBefore/> 原样保留。
			if elementIndex != bodyStartIndex {
				next, _ = ooxmlpatch.ApplyParagraphProperties(next, ooxmlpatch.ParagraphPropertiesSpec{PageBreakBefore: true})
			}
		}
		if item.RuleKey != "" {
			if rule, found := rolePlanStyleRule(profile, item.Role, item.RuleKey); found {
				if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
					next = applyParagraphStyle(next, style)
				}
			}
		}
		// English abstract: templates commonly merge the "Abstract:" label and
		// the body into a single paragraph (role=abstract_en). Apply the label
		// emphasis (bold, 16pt) from the abstract_en title rule only to the
		// label run, and lay the remaining body out with the abstract_en_body
		// rule (12pt, not bold, justified) instead of bolding and centering the
		// whole paragraph. Idempotent for the repair gate.
		if item.Role == "abstract_en" {
			if split, ok := splitEnglishAbstractLabel(next, profile, item.RuleKey); ok {
				next = split
			}
		}
		// CQIE 规范：中英文摘要必须分页。英文摘要（Abstract 标签段）需另起
		// 新页，与中文摘要/中文关键词分离。以 Abstract 段为定位锚点，幂等
		// 设置 pageBreakBefore：已存在时 upsert 不变，重复运行 gate 安全。
		if item.Role == "abstract_en" && strings.HasPrefix(strings.TrimSpace(paragraphPlainText(next)), "Abstract") {
			next, _ = ooxmlpatch.ApplyParagraphProperties(next, ooxmlpatch.ParagraphPropertiesSpec{PageBreakBefore: true})
		}
		if item.Role == "keywords_en" {
			next = applyInlineLabel(next, profile, "keywords_en", "Key words:")
			next = applyInlineLabel(next, profile, "keywords_en", "Keywords:")
		}
		if item.Role == "keywords_cn" {
			next = applyInlineLabel(next, profile, "keywords_cn", "关键词：")
			next = applyInlineLabel(next, profile, "keywords_cn", "关键词:")
		}
		if item.Flow != nil && item.Flow.PageBreakBefore {
			next, _ = ooxmlpatch.ApplyParagraphProperties(next, ooxmlpatch.ParagraphPropertiesSpec{PageBreakBefore: true})
		}
		if next != paragraph {
			count++
		}
		return next
	})
	updated = applyHeadingBlanks(updated, profile, assignments)
	if updated == documentXML {
		return updated, 0
	}
	if count == 0 {
		count = 1
	}
	return updated, count
}

// RoleStyleRequirement 导出 rolePlanStyleRule，供工作流日志层获取"命中规则的模板要求值"。
// 仅用于读取/展示，不参与 apply/validation 判定，故对其判定逻辑零影响。
func RoleStyleRequirement(profile *templateprofile.Profile, role, key string) (templateprofile.StyleRule, bool) {
	return rolePlanStyleRule(profile, role, key)
}

func rolePlanStyleRule(profile *templateprofile.Profile, role, key string) (templateprofile.StyleRule, bool) {
	return resolveTemplateProfileStyle(profile.Styles, key)
}

func applyHeading1Style(paragraph string) string {
	const style = `<w:pStyle w:val="Heading1"/>`
	// Word may serialize pStyle either as a self-closing element or with an
	// explicit closing tag.  Matching both forms prevents a second pass from
	// appending another style element and breaking the repair idempotence gate.
	pStyle := regexp.MustCompile(`(?s)<w:pStyle\b[^>]*/>|<w:pStyle\b[^>]*>.*?</w:pStyle>`)
	if pStyle.MatchString(paragraph) {
		return pStyle.ReplaceAllString(paragraph, style)
	}
	if strings.Contains(paragraph, "</w:pPr>") {
		return strings.Replace(paragraph, "</w:pPr>", style+"</w:pPr>", 1)
	}
	open := strings.Index(paragraph, ">")
	if open < 0 {
		return paragraph
	}
	return paragraph[:open+1] + "<w:pPr>" + style + "</w:pPr>" + paragraph[open+1:]
}

// Thesis chapter numbers are literal student text (for example "1 绪论").
// Template Heading1 also carries an empty Word list definition, which shifts
// centered titles even though the paragraph itself is set to center.
func removeHeadingNumbering(paragraph string) string {
	return regexp.MustCompile(`(?s)<w:numPr\b.*?</w:numPr>`).ReplaceAllString(paragraph, "")
}

func removeHeading1AutoNumbering(stylesXML string) string {
	styles := regexp.MustCompile(`(?s)<w:style\b[^>]*>.*?</w:style>`)
	return styles.ReplaceAllStringFunc(stylesXML, func(style string) string {
		if !strings.Contains(style, `w:styleId="Heading1"`) {
			return style
		}
		return regexp.MustCompile(`(?s)<w:numPr\b.*?</w:numPr>`).ReplaceAllString(style, "")
	})
}

func roleToProfileKey(role string) string {
	switch role {
	case "heading_1", "heading_2", "heading_3", "heading_4", "body", "references", "references_title", "acknowledgements", "acknowledgements_title", "appendix_title", "appendix", "abstract_title", "abstract_cn", "abstract_body", "abstract_en", "abstract_en_body", "keywords_cn", "keywords_en", "toc_title", "toc_entry", "cover_title", "cover_date", "title", "figure_caption", "table_caption":
		if role == "acknowledgements" {
			// An acknowledgement body paragraph must use the content rule
			// (Song 12pt, not bold) rather than the acknowledgements_title
			// heading rule (black 16pt bold centered) that only the standalone
			// "致 谢" section label should get. Prefer a dedicated body style,
			// falling back to the generic body rule.
			return preferStyleKey("acknowledgements_body", "body")
		}
		if role == "abstract_body" {
			return "abstract_body"
		}
		if role == "abstract_title" {
			return "abstract_cn"
		}
		if role == "abstract_cn" {
			// A Chinese abstract body paragraph (often a merged
			// "摘要：……" line) must use the abstract body rule (Song 12pt),
			// not the bold black heading rule that only the standalone
			// "摘 要" label should get. This matches V2Abstract→abstract_body.
			return "abstract_body"
		}
		if role == "keywords_cn" {
			// A Chinese keywords paragraph ("关键词：……") must carry the
			// content rule (Song 12pt) extracted from the trailing content,
			// falling back to the label rule when the template only has a
			// separate label style. Matches V2Keywords→{keywords_cn_body,keywords_cn}.
			return preferStyleKey("keywords_cn_body", "keywords_cn")
		}
		if role == "keywords_en" {
			return preferStyleKey("keywords_en_body", "keywords_en")
		}
		if role == "abstract_en_body" {
			return "abstract_en_body"
		}
		if role == "body" {
			return "body"
		}
		if role == "references" {
			return "references"
		}
		if role == "appendix_title" {
			return preferStyleKey("appendix_title", "heading_1")
		}
		if role == "appendix" {
			return preferStyleKey("appendix", "body")
		}
		return role
	default:
		return ""
	}
}

var rolePlanRunTextPattern = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)

func paragraphPlainText(xml string) string {
	var b strings.Builder
	for _, m := range rolePlanRunTextPattern.FindAllStringSubmatch(xml, -1) {
		b.WriteString(m[1])
	}
	return b.String()
}

// splitEnglishAbstractLabel rewrites a merged "Abstract: <body>" paragraph
// (role=abstract_en) so the label run keeps the template abstract_en title
// emphasis (bold 16pt) while the remaining body runs follow the
// abstract_en_body rule (12pt, not bold, justified) and the paragraph-level
// alignment switches from centered to justified. It is idempotent: a paragraph
// already laid out this way is returned unchanged (repair gate safe).
func splitEnglishAbstractLabel(paragraph string, profile *templateprofile.Profile, ruleKey string) (string, bool) {
	if profile == nil {
		return paragraph, false
	}
	trimmed := strings.TrimSpace(paragraphPlainText(paragraph))
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "Abstract:"))
	if !strings.HasPrefix(trimmed, "Abstract:") || body == "" {
		// Standalone "Abstract:" label or a non-abstract paragraph: keep the
		// whole-paragraph abstract_en title rule.
		return paragraph, false
	}
	bodyRule, bodyOK := rolePlanStyleRule(profile, "abstract_en_body", "abstract_en_body")
	if !bodyOK {
		return paragraph, false
	}
	bodyStyle, bodyValid := paragraphStyleFromTemplateProfile(bodyRule)
	if !bodyValid {
		return paragraph, false
	}
	next := applyParagraphStyle(paragraph, bodyStyle)
	next = applyInlineLabel(next, profile, "abstract_en", "Abstract:")
	return next, next != paragraph
}
