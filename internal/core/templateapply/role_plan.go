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
		item.RuleKey = roleToProfileKey(assignment.Role)
		if item.RuleKey != "" {
			_, item.Apply = resolveTemplateProfileStyle(profile.Styles, item.RuleKey)
		}
		sectionKey := ""
		switch assignment.Role {
		case "heading_1":
			if !firstBodyHeadingSeen {
				sectionKey = "body_start"
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
				// be destructive. Keep it visible in the plan for manual review.
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
			if expected, found := resolveTemplateProfileStyle(profile.Styles, item.RuleKey); found {
				issues = append(issues, compareRoleStyle(item, expected, styles[actualIndex])...)
			}
		}
		if item.Flow != nil && !item.Flow.ReviewRequired && item.Flow.PageBreakBefore && !strings.Contains(paragraphs[actualIndex], "<w:pageBreakBefore") {
			issues = append(issues, validationIssue(item, "pageBreakBefore", "true", "false"))
		}
	}
	return issues, nil
}

func compareRoleStyle(item FormatPlanItem, expected, actual templateprofile.StyleRule) []RolePlanValidationIssue {
	issues := []RolePlanValidationIssue{}
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
		}
		if item.RuleKey != "" {
			if rule, found := resolveTemplateProfileStyle(profile.Styles, item.RuleKey); found {
				if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
					next = applyParagraphStyle(next, style)
				}
			}
		}
		// CQIE requires one blank 20pt line above and below every chapter title.
		// This is a school rule, not an incidental template sample value.
		if item.Role == "heading_1" && item.Trusted {
			next, _ = ooxmlpatch.ApplyParagraphProperties(next, ooxmlpatch.ParagraphPropertiesSpec{BeforeTwips: 400, AfterTwips: 400})
		}
		if item.Flow != nil && !item.Flow.ReviewRequired && item.Flow.PageBreakBefore {
			next, _ = ooxmlpatch.ApplyParagraphProperties(next, ooxmlpatch.ParagraphPropertiesSpec{PageBreakBefore: true})
		}
		if next != paragraph {
			count++
		}
		return next
	})
	return updated, count
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
			return "acknowledgements_title"
		}
		if role == "abstract_body" {
			return "abstract_body"
		}
		if role == "abstract_title" {
			return "abstract_cn"
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
