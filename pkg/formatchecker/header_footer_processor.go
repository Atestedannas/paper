package formatchecker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/measurement"
)

type HeaderFooterProcessor struct {
	config HeaderFooterConfig
}

func NewHeaderFooterProcessor(config HeaderFooterConfig) *HeaderFooterProcessor {
	return &HeaderFooterProcessor{
		config: config,
	}
}

func (p *HeaderFooterProcessor) ProcessDocument(doc *document.Document) error {
	if err := p.applyPageSetup(doc); err != nil {
		return fmt.Errorf("failed to apply page setup: %w", err)
	}

	if err := p.applyHeaders(doc); err != nil {
		return fmt.Errorf("failed to apply headers: %w", err)
	}

	if err := p.applyFooters(doc); err != nil {
		return fmt.Errorf("failed to apply footers: %w", err)
	}

	if err := p.applyPageNumbers(doc); err != nil {
		return fmt.Errorf("failed to apply page numbers: %w", err)
	}

	return nil
}

func (p *HeaderFooterProcessor) applyPageSetup(doc *document.Document) error {
	section := doc.BodySection()

	pgMar := section.X().PgMar
	if pgMar != nil {
		headerDist := p.config.HeaderDistance * measurement.Centimeter
		footerDist := p.config.FooterDistance * measurement.Centimeter

		headerTwips := uint64(headerDist * 20)
		footerTwips := uint64(footerDist * 20)

		if pgMar.HeaderAttr.ST_UnsignedDecimalNumber != nil {
			headerVal := headerTwips
			pgMar.HeaderAttr.ST_UnsignedDecimalNumber = &headerVal
		}

		if pgMar.FooterAttr.ST_UnsignedDecimalNumber != nil {
			footerVal := footerTwips
			pgMar.FooterAttr.ST_UnsignedDecimalNumber = &footerVal
		}
	}

	return nil
}

func (p *HeaderFooterProcessor) applyHeaders(doc *document.Document) error {
	paragraphs := doc.Paragraphs()
	structure := p.analyzeDocumentStructure(paragraphs)

	for i, para := range paragraphs {
		text := p.extractParagraphText(para)
		isMainBodyPage := p.isMainBodyPage(text, i, structure)

		if !p.config.MainBodyHeader.Enable {
			continue
		}

		if isMainBodyPage {
			if err := p.setHeaderForParagraph(para, i); err != nil {
				continue
			}
		}
	}

	return nil
}

func (p *HeaderFooterProcessor) setHeaderForParagraph(para document.Paragraph, paragraphIndex int) error {
	xPara := para.X()
	if xPara == nil || xPara.PPr == nil || xPara.PPr.SectPr == nil {
		return nil
	}

	content := p.getHeaderContent(paragraphIndex)
	if content == "" {
		return nil
	}

	_ = content
	_ = xPara

	return nil
}

func (p *HeaderFooterProcessor) getHeaderContent(paragraphIndex int) string {
	switch p.config.PrintMode {
	case PrintModeSingleSided:
		if p.config.MainBodyHeader.RightPage != "" {
			return fmt.Sprintf("%s\t%s", p.config.MainBodyHeader.LeftPage, p.config.MainBodyHeader.RightPage)
		}
		return p.config.MainBodyHeader.LeftPage

	case PrintModeDoubleSided:
		if paragraphIndex%2 == 0 {
			return p.config.MainBodyHeader.LeftPage
		}
		return p.config.MainBodyHeader.RightPage

	default:
		return p.config.MainBodyHeader.LeftPage
	}
}

func (p *HeaderFooterProcessor) applyFooters(doc *document.Document) error {
	return nil
}

func (p *HeaderFooterProcessor) applyPageNumbers(doc *document.Document) error {
	paragraphs := doc.Paragraphs()
	structure := p.analyzeDocumentStructure(paragraphs)

	pageNum := 1
	for i := range paragraphs {
		text := p.extractParagraphText(paragraphs[i])

		isFrontMatter := p.isFrontMatterPage(text, i, structure)
		isMainBody := p.isMainBodyPage(text, i, structure)

		var format PageNumberFormat
		if isFrontMatter {
			format = p.config.PageNumberConfig.FrontMatterFormat
			if structure.AbstractStart > 0 {
				pageNum = p.config.PageNumberConfig.FrontMatterStartNum + (i-structure.AbstractStart)/10
			}
		} else if isMainBody {
			format = p.config.PageNumberConfig.MainBodyFormat
			if structure.MainBodyStart > 0 {
				pageNum = p.config.PageNumberConfig.MainBodyStartNum + (i-structure.MainBodyStart)/10
			}
		} else {
			continue
		}

		if format == PageNumberNone {
			continue
		}

		pageNumberStr := p.formatPageNumber(pageNum, format)
		if err := p.setPageNumberForParagraph(paragraphs[i], pageNumberStr); err != nil {
			continue
		}

		pageNum++
	}

	return nil
}

func (p *HeaderFooterProcessor) formatPageNumber(num int, format PageNumberFormat) string {
	switch format {
	case PageNumberRoman:
		return p.toRomanNumerals(num)
	case PageNumberArabic:
		return strconv.Itoa(num)
	default:
		return strconv.Itoa(num)
	}
}

func (p *HeaderFooterProcessor) toRomanNumerals(num int) string {
	if num <= 0 {
		return ""
	}

	romanNumerals := []struct {
		value   int
		numeral string
	}{
		{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"},
		{100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"},
		{10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"},
	}

	result := ""
	for _, rn := range romanNumerals {
		for num >= rn.value {
			result += rn.numeral
			num -= rn.value
		}
	}

	return result
}

func (p *HeaderFooterProcessor) setPageNumberForParagraph(para document.Paragraph, pageNumber string) error {
	xPara := para.X()
	if xPara == nil || xPara.PPr == nil {
		return nil
	}

	sectPr := xPara.PPr.SectPr
	if sectPr == nil {
		return nil
	}

	_ = sectPr
	_ = pageNumber

	return nil
}

func (p *HeaderFooterProcessor) analyzeDocumentStructure(paragraphs []document.Paragraph) *HeaderFooterDocumentStructure {
	structure := &HeaderFooterDocumentStructure{}

	for i, para := range paragraphs {
		text := p.extractParagraphText(para)
		trimmedText := strings.TrimSpace(text)

		if strings.Contains(trimmedText, "摘要") && len(trimmedText) < 20 && structure.AbstractStart == 0 {
			structure.AbstractStart = i
		}

		if strings.Contains(trimmedText, "关键词") && len(trimmedText) < 30 {
			structure.KeywordsStart = i
			structure.KeywordsEnd = i
		}

		if strings.Contains(trimmedText, "目录") && len(trimmedText) < 15 {
			structure.TOCStart = i
		}

		if structure.TOCStart > 0 && structure.TOCEnd == 0 {
			if matched, _ := regexp.MatchString(`^(第一章|1\s+|1\.)`, trimmedText); matched {
				structure.TOCEnd = i
				structure.MainBodyStart = i
			}
		}

		if strings.Contains(trimmedText, "参考文献") || strings.Contains(trimmedText, "References") {
			structure.ReferencesStart = i
		}
	}

	return structure
}

type HeaderFooterDocumentStructure struct {
	AbstractStart   int
	AbstractEnd     int
	KeywordsStart   int
	KeywordsEnd     int
	TOCStart        int
	TOCEnd          int
	MainBodyStart   int
	ReferencesStart int
}

func (p *HeaderFooterProcessor) isAbstractPage(text string, index int, structure *HeaderFooterDocumentStructure) bool {
	if structure.AbstractStart == 0 {
		return false
	}
	return index >= structure.AbstractStart && (structure.AbstractEnd == 0 || index <= structure.AbstractEnd)
}

func (p *HeaderFooterProcessor) isMainBodyPage(text string, index int, structure *HeaderFooterDocumentStructure) bool {
	if structure.MainBodyStart == 0 {
		return false
	}
	return index >= structure.MainBodyStart && (structure.ReferencesStart == 0 || index < structure.ReferencesStart)
}

func (p *HeaderFooterProcessor) isFrontMatterPage(text string, index int, structure *HeaderFooterDocumentStructure) bool {
	if structure.AbstractStart == 0 {
		return false
	}
	return index >= structure.AbstractStart && index < structure.MainBodyStart
}

func (p *HeaderFooterProcessor) extractParagraphText(para document.Paragraph) string {
	var text strings.Builder
	for _, run := range para.Runs() {
		text.WriteString(run.Text())
	}
	return text.String()
}

func (p *HeaderFooterProcessor) parseFontSize(size string) float64 {
	sizeMap := map[string]float64{
		"初号": 42, "小初": 36, "一号": 26, "小一": 24,
		"二号": 22, "小二": 18, "三号": 16, "小三": 15,
		"四号": 14, "小四": 12, "五号": 10.5, "小五": 9,
		"六号": 7.5, "小六": 6.5, "七号": 5.5, "八号": 5,
	}

	if val, ok := sizeMap[size]; ok {
		return val * 2
	}

	if val, err := strconv.ParseFloat(size, 64); err == nil {
		return val * 2
	}

	return 21
}
