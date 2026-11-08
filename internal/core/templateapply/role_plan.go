package templateapply

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/documentrole"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpatch"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/roleclassify"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

type FormatPlanItem struct {
	TOCTarget            *paperast.TOCTargetRelation   `json:"tocTarget,omitempty"`
	ChapterGroup         *paperast.ChapterHeadingGroup `json:"chapterGroup,omitempty"`
	ParagraphOnly        bool                          `json:"paragraphOnly,omitempty"`
	PreserveRunSemantics bool                          `json:"preserveRunSemantics,omitempty"`
	ReviewProperties     []string                      `json:"reviewProperties,omitempty"`
	NodeID               string                        `json:"nodeId"`
	Role                 string                        `json:"role"`
	RuleKey              string                        `json:"ruleKey,omitempty"`
	Confidence           float64                       `json:"confidence"`
	Trusted              bool                          `json:"trusted"`
	Apply                bool                          `json:"apply"`
	Reason               string                        `json:"reason,omitempty"`
	Evidence             []string                      `json:"evidence,omitempty"`
	Flow                 *FlowPlan                     `json:"flow,omitempty"`
}

type FlowPlan struct {
	RuleKey               string   `json:"ruleKey,omitempty"`
	KeepNext              bool     `json:"keepNext,omitempty"`
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
	for assignmentIndex, assignment := range assignments {
		item := FormatPlanItem{
			TOCTarget:    assignment.TOCTarget,
			ChapterGroup: assignment.ChapterGroup,
			NodeID:       assignment.NodeID, Role: assignment.Role, Confidence: assignment.Confidence, Trusted: assignment.Trusted,
			Evidence: append([]string(nil), assignment.Evidence...),
		}
		item.ParagraphOnly = assignment.Role == "body" && slices.Contains(assignment.Evidence, "ooxml:inline_math_body_prose")
		if item.ParagraphOnly {
			item.ReviewProperties = []string{"runFormat"}
		}
		item.PreserveRunSemantics = !item.ParagraphOnly && mixedBodyEligible(profile, assignment)
		if assignment.ReviewReason != "" && !item.PreserveRunSemantics {
			item.Reason = assignment.ReviewReason
			plan = append(plan, item)
			continue
		}
		if (!item.PreserveRunSemantics && !assignment.Trusted && assignment.Confidence < 0.85) || assignment.Role == documentrole.Unknown || !documentrole.Valid(assignment.Role) {
			item.Reason = "unknown_or_low_confidence_preserve_original"
			plan = append(plan, item)
			continue
		}
		if profile == nil {
			item.Reason = "no_role_rule_preserve_original"
			plan = append(plan, item)
			continue
		}
		item.RuleKey = roleStyleKeyForAssignment(profile, assignment)
		if item.RuleKey != "" {
			rule, found := rolePlanStyleRule(profile, assignment.Role, item.RuleKey)
			if item.ParagraphOnly {
				rule = templateprofile.FormulaParagraphStyle(rule)
			}
			if item.PreserveRunSemantics {
				rule = mixedBodyRule(rule)
			}
			rule, blocked := templateprofile.ExecutableStyle(rule)
			item.ReviewProperties = append(item.ReviewProperties, blocked...)
			if found && rule.ReviewRequired {
				item.Reason = "template_style_conflict_preserve_original"
				plan = append(plan, item)
				continue
			}
			_, executable := paragraphStyleFromTemplateProfile(rule)
			item.Apply = found && executable
			if len(item.ReviewProperties) > 0 {
				item.Reason = "unresolved_properties_preserved"
			}
		}
		sectionKey := assignment.Role
		if sectionKey == "abstract_title" {
			sectionKey = "abstract_cn"
		} else if sectionKey == "abstract_en_title" {
			sectionKey = "abstract_en"
		}
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
				ReviewRequired:        section.ReviewRequired,
			}
			if section.DetectedFrom != "" {
				flow.Evidence = append(flow.Evidence, section.DetectedFrom)
			}
			if section.ReviewReason != "" {
				flow.Evidence = append(flow.Evidence, section.ReviewReason,
					fmt.Sprintf("observed_blank_paragraphs_before:%d", section.ObservedBlankParagraphsBefore))
			}
			if assignment.ChapterGroup != nil && assignment.ChapterGroup.PrimaryNodeID != assignment.NodeID {
				flow.PageBreakBefore, flow.SectionBreakType, flow.BlankParagraphsBefore = false, "", 0
				flow.Evidence = append(flow.Evidence, "chapter_group:"+assignment.ChapterGroup.ID)
			} else if flow.PageBreakBefore && strings.HasPrefix(section.DetectedFrom, "explicit_requirement:") && unresolvedBilingualHeadingBoundary(assignments, assignmentIndex) {
				flow.PageBreakBefore, flow.SectionBreakType, flow.BlankParagraphsBefore = false, "", 0
				flow.ReviewRequired = true
				flow.Evidence = append(flow.Evidence, "adjacent_bilingual_heading_boundary_unresolved")
			}
			item.Flow = flow
			// A section break is synthesized relationship-safely: the new section
			// inherits the previous section's running headers, footers and numbering
			// because no headerReference/footerReference is written, so only the
			// break type and page setup are copied from the template.
			item.Apply = item.Apply || flow.PageBreakBefore || section.SectionBreak || section.BlankParagraphsBefore > 0
		}
		if chapterGroupContinues(assignments, assignmentIndex) {
			if item.Flow == nil {
				item.Flow = &FlowPlan{RuleKey: "chapter_group"}
			}
			item.Flow.KeepNext = true
			item.Flow.Evidence = append(item.Flow.Evidence, assignment.ChapterGroup.Evidence...)
		}
		if bindTOCTitleToEntry(profile, assignments, assignmentIndex) {
			if item.Flow == nil {
				item.Flow = &FlowPlan{RuleKey: "toc_title_binding"}
			}
			item.Flow.KeepNext = true
			item.Flow.Evidence = append(item.Flow.Evidence, "context:trusted_toc_title_followed_by_first_entry")
		}
		if !item.Apply {
			item.Reason = "template_rule_missing_preserve_original"
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
	updated, count := applyRoleFormatPlanToDocumentXML(string(content), profile, assignments, figureEffectiveImageStyles(path, string(content)))
	pkg.Set(documentTarget, []byte(updated))
	if err := pkg.Write(path); err != nil {
		return 0, err
	}
	return count, nil
}

func appliesHeading1(profile *templateprofile.Profile, assignments []roleclassify.Assignment) bool {
	for _, item := range BuildRoleFormatPlan(profile, assignments) {
		if item.Role == "heading_1" && item.Apply && item.Trusted && len(item.ReviewProperties) == 0 {
			return true
		}
	}
	return false
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
	_, count := applyRoleFormatPlanToDocumentXML(string(content), profile, assignments, figureEffectiveImageStyles(path, string(content)))
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
	styleContent, _ := pkg.Get("word/styles.xml")
	logicalAlignmentIsLTR := !possibleRTLParagraph(string(content)) && !possibleRTLParagraph(string(styleContent))
	paragraphs, indexByID := map[int]string{}, map[string]int{}
	assignments = remapRoleAssignments(string(content), assignments)
	index := -1
	templateProfileDocElementPattern.ReplaceAllStringFunc(string(content), func(raw string) string {
		index++
		if strings.HasPrefix(raw, "<w:p") {
			paragraphs[index] = raw
			if id := ooxmlpkg.ParagraphParaID(raw); id != "" {
				indexByID["P:"+strings.ToUpper(id)] = index
			}
		}
		return raw
	})
	paragraphs[index+1] = terminalSectionProperties(string(content))
	captionBindings := tableCaptionBindingObservations(string(content), profile, assignments)
	plan := BuildRoleFormatPlan(profile, assignments)
	issues := []RolePlanValidationIssue{}
	for i, assignment := range assignments {
		if assignment.ReviewReason == "missing_or_ambiguous_target_preserve_original" {
			issues = append(issues, RolePlanValidationIssue{NodeID: assignment.NodeID, Role: assignment.Role, Property: "targetNode", Expected: assignment.NodeID, Actual: "missing_or_ambiguous"})
			continue
		}
		if i >= len(plan) {
			continue
		}
		item := plan[i]
		actualIndex := assignment.Index
		if stable, found := indexByID[strings.ToUpper(assignment.NodeID)]; found {
			actualIndex = stable
		}
		binding, hasBinding := captionBindings[actualIndex]
		if hasBinding && !binding.KeepNext {
			issues = append(issues, validationIssue(item, "keepNext", "true", "missing_direct_keep_next"))
		}
		if item.Flow != nil && item.Flow.KeepNext && (!styles[actualIndex].KeepNextSet || !styles[actualIndex].KeepNext) {
			property := "tocTitleKeepNext"
			if item.ChapterGroup != nil {
				property = "chapterGroupKeepNext"
			}
			issues = append(issues, validationIssue(item, property, "true", "false"))
		}
		if !item.Apply {
			continue
		}
		if item.PreserveRunSemantics && !safeMixedBody(paragraphs[actualIndex]) {
			issues = append(issues, validationIssue(item, "mixedRunSafety", "plain_text_runs", "requires_review"))
			continue
		}
		if item.RuleKey != "" {
			if expected, found := rolePlanStyleRule(profile, item.Role, item.RuleKey); found {
				if item.ParagraphOnly {
					expected = templateprofile.FormulaParagraphStyle(expected)
				}
				if item.PreserveRunSemantics {
					expected = mixedBodyRule(expected)
				}
				actual := styles[actualIndex]
				if hasBinding || (item.Flow != nil && item.Flow.KeepNext) {
					// The binding check above is independent of sampled typography;
					// TOC titles may satisfy it through effective inherited formatting.
					expected.KeepNextSet = false
				}
				if logicalAlignmentIsLTR {
					if actual.Alignment == "start" {
						actual.Alignment = "left"
					}
					if actual.Alignment == "end" {
						actual.Alignment = "right"
					}
					if expected.Alignment == "start" {
						expected.Alignment = "left"
					}
					if expected.Alignment == "end" {
						expected.Alignment = "right"
					}
				}
				issues = append(issues, compareRoleStyle(item, expected, actual)...)
			}
		}
		pageBreak := hasRolePageBoundary(paragraphs, actualIndex)
		if item.Flow != nil && item.Flow.PageBreakBefore && !pageBreak {
			issues = append(issues, validationIssue(item, "pageBreakBefore", "true", "false"))
		}
		if item.Role == "heading_1" && item.Trusted {
			if profile.HeadingBlankBefore != nil && chapterGroupFirst(assignment) && !isHeadingBlank(paragraphs[actualIndex-1]) {
				issues = append(issues, validationIssue(item, "blankParagraphBefore", "1", "0"))
			}
			if profile.HeadingBlankAfter != nil && chapterGroupLast(assignment) && !isHeadingBlank(paragraphs[actualIndex+1]) {
				issues = append(issues, validationIssue(item, "blankParagraphAfter", "1", "0"))
			}
		}
	}
	return issues, nil
}

func compareRoleStyle(item FormatPlanItem, expected, actual templateprofile.StyleRule) []RolePlanValidationIssue {
	expected, _ = templateprofile.ExecutableStyle(expected)
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
	compareFont := func(property, slot, want, got string) {
		if item.PreserveRunSemantics && templateprofile.FontSlotDefinitelyUnused(slot, actual.RunStyles) {
			return
		}
		compare(property, want, got)
	}
	compareFont("font.eastAsia", "FontEastAsia", expected.FontEastAsia, actual.FontEastAsia)
	compareFont("font.ascii", "FontASCII", expected.FontASCII, actual.FontASCII)
	compareFont("font.hAnsi", "FontHAnsi", expected.FontHAnsi, actual.FontHAnsi)
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
	for _, pair := range []struct{ name, chars, twips, actualChars, actualTwips string }{
		{"left", expected.LeftChars, expected.LeftTwips, actual.LeftChars, actual.LeftTwips},
		{"right", expected.RightChars, expected.RightTwips, actual.RightChars, actual.RightTwips},
		{"hanging", expected.HangingChars, expected.HangingTwips, actual.HangingChars, actual.HangingTwips},
	} {
		if pair.chars != "" {
			compare(pair.name+"Chars", pair.chars, pair.actualChars)
		} else if pair.twips != "" {
			compare(pair.name+"Twips", pair.twips, pair.actualTwips)
			if pair.actualChars != "" {
				issues = append(issues, validationIssue(item, pair.name+"Unit", "twips", "inherited_or_direct_chars_override"))
			}
		}
	}
	if expected.HangingChars == "" && expected.HangingTwips == "" {
		compare("firstLineChars", expected.FirstLineChars, actual.FirstLineChars)
		if expected.FirstLineChars == "" {
			compare("firstLineTwips", expected.FirstLineTwips, actual.FirstLineTwips)
		}
	}
	// Undeclared off and unresolved effective formatting are different states.
	// Mixed runs, invalid XML values and missing inheritance cannot prove a
	// false requirement. Keep the resolver's reason in the verification result.
	compareBool := func(property string, set, want, actualSet, got bool, evidence string) {
		if !set {
			return
		}
		if !actualSet && evidence != "" && evidence != "no_visible_text" {
			issues = append(issues, validationIssue(item, property, fmt.Sprint(want), evidence))
			return
		}
		if actualSet {
			if want != got {
				issues = append(issues, validationIssue(item, property, fmt.Sprint(want), fmt.Sprint(got)))
			}
			return
		}
		if want {
			issues = append(issues, validationIssue(item, property, fmt.Sprint(want), fmt.Sprint(got)))
		}
	}
	compareBool("bold", expected.BoldSet, expected.Bold, actual.BoldSet, actual.Bold, actual.BoldEvidence)
	compareBool("italic", expected.ItalicSet, expected.Italic, actual.ItalicSet, actual.Italic, actual.ItalicEvidence)
	// Pagination defaults differ by property. Only a resolved declaration can
	// prove an explicit template requirement; missing/invalid evidence cannot.
	paginationEvidence := func(name string, set bool) string {
		e := actual.PropertyEvidence[name]
		if e.State != "" && e.State != "resolved" {
			return e.State + ":" + e.Source
		}
		if !set {
			return "unspecified:no_declaration"
		}
		return ""
	}
	compareBool("keepNext", expected.KeepNextSet, expected.KeepNext, actual.KeepNextSet, actual.KeepNext, paginationEvidence("KeepNext", actual.KeepNextSet))
	compareBool("keepLines", expected.KeepLinesSet, expected.KeepLines, actual.KeepLinesSet, actual.KeepLines, paginationEvidence("KeepLines", actual.KeepLinesSet))
	compareBool("widowControl", expected.WidowControlSet, expected.WidowControl, actual.WidowControlSet, actual.WidowControl, paginationEvidence("WidowControl", actual.WidowControlSet))
	return issues
}

func validationIssue(item FormatPlanItem, property, expected, actual string) RolePlanValidationIssue {
	return RolePlanValidationIssue{NodeID: item.NodeID, Role: item.Role, Property: property, Expected: expected, Actual: actual}
}

func applyRoleFormatPlanToDocumentXML(documentXML string, profile *templateprofile.Profile, assignments []roleclassify.Assignment, figureStyles ...figureLayoutStyles) (string, int) {
	originalXML := documentXML
	// Establish the target section before checking pagination, so an odd/next
	// page start is not followed by an unnecessary direct paragraph page break.
	documentXML, sectionCount := applySectionBreaksToDocumentXML(documentXML, profile, assignments)
	assignments = remapRoleAssignments(documentXML, assignments)
	plan := BuildRoleFormatPlan(profile, assignments)
	byID := map[string]FormatPlanItem{}
	byIndex := map[int]FormatPlanItem{}
	for i, assignment := range assignments {
		if i >= len(plan) {
			break
		}
		byID[strings.ToUpper(assignment.NodeID)] = plan[i]
		if assignment.Trusted || plan[i].PreserveRunSemantics {
			byIndex[assignment.Index] = plan[i]
		}
	}
	count := sectionCount
	elementIndex := -1
	paragraphs := map[int]string{}
	elements := templateProfileDocElementPattern.FindAllString(documentXML, -1)
	for i, raw := range elements {
		if strings.HasPrefix(raw, "<w:p") {
			paragraphs[i] = raw
		}
	}
	paragraphs[len(elements)] = terminalSectionProperties(documentXML)
	captionBindings := tableCaptionBindingObservations(documentXML, profile, assignments)
	updated := templateProfileDocElementPattern.ReplaceAllStringFunc(documentXML, func(paragraph string) string {
		elementIndex++
		if strings.HasPrefix(paragraph, "<w:tbl") {
			return paragraph
		}
		item, ok := byIndex[elementIndex]
		if id := ooxmlpkg.ParagraphParaID(paragraph); id != "" {
			if stable, found := byID["P:"+strings.ToUpper(id)]; found {
				item, ok = stable, true
			}
		}
		if !ok || !item.Apply {
			// A proven table association is independent of missing or disputed
			// font properties; keep the caption attached without restyling it.
			if _, hasBinding := captionBindings[elementIndex]; hasBinding || (item.Flow != nil && item.Flow.KeepNext) {
				next, _ := applyLegacyParagraphSpec(paragraph, ooxmlpatch.ParagraphPropertiesSpec{KeepNext: true, KeepNextSet: true})
				if next != paragraph {
					count++
				}
				return next
			}
			return paragraph
		}
		next := paragraph
		// Preserve the student's style reference and inherited numbering. A
		// template's style ID is not necessarily defined in the target package.
		// Direct properties implement the known rule; outline records the role.
		if item.Role == "heading_1" && item.Trusted {
			// The target package may not define Heading1 (or may define it
			// without an outline). Preserve the recognized structural role
			// explicitly so unnumbered chapters survive the next parse.
			next, _ = applyLegacyParagraphSpec(next, ooxmlpatch.ParagraphPropertiesSpec{OutlineLevel: 0, OutlineLevelSet: true})
		}
		if item.RuleKey != "" {
			if rule, found := rolePlanStyleRule(profile, item.Role, item.RuleKey); found {
				if item.ParagraphOnly {
					rule = templateprofile.FormulaParagraphStyle(rule)
				}
				if item.PreserveRunSemantics {
					rule = mixedBodyRule(rule)
				}
				if style, valid := paragraphStyleFromTemplateProfile(rule); valid {
					if item.PreserveRunSemantics {
						next = applyMixedBodyStyle(next, style)
					} else if item.ParagraphOnly || item.Role == "formula" || item.Role == "formula_number" {
						next = applyParagraphProperties(next, style)
					} else {
						next = applyParagraphStyle(next, style)
					}
					if item.Role == "toc_entry" {
						next = resetTOCPointSpacingUnits(next, rule)
					}
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
		if item.Role == "keywords_en" {
			next = applyInlineLabel(next, profile, "keywords_en", "Key words:")
			next = applyInlineLabel(next, profile, "keywords_en", "Keywords:")
		}
		if item.Role == "keywords_cn" {
			next = applyInlineLabel(next, profile, "keywords_cn", "关键词：")
			next = applyInlineLabel(next, profile, "keywords_cn", "关键词:")
		}
		if item.Flow != nil && item.Flow.PageBreakBefore && !hasRolePageBoundary(paragraphs, elementIndex) {
			next, _ = applyLegacyParagraphSpec(next, ooxmlpatch.ParagraphPropertiesSpec{PageBreakBefore: true})
		}
		if _, hasBinding := captionBindings[elementIndex]; hasBinding || (item.Flow != nil && item.Flow.KeepNext) {
			next, _ = applyLegacyParagraphSpec(next, ooxmlpatch.ParagraphPropertiesSpec{KeepNext: true, KeepNextSet: true})
		}
		if next != paragraph {
			count++
		}
		return next
	})
	updated = applyHeadingBlanks(updated, profile, assignments)
	updated, blankCount := applyBlankParagraphsBeforeToDocumentXML(updated, profile, assignments)
	count += blankCount
	var targetFigureStyles figureLayoutStyles
	if len(figureStyles) == 1 {
		targetFigureStyles = figureStyles[0]
	}
	updated, figureCount := applyFigureCaptionsBelow(updated, profile, assignments, targetFigureStyles)
	count += figureCount
	if updated == originalXML {
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
	rule, found := resolveTemplateProfileStyle(profile.Styles, key)
	if role == "formula" || role == "formula_number" {
		rule = templateprofile.FormulaParagraphStyle(rule)
	}
	return rule, found
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
	if role == "abstract_en_title" {
		return "abstract_en"
	}
	switch role {
	case "formula", "formula_number":
		return role
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
