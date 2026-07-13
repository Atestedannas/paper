package templatefiller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ClassificationParagraph struct {
	Index             int    `json:"index"`
	Type              string `json:"type"`
	Text              string `json:"text"`
	Kind              string `json:"kind,omitempty"`
	SourceXML         string `json:"-"`
	HasComplexContent bool   `json:"-"`
}

type ClassificationResult struct {
	Paragraphs []ClassificationParagraph `json:"paragraphs"`
}

type FillResult struct {
	Status     string `json:"status"`
	Output     string `json:"output"`
	Paragraphs int    `json:"paragraphs"`
	Tables     int    `json:"tables"`
}

type TemplateFiller struct {
	TemplateDir       string
	UsePythonFallback bool
	DeepSeekClient    DeepSeekClient
}

func NewTemplateFiller() *TemplateFiller {
	return &TemplateFiller{
		TemplateDir:       locateDir("golden_templates", "uploads/golden_templates"),
		UsePythonFallback: false,
	}
}

func (tf *TemplateFiller) Fill(ctx context.Context, inputPath, templatePath string, classification ClassificationResult, outputDir string) (string, error) {
	if _, err := os.Stat(templatePath); err != nil {
		return "", fmt.Errorf("golden template not found: %s", templatePath)
	}
	if _, err := os.Stat(inputPath); err != nil {
		return "", fmt.Errorf("input file not found: %s", inputPath)
	}

	classification = normalizeClassificationForTemplateFill(classification)
	sections := classificationToSections(classification)

	coverFields, _ := ExtractCoverFields(inputPath)
	mergeInferredCoverFields(coverFields, classification)
	sections = append(sections, buildCoverSections(coverFields)...)

	studentDocBytes, _ := os.ReadFile(inputPath)
	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	filler := NewOOXMLFiller()
	return filler.FillTemplateWithMedia(ctx, templatePath, studentDocBytes, sections, outputDir, baseName)
}

func classificationToSections(cls ClassificationResult) []SectionContent {
	groups := make(map[string]*SectionContent)
	order := make([]string, 0)

	for _, p := range cls.Paragraphs {
		sectionType := mapParaTypeToSection(p.Type)
		sc, ok := groups[sectionType]
		if !ok {
			sc = &SectionContent{SectionType: sectionType}
			groups[sectionType] = sc
			order = append(order, sectionType)
		}
		sc.Paragraphs = append(sc.Paragraphs, classificationParagraphToContentParagraph(p))
	}

	result := make([]SectionContent, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	if toc := synthesizeTOCSection(cls, groups["table_of_contents"]); toc != nil {
		result = append(result, *toc)
	}
	return result
}

func normalizeClassificationForTemplateFill(cls ClassificationResult) ClassificationResult {
	result := ClassificationResult{Paragraphs: make([]ClassificationParagraph, 0, len(cls.Paragraphs)+2)}
	hasAbstractTitle := false
	hasEnAbstractTitle := false

	for _, p := range cls.Paragraphs {
		if p.Type == "abstract_title" {
			hasAbstractTitle = true
		}
		if p.Type == "en_abstract_title" {
			hasEnAbstractTitle = true
		}
	}

	for _, p := range cls.Paragraphs {
		if p.Type == "abstract" && !p.HasComplexContent && !hasAbstractTitle {
			if title, rest, ok := splitInlineAbstractLabel(p.Text, true); ok {
				result.Paragraphs = append(result.Paragraphs, ClassificationParagraph{Index: p.Index, Type: "abstract_title", Text: title})
				if strings.TrimSpace(rest) != "" {
					p.Text = rest
					result.Paragraphs = append(result.Paragraphs, p)
				}
				hasAbstractTitle = true
				continue
			}
		}
		if p.Type == "en_abstract" && !p.HasComplexContent && !hasEnAbstractTitle {
			if title, rest, ok := splitInlineAbstractLabel(p.Text, false); ok {
				result.Paragraphs = append(result.Paragraphs, ClassificationParagraph{Index: p.Index, Type: "en_abstract_title", Text: title})
				if strings.TrimSpace(rest) != "" {
					p.Text = rest
					result.Paragraphs = append(result.Paragraphs, p)
				}
				hasEnAbstractTitle = true
				continue
			}
		}
		result.Paragraphs = append(result.Paragraphs, p)
	}
	return result
}

func splitInlineAbstractLabel(text string, chinese bool) (title string, rest string, ok bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return "", "", false
	}
	if chinese {
		if strings.HasPrefix(s, "摘要：") || strings.HasPrefix(s, "摘要:") {
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "摘要："), "摘要:"))
			return "摘要", rest, true
		}
		if strings.HasPrefix(s, "摘 要：") || strings.HasPrefix(s, "摘 要:") {
			rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "摘 要："), "摘 要:"))
			return "摘 要", rest, true
		}
		return "", "", false
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "abstract:") {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 2 {
			return "Abstract", strings.TrimSpace(parts[1]), true
		}
	}
	if strings.HasPrefix(lower, "abstract：") {
		parts := strings.SplitN(s, "：", 2)
		if len(parts) == 2 {
			return "Abstract", strings.TrimSpace(parts[1]), true
		}
	}
	return "", "", false
}

func mergeInferredCoverFields(fields *CoverFields, cls ClassificationResult) {
	if fields == nil {
		return
	}
	inferred := inferCoverFieldsFromClassification(cls)
	fields.Title = preferMeaningfulCoverValue(fields.Title, inferred.Title)
	fields.Subtitle = preferMeaningfulCoverValue(fields.Subtitle, inferred.Subtitle)
	fields.College = preferMeaningfulCoverValue(fields.College, inferred.College)
	fields.Major = preferMeaningfulCoverValue(fields.Major, inferred.Major)
	fields.Grade = preferMeaningfulCoverValue(fields.Grade, inferred.Grade)
	fields.StudentID = preferMeaningfulCoverValue(fields.StudentID, inferred.StudentID)
	fields.Name = preferMeaningfulCoverValue(fields.Name, inferred.Name)
	fields.Advisor = preferMeaningfulCoverValue(fields.Advisor, inferred.Advisor)
	fields.Date = preferMeaningfulCoverValue(fields.Date, inferred.Date)
	if !isMeaningfulCoverValue(fields.Date) {
		fields.Date = normalizeCoverDate("")
	}
}

func inferCoverFieldsFromClassification(cls ClassificationResult) *CoverFields {
	fields := &CoverFields{}
	for _, p := range leadingCoverParagraphs(cls.Paragraphs) {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		if p.Type == "cover" || strings.HasPrefix(p.Type, "cover_") || p.Type == "title" {
			parseCoverLineIntoFields(fields, text)
		}
	}
	return fields
}

func leadingCoverParagraphs(paragraphs []ClassificationParagraph) []ClassificationParagraph {
	var out []ClassificationParagraph
	for _, p := range paragraphs {
		switch p.Type {
		case "abstract_title", "abstract", "en_abstract_title", "en_abstract", "table_of_contents", "table_of_contents_title", "heading_1", "references_title", "acknowledgements_title", "appendix_title":
			return out
		default:
			out = append(out, p)
		}
	}
	return out
}

func parseCoverLineIntoFields(fields *CoverFields, text string) {
	text = normalizeCoverLine(text)
	switch {
	case strings.Contains(text, "学院"):
		fields.College = preferMeaningfulCoverValue(fields.College, strings.TrimSpace(strings.TrimPrefix(text, "学院")))
	case strings.Contains(text, "专业"):
		fields.Major = preferMeaningfulCoverValue(fields.Major, strings.TrimSpace(strings.TrimPrefix(text, "专业")))
	case strings.Contains(text, "班级"):
		fields.Grade = preferMeaningfulCoverValue(fields.Grade, strings.TrimSpace(strings.TrimPrefix(text, "班级")))
	case strings.Contains(text, "学号"):
		fields.StudentID = preferMeaningfulCoverValue(fields.StudentID, strings.TrimSpace(strings.TrimPrefix(text, "学号")))
	case strings.Contains(text, "姓名"):
		fields.Name = preferMeaningfulCoverValue(fields.Name, strings.TrimSpace(strings.TrimPrefix(text, "姓名")))
	case strings.Contains(text, "指导教师"):
		fields.Advisor = preferMeaningfulCoverValue(fields.Advisor, strings.TrimSpace(strings.TrimPrefix(text, "指导教师")))
	}
}

func preferMeaningfulCoverValue(current, candidate string) string {
	if isMeaningfulCoverValue(current) {
		return strings.TrimSpace(current)
	}
	if isMeaningfulCoverValue(candidate) {
		return strings.TrimSpace(candidate)
	}
	return strings.TrimSpace(current)
}

func isMeaningfulCoverValue(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(v, " ", ""))
	for _, frag := range []string{"xxxx", "xxx", "请填写", "删除", "说明", "模板", "{{", "}}"} {
		if strings.Contains(lower, frag) {
			return false
		}
	}
	return true
}

func normalizeCoverLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "　", " "))
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func looksLikePaperTitle(text string) bool {
	text = normalizeCoverLine(text)
	return len([]rune(text)) >= 6 && len([]rune(text)) <= 40 && !strings.Contains(text, "摘要")
}

func looksLikeSubtitle(text string) bool {
	text = normalizeCoverLine(text)
	return strings.Contains(text, "——") || strings.Contains(text, "--")
}

func synthesizeTOCSection(cls ClassificationResult, existing *SectionContent) *SectionContent {
	if tocSectionHasMeaningfulContent(existing) {
		return nil
	}
	var tocParas []ContentParagraph
	for _, p := range cls.Paragraphs {
		switch p.Type {
		case "heading_1", "heading_2", "heading_3":
			if strings.TrimSpace(p.Text) != "" {
				tocParas = append(tocParas, ContentParagraph{Text: strings.TrimSpace(p.Text), ParaType: "table_of_contents"})
			}
		}
	}
	if len(tocParas) == 0 {
		return nil
	}
	return &SectionContent{SectionType: "table_of_contents", Paragraphs: tocParas}
}

func classificationParagraphToContentParagraph(p ClassificationParagraph) ContentParagraph {
	cp := ContentParagraph{
		Text:              p.Text,
		SourceXML:         p.SourceXML,
		HasComplexContent: p.HasComplexContent,
		ParaType:          p.Type,
	}
	if cp.HasComplexContent && cp.SourceXML != "" {
		return cp
	}
	switch p.Type {
	case "keywords":
		if runs, plain, ok := buildKeywordRuns(p.Text, true); ok {
			cp.Text = plain
			cp.Runs = runs
		}
	case "en_keywords":
		if runs, plain, ok := buildKeywordRuns(p.Text, false); ok {
			cp.Text = plain
			cp.Runs = runs
		}
	}
	return cp
}

func tocSectionHasMeaningfulContent(existing *SectionContent) bool {
	if existing == nil {
		return false
	}
	for _, p := range existing.Paragraphs {
		if strings.TrimSpace(p.Text) != "" {
			return true
		}
	}
	return false
}

func buildKeywordRuns(text string, chinese bool) ([]ContentRun, string, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, "", false
	}

	label := "关键词："
	altLabel := "关键词:"
	if !chinese {
		label = "Keywords:"
		altLabel = "Key words:"
	}

	content := s
	switch {
	case strings.HasPrefix(s, label):
		content = strings.TrimSpace(strings.TrimPrefix(s, label))
	case strings.HasPrefix(s, altLabel):
		content = strings.TrimSpace(strings.TrimPrefix(s, altLabel))
	default:
		if chinese {
			return nil, "", false
		}
	}

	if !chinese {
		for _, dup := range []string{"Keywords:", "Key words:", "keywords:", "key words:"} {
			content = strings.ReplaceAll(content, dup, "")
		}
	}
	replacer := strings.NewReplacer("；", ";", "，", ";", "、", ";", ",", ";")
	content = replacer.Replace(content)
	parts := strings.Split(content, ";")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			if !chinese {
				part = strings.ToUpper(part[:1]) + part[1:]
			}
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return nil, "", false
	}
	separator := "；"
	if !chinese {
		separator = ",  "
	}
	content = strings.Join(cleaned, separator)
	bold := true
	runs := []ContentRun{{Text: label, Bold: &bold}, {Text: " " + content}}
	return runs, label + " " + content, true
}

func buildCoverSections(fields *CoverFields) []SectionContent {
	if fields == nil {
		return nil
	}
	title, subtitle, syntheticSecondLine := splitCoverTitleForTemplate(fields.Title, fields.Subtitle)
	var sections []SectionContent
	add := func(sectionType, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		sections = append(sections, SectionContent{SectionType: sectionType, Paragraphs: []ContentParagraph{{Text: strings.TrimSpace(text)}}})
	}
	add("cover_title", title)
	add("cover_subtitle", subtitle)
	add("cover_college", fields.College)
	add("cover_major", fields.Major)
	add("cover_grade", fields.Grade)
	add("cover_student_id", fields.StudentID)
	add("cover_student_name", fields.Name)
	add("cover_advisor", fields.Advisor)
	add("cover_date", normalizeCoverDate(fields.Date))
	add("inner_title", strings.TrimSpace(fields.Title))
	if !syntheticSecondLine {
		add("inner_subtitle", strings.TrimSpace(fields.Subtitle))
	}
	return sections
}

func splitCoverTitleForTemplate(title, subtitle string) (string, string, bool) {
	title = strings.TrimSpace(title)
	subtitle = strings.TrimSpace(subtitle)
	if title == "" {
		return "", subtitle, false
	}
	if subtitle != "" {
		return title, subtitle, false
	}
	runes := []rune(title)
	if len(runes) <= 22 {
		return title, "", false
	}
	splitAt := len(runes) / 2
	return strings.TrimSpace(string(runes[:splitAt])), strings.TrimSpace(string(runes[splitAt:])), true
}

func normalizeCoverDate(date string) string {
	date = strings.TrimSpace(date)
	if date != "" {
		date = strings.ReplaceAll(date, " ", "")
		date = strings.ReplaceAll(date, "　", "")
		return date
	}
	now := time.Now()
	return fmt.Sprintf("%d年%d月", now.Year(), int(now.Month()))
}

func mapParaTypeToSection(paraType string) string {
	switch paraType {
	case "cover", "cover_title", "title":
		return "cover_title"
	case "cover_subtitle":
		return "cover_subtitle"
	case "cover_college":
		return "cover_college"
	case "cover_major":
		return "cover_major"
	case "cover_grade":
		return "cover_grade"
	case "cover_student_name":
		return "cover_student_name"
	case "cover_student_id":
		return "cover_student_id"
	case "cover_advisor":
		return "cover_advisor"
	case "cover_date":
		return "cover_date"
	case "abstract_title":
		return "abstract_title"
	case "abstract":
		return "abstract"
	case "keywords":
		return "keywords"
	case "en_abstract_title":
		return "en_abstract_title"
	case "en_abstract":
		return "en_abstract"
	case "en_keywords":
		return "en_keywords"
	case "table_of_contents_title", "table_of_contents":
		return "table_of_contents"
	case "references_title":
		return "references_title"
	case "references":
		return "references"
	case "acknowledgements_title":
		return "acknowledgements_title"
	case "acknowledgements":
		return "acknowledgements"
	case "appendix_title":
		return "appendix_title"
	case "appendix":
		return "appendix"
	default:
		return "body"
	}
}

func (tf *TemplateFiller) GetPreparedTemplatePath(templateID string) string {
	if tf.TemplateDir == "" {
		return ""
	}
	path := filepath.Join(tf.TemplateDir, templateID+"_prepared.docx")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func (tf *TemplateFiller) GetTemplatePath(templateID string) string {
	if p := tf.GetPreparedTemplatePath(templateID); p != "" {
		return p
	}
	if tf.TemplateDir == "" {
		return ""
	}
	return filepath.Join(tf.TemplateDir, templateID+".docx")
}

func (tf *TemplateFiller) HasTemplate(templateID string) bool {
	_, err := os.Stat(tf.GetTemplatePath(templateID))
	return err == nil
}

func (tf *TemplateFiller) EnsureGoldenTemplate(templateID string) (string, error) {
	path := tf.GetTemplatePath(templateID)
	if path == "" {
		return "", fmt.Errorf("template not found")
	}
	return path, nil
}

func locateDir(_ string, relPath string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(wd, relPath)
}
