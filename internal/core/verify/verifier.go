package verify

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/cqrwst"
	"github.com/paper-format-checker/backend/internal/core/goldenregression"
	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
	"github.com/paper-format-checker/backend/internal/core/paperast"
	"github.com/paper-format-checker/backend/internal/core/renderverify"
	"github.com/paper-format-checker/backend/internal/core/repaircontract"
	"github.com/paper-format-checker/backend/internal/core/templatecontract"
	"github.com/paper-format-checker/backend/internal/core/templateprofile"
)

const documentTarget = "word/document.xml"

var placeholderPattern = regexp.MustCompile(`\{\{[^{}]+\}\}`)

type Issue struct {
	Kind     string
	Severity string
	Message  string
	Target   string
}

type Result struct {
	Passed           bool                     `json:"passed"`
	ComplianceStatus string                   `json:"compliance_status"`
	ComplianceReason string                   `json:"compliance_reason"`
	FatalIssues      []Issue                  `json:"fatal_issues,omitempty"`
	RepairableIssues []Issue                  `json:"repairable_issues,omitempty"`
	Warnings         []Issue                  `json:"warnings,omitempty"`
	RenderResult     *renderverify.Result     `json:"render_result,omitempty"`
	GoldenRegression *goldenregression.Result `json:"golden_regression,omitempty"`
}

type Verifier struct {
	templateProfile *templateprofile.Profile
	closure         *ClosureArtifacts
	renderOptions   *renderverify.Options
	goldenPath      string
	skipCQRWST      bool
}

type ClosureArtifacts struct {
	TemplateRules  templatecontract.RuleSet
	PaperAST       paperast.Snapshot
	RepairContract repaircontract.Contract
}

func NewVerifier() *Verifier {
	return &Verifier{}
}

func NewVerifierWithTemplateProfile(profile *templateprofile.Profile) *Verifier {
	return &Verifier{templateProfile: profile}
}

func NewVerifierWithTemplateProfileAndClosure(profile *templateprofile.Profile, rules templatecontract.RuleSet, ast paperast.Snapshot, contract repaircontract.Contract) *Verifier {
	verifier := &Verifier{
		templateProfile: profile,
		closure: &ClosureArtifacts{
			TemplateRules:  rules,
			PaperAST:       ast,
			RepairContract: contract,
		},
	}
	verifier.configureRenderGateFromEnv()
	return verifier
}

func (v *Verifier) WithRenderGate(options renderverify.Options, goldenPath string) *Verifier {
	if v == nil {
		return nil
	}
	v.renderOptions = &options
	v.goldenPath = strings.TrimSpace(goldenPath)
	return v
}

func (v *Verifier) WithoutCQRWSTRules() *Verifier {
	if v == nil {
		return nil
	}
	v.skipCQRWST = true
	return v
}

func (v *Verifier) Verify(ctx context.Context, docxPath string) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	pkg, err := ooxmlpkg.Open(docxPath)
	if err != nil {
		return finalizeResult(Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "docx_open",
				Severity: "fatal",
				Message:  fmt.Sprintf("open docx %q failed: %v", docxPath, err),
				Target:   docxPath,
			}},
		}), nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	content, ok := pkg.Get(documentTarget)
	if !ok {
		return finalizeResult(Result{
			Passed: false,
			FatalIssues: []Issue{{
				Kind:     "missing_document_xml",
				Severity: "fatal",
				Message:  "required document XML is missing",
				Target:   documentTarget,
			}},
		}), nil
	}

	result := Result{}
	document := string(content)
	if placeholderPattern.MatchString(document) {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "placeholder",
			Severity: "error",
			Message:  "document still contains template placeholders",
			Target:   documentTarget,
		})
	}

	if v == nil || !v.skipCQRWST {
		cqrwstResult, err := v.checkCQRWST(ctx, docxPath)
		if err != nil {
			result.FatalIssues = append(result.FatalIssues, Issue{
				Kind:     "cqrwst_check",
				Severity: "fatal",
				Message:  fmt.Sprintf("CQRWST rule check failed: %v", err),
				Target:   documentTarget,
			})
		} else {
			for _, issue := range cqrwstResult.Issues {
				result.RepairableIssues = append(result.RepairableIssues, Issue{
					Kind:     "cqrwst_rule",
					Severity: issue.Severity,
					Message:  issue.Message,
					Target:   issue.Target,
				})
			}
		}
	}

	if len(strings.TrimSpace(document)) < 20 {
		result.Warnings = append(result.Warnings, Issue{
			Kind:     "short_document",
			Severity: "warning",
			Message:  "document XML is empty or unexpectedly short",
			Target:   documentTarget,
		})
	}
	checkFinalDeliveryOOXML(pkg, &result)
	v.checkClosureArtifacts(&result)
	v.checkRenderedOutput(ctx, docxPath, &result)

	result.Passed = len(result.FatalIssues) == 0 && len(result.RepairableIssues) == 0
	return finalizeResult(result), nil
}

func checkFinalDeliveryOOXML(pkg *ooxmlpkg.DocxPackage, result *Result) {
	if pkg == nil || result == nil {
		return
	}
	for _, name := range pkg.Names() {
		contentBytes, ok := pkg.Get(name)
		if !ok {
			continue
		}
		content := string(contentBytes)
		if strings.HasPrefix(name, "word/") && strings.HasSuffix(name, ".xml") && strings.Contains(content, `w:val="start"`) {
			result.FatalIssues = append(result.FatalIssues, Issue{
				Kind:     "renderer_incompatible_ooxml",
				Severity: "fatal",
				Message:  "Word XML contains w:val=\"start\", which is incompatible with the renderer used for final verification",
				Target:   name,
			})
		}
	}
	if _, ok := pkg.Get("word/comments.xml"); ok {
		result.RepairableIssues = append(result.RepairableIssues, Issue{
			Kind:     "comments_not_finalized",
			Severity: "error",
			Message:  "final delivery still contains Word comments",
			Target:   "word/comments.xml",
		})
	}
}

func (v *Verifier) checkCQRWST(ctx context.Context, docxPath string) (cqrwst.Result, error) {
	if v != nil && v.templateProfile != nil {
		return cqrwst.CheckDOCXWithTemplateProfile(ctx, docxPath, v.templateProfile)
	}
	return cqrwst.CheckDOCX(ctx, docxPath)
}

func (v *Verifier) checkClosureArtifacts(result *Result) {
	if v == nil || v.closure == nil || result == nil {
		return
	}
	for _, issue := range templatecontract.Validate(v.closure.TemplateRules) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_template_rule",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
	for _, issue := range paperast.Validate(v.closure.PaperAST) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_paper_ast",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
	for _, issue := range repaircontract.Validate(v.closure.RepairContract) {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "closure_repair_contract",
			Severity: "fatal",
			Message:  issue.Message,
			Target:   issue.Kind,
		})
	}
}

func (v *Verifier) checkRenderedOutput(ctx context.Context, docxPath string, result *Result) {
	if v == nil || result == nil || v.renderOptions == nil || !v.renderOptions.Enabled {
		return
	}
	options := *v.renderOptions
	if len(options.SamePageRules) == 0 && v.closure != nil {
		options.SamePageRules = deriveRenderSamePageRules(v.closure.PaperAST)
	}
	renderResult, err := renderverify.Check(ctx, docxPath, options)
	if err != nil {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "render_verify",
			Severity: "fatal",
			Message:  fmt.Sprintf("render verification failed: %v", err),
			Target:   docxPath,
		})
		return
	}
	result.RenderResult = &renderResult
	for _, issue := range renderResult.Issues {
		appendRenderIssue(result, issue)
	}
	if !renderResult.Passed || strings.TrimSpace(v.goldenPath) == "" {
		return
	}
	goldenOptions := options
	goldenOptions.RequiredText = nil
	goldenOptions.ForbiddenText = nil
	goldenOptions.SamePageRules = nil
	goldenOptions.AllowBlankPage = nil
	goldenResult, err := renderverify.Check(ctx, v.goldenPath, goldenOptions)
	if err != nil {
		result.FatalIssues = append(result.FatalIssues, Issue{
			Kind:     "golden_render",
			Severity: "fatal",
			Message:  fmt.Sprintf("render golden sample failed: %v", err),
			Target:   v.goldenPath,
		})
		return
	}
	if !goldenResult.Passed {
		for _, issue := range goldenResult.Issues {
			appendRenderIssue(result, issue)
		}
		return
	}
	regression := goldenregression.CompareSnapshots(goldenregression.Options{
		Candidate:      goldenregression.PageSnapshot{Pages: renderResult.PageTexts},
		Golden:         goldenregression.PageSnapshot{Pages: goldenResult.PageTexts},
		CheckPageCount: envBool("GOLDEN_PAGE_COUNT_STRICT"),
		MaxPageDelta:   envInt("GOLDEN_PAGE_COUNT_MAX_DELTA", 0),
		Landmarks: []goldenregression.Landmark{
			{Name: "abstract", Text: "摘要"},
			{Name: "toc", Text: "目录"},
		},
		SamePageLandmark: deriveGoldenSamePageLandmarks(v.closure),
	})
	result.GoldenRegression = &regression
	for _, issue := range regression.Issues {
		switch issue.Severity {
		case goldenregression.SeverityError:
			result.RepairableIssues = append(result.RepairableIssues, Issue{
				Kind:     "golden_regression_" + issue.Kind,
				Severity: string(issue.Severity),
				Message:  issue.Message,
				Target:   issue.Target,
			})
		default:
			result.Warnings = append(result.Warnings, Issue{
				Kind:     "golden_regression_" + issue.Kind,
				Severity: string(issue.Severity),
				Message:  issue.Message,
				Target:   issue.Target,
			})
		}
	}
}

func (v *Verifier) configureRenderGateFromEnv() {
	if v == nil || !renderverify.DefaultEnabled() {
		return
	}
	v.renderOptions = &renderverify.Options{
		Enabled: true,
		Strict:  envBoolDefault("RENDER_VERIFY_STRICT", true),
	}
	v.goldenPath = strings.TrimSpace(os.Getenv("GOLDEN_TEMPLATE_PATH"))
}

func appendRenderIssue(result *Result, issue renderverify.Issue) {
	converted := Issue{
		Kind:     "render_" + issue.Kind,
		Severity: string(issue.Severity),
		Message:  issue.Message,
		Target:   issue.Target,
	}
	switch issue.Severity {
	case renderverify.SeverityFatal:
		result.FatalIssues = append(result.FatalIssues, converted)
	case renderverify.SeverityError:
		result.RepairableIssues = append(result.RepairableIssues, converted)
	default:
		result.Warnings = append(result.Warnings, converted)
	}
}

func deriveRenderSamePageRules(ast paperast.Snapshot) []renderverify.SamePageRule {
	title, abstract := findTitleAndAbstractLandmarks(ast)
	if title == "" || abstract == "" {
		return nil
	}
	return []renderverify.SamePageRule{{
		Name:      "title_and_abstract_same_page",
		LeftText:  title,
		RightText: abstract,
	}}
}

func deriveGoldenSamePageLandmarks(closure *ClosureArtifacts) []goldenregression.SamePageLandmark {
	if closure == nil {
		return nil
	}
	title, abstract := findTitleAndAbstractLandmarks(closure.PaperAST)
	if title == "" || abstract == "" {
		return nil
	}
	return []goldenregression.SamePageLandmark{{
		Name:  "title_and_abstract_same_page",
		Left:  title,
		Right: abstract,
	}}
}

func findTitleAndAbstractLandmarks(ast paperast.Snapshot) (string, string) {
	abstractIndex := -1
	abstractText := ""
	for index, node := range ast.Nodes {
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		if node.SemanticRole == "abstract_cn" || strings.HasPrefix(compactText(text), "摘要") {
			abstractIndex = index
			abstractText = "摘要"
			break
		}
	}
	if abstractIndex <= 0 {
		return "", ""
	}
	for index := abstractIndex - 1; index >= 0; index-- {
		node := ast.Nodes[index]
		if node.NodeType != "paragraph" {
			continue
		}
		text := strings.TrimSpace(node.Text)
		if isLikelyTitleLandmark(text) {
			return text, abstractText
		}
	}
	return "", ""
}

func isLikelyTitleLandmark(text string) bool {
	compact := compactText(text)
	runeCount := len([]rune(compact))
	if runeCount < 6 || runeCount > 80 {
		return false
	}
	blockedPrefixes := []string{"摘要", "关键词", "Abstract", "KeyWords", "Keywords", "目录", "重庆人文科技学院", "本科毕业论文", "本科毕业设计"}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(compact, compactText(prefix)) {
			return false
		}
	}
	return true
}

func compactText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u3000", " ")
	return strings.Join(strings.Fields(value), "")
}

func envBool(name string) bool {
	return envBoolDefault(name, false)
}

func envBoolDefault(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func finalizeResult(result Result) Result {
	switch {
	case result.Passed:
		result.ComplianceStatus = "format_compliant"
		result.ComplianceReason = "all deterministic verification checks passed"
	case len(result.FatalIssues) > 0:
		result.ComplianceStatus = "rejected"
		result.ComplianceReason = "fatal verification issues prevent compliance proof"
	default:
		result.ComplianceStatus = "review_required"
		result.ComplianceReason = "repairable verification issues remain"
	}
	return result
}
