package ooxmlpatch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/paper-format-checker/backend/internal/core/ooxmlpkg"
)

const (
	documentRelationshipsTarget = "word/_rels/document.xml.rels"
	contentTypesTarget          = "[Content_Types].xml"
	settingsTarget              = "word/settings.xml"

	relationshipNamespace = "http://schemas.openxmlformats.org/package/2006/relationships"
	headerRelationship    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	footerRelationship    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	headerContentType     = "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"
	footerContentType     = "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"
	stylesRelationship    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	numberingRelationship = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	stylesContentType     = "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"
	numberingContentType  = "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"
)

const (
	HeaderRelationshipType = headerRelationship
	FooterRelationshipType = footerRelationship
	HeaderContentType      = headerContentType
	FooterContentType      = footerContentType
)

type FixedRelationshipPartSpec struct {
	PartName         string
	Content          string
	RelationshipID   string
	RelationshipType string
	ContentType      string
}

var (
	relationshipElement = regexp.MustCompile(`<Relationship\b[^>]*/>`)
	relationshipIDAttr  = regexp.MustCompile(`\bId="(rId\d+)"`)
	sectionStartElement = regexp.MustCompile(`(?s)<w:sectPr\b[^>]*>`)
)

type HeaderFooterPolicySpec struct {
	Policy       string
	OddText      string
	EvenText     string
	HeaderLine   string
	FontEastAsia string
	FontSizeHalf int
}

type PageNumberingPolicySpec struct {
	Policy      string
	FrontFormat string
	BodyFormat  string
	BodyStart   int
	BodyWrapper string
}

func ApplyHeaderFooterAndPageNumbering(pkg *ooxmlpkg.DocxPackage, documentTarget string, header HeaderFooterPolicySpec, page PageNumberingPolicySpec) (int, error) {
	if pkg == nil {
		return 0, fmt.Errorf("docx package is nil")
	}
	if strings.TrimSpace(documentTarget) == "" {
		documentTarget = "word/document.xml"
	}
	content, ok := pkg.Get(documentTarget)
	if !ok {
		return 0, fmt.Errorf("%s missing", documentTarget)
	}

	documentXML := string(content)
	originalDocument := documentXML
	changed := 0

	if header.Policy == "odd_even" {
		oddRelID := ensureHeaderPart(pkg, "word/header1.xml", headerXML(defaultHeaderText(header.OddText), header), headerRelationship, headerContentType)
		evenRelID := ensureHeaderPart(pkg, "word/header2.xml", headerXML(header.EvenText, header), headerRelationship, headerContentType)
		documentXML = ensureLastSectionReferences(documentXML, []sectionReference{
			{kind: "header", refType: "default", relID: oddRelID},
			{kind: "header", refType: "even", relID: evenRelID},
		})
		settingsXML := ensureSettingsXML(pkg)
		if updated, ok := ApplySettingsProperties(settingsXML, SettingsPropertiesSpec{EvenAndOddHeaders: true}); ok {
			pkg.Set(settingsTarget, []byte(updated))
			changed++
		}
		changed += 2
	}

	if needsPageFooter(page) {
		footerRelID := ensureHeaderPart(pkg, "word/footer1.xml", footerXML(page), footerRelationship, footerContentType)
		documentXML = ensureLastSectionReferences(documentXML, []sectionReference{{kind: "footer", refType: "default", relID: footerRelID}})
		changed++
	}

	documentXML = applyPageNumberingToSections(documentXML, page)
	if documentXML != originalDocument {
		pkg.Set(documentTarget, []byte(documentXML))
		changed++
	}
	return changed, nil
}

func ApplyHeadingNumberingDefinitions(pkg *ooxmlpkg.DocxPackage, levels []string) (int, error) {
	if pkg == nil {
		return 0, fmt.Errorf("docx package is nil")
	}
	if len(levels) == 0 {
		return 0, nil
	}
	pkg.Set("word/numbering.xml", []byte(headingNumberingXML(levels)))
	pkg.Set("word/styles.xml", []byte(headingStylesXML(levels)))
	ensureContentTypeOverride(pkg, "word/numbering.xml", numberingContentType)
	ensureContentTypeOverride(pkg, "word/styles.xml", stylesContentType)
	ensureRelationship(pkg, numberingRelationship, "numbering.xml")
	ensureRelationship(pkg, stylesRelationship, "styles.xml")
	return 2, nil
}

func EnsureFixedRelationshipPart(pkg *ooxmlpkg.DocxPackage, spec FixedRelationshipPartSpec) int {
	if pkg == nil || spec.PartName == "" || spec.RelationshipID == "" || spec.RelationshipType == "" || spec.ContentType == "" {
		return 0
	}
	count := 0
	current, ok := pkg.Get(spec.PartName)
	if !ok || string(current) != spec.Content {
		pkg.Set(spec.PartName, []byte(spec.Content))
		count++
	}
	if ensureFixedRelationship(pkg, spec.RelationshipID, spec.RelationshipType, strings.TrimPrefix(spec.PartName, "word/")) {
		count++
	}
	beforeTypes, _ := pkg.Get(contentTypesTarget)
	ensureContentTypeOverride(pkg, spec.PartName, spec.ContentType)
	afterTypes, _ := pkg.Get(contentTypesTarget)
	if string(beforeTypes) != string(afterTypes) {
		count++
	}
	return count
}

func ensureFixedRelationship(pkg *ooxmlpkg.DocxPackage, id string, relationshipType string, target string) bool {
	rels := ensureRelationshipsXML(pkg)
	replacement := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, id, relationshipType, target)
	for _, rel := range relationshipElement.FindAllString(rels, -1) {
		if strings.Contains(rel, `Id="`+id+`"`) {
			if rel == replacement {
				return false
			}
			rels = strings.Replace(rels, rel, replacement, 1)
			pkg.Set(documentRelationshipsTarget, []byte(rels))
			return true
		}
	}
	rels = insertBeforeClosingTag(rels, "Relationships", replacement)
	pkg.Set(documentRelationshipsTarget, []byte(rels))
	return true
}

type sectionReference struct {
	kind    string
	refType string
	relID   string
}

func ensureHeaderPart(pkg *ooxmlpkg.DocxPackage, partName, xmlText, relationshipType, contentType string) string {
	pkg.Set(partName, []byte(xmlText))
	ensureContentTypeOverride(pkg, partName, contentType)
	return ensureRelationship(pkg, relationshipType, strings.TrimPrefix(partName, "word/"))
}

func ensureRelationship(pkg *ooxmlpkg.DocxPackage, relationshipType, target string) string {
	rels := ensureRelationshipsXML(pkg)
	for _, rel := range relationshipElement.FindAllString(rels, -1) {
		if strings.Contains(rel, `Type="`+relationshipType+`"`) && strings.Contains(rel, `Target="`+target+`"`) {
			if match := relationshipIDAttr.FindStringSubmatch(rel); len(match) == 2 {
				return match[1]
			}
		}
	}
	nextID := nextRelationshipID(rels)
	rel := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, nextID, relationshipType, target)
	rels = insertBeforeClosingTag(rels, "Relationships", rel)
	pkg.Set(documentRelationshipsTarget, []byte(rels))
	return nextID
}

func ensureRelationshipsXML(pkg *ooxmlpkg.DocxPackage) string {
	content, ok := pkg.Get(documentRelationshipsTarget)
	if !ok || strings.TrimSpace(string(content)) == "" {
		return `<Relationships xmlns="` + relationshipNamespace + `"></Relationships>`
	}
	return string(content)
}

func nextRelationshipID(rels string) string {
	maxID := 0
	for _, match := range relationshipIDAttr.FindAllStringSubmatch(rels, -1) {
		n, _ := strconv.Atoi(strings.TrimPrefix(match[1], "rId"))
		if n > maxID {
			maxID = n
		}
	}
	return "rId" + strconv.Itoa(maxID+1)
}

func ensureContentTypeOverride(pkg *ooxmlpkg.DocxPackage, partName, contentType string) {
	partName = "/" + strings.TrimPrefix(partName, "/")
	content := ensureContentTypesXML(pkg)
	if strings.Contains(content, `PartName="`+partName+`"`) {
		return
	}
	override := fmt.Sprintf(`<Override PartName="%s" ContentType="%s"/>`, partName, contentType)
	content = insertBeforeClosingTag(content, "Types", override)
	pkg.Set(contentTypesTarget, []byte(content))
}

func ensureContentTypesXML(pkg *ooxmlpkg.DocxPackage) string {
	content, ok := pkg.Get(contentTypesTarget)
	if !ok || strings.TrimSpace(string(content)) == "" {
		return `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`
	}
	return string(content)
}

func ensureSettingsXML(pkg *ooxmlpkg.DocxPackage) string {
	content, ok := pkg.Get(settingsTarget)
	if !ok || strings.TrimSpace(string(content)) == "" {
		return `<?xml version="1.0" encoding="UTF-8"?><w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:settings>`
	}
	return string(content)
}

func ensureLastSectionReferences(documentXML string, refs []sectionReference) string {
	if !sectionPropertiesElement.MatchString(documentXML) {
		documentXML, _ = ApplySectionProperties(documentXML, SectionPropertiesSpec{})
	}
	matches := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	if len(matches) == 0 {
		return documentXML
	}
	last := matches[len(matches)-1]
	section := documentXML[last[0]:last[1]]
	section = normalizeSectionElement(section)
	for _, ref := range refs {
		section = removeSectionReference(section, ref.kind, ref.refType)
		section = insertAfterSectionStart(section, fmt.Sprintf(`<w:%sReference w:type="%s" r:id="%s"/>`, ref.kind, ref.refType, ref.relID))
	}
	return documentXML[:last[0]] + section + documentXML[last[1]:]
}

func removeSectionReference(section, kind, refType string) string {
	pattern := regexp.MustCompile(fmt.Sprintf(`<w:%sReference\b[^>]*\bw:type="%s"[^>]*/>`, regexp.QuoteMeta(kind), regexp.QuoteMeta(refType)))
	return pattern.ReplaceAllString(section, "")
}

func insertAfterSectionStart(section, insertion string) string {
	loc := sectionStartElement.FindStringIndex(section)
	if loc == nil {
		return section
	}
	return section[:loc[1]] + insertion + section[loc[1]:]
}

func normalizeSectionElement(section string) string {
	if strings.HasSuffix(section, "/>") {
		return strings.TrimSpace(strings.TrimSuffix(section, "/>")) + "></w:sectPr>"
	}
	return section
}

func applyPageNumberingToSections(documentXML string, page PageNumberingPolicySpec) string {
	matches := sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	if len(matches) == 0 {
		documentXML, _ = ApplySectionProperties(documentXML, SectionPropertiesSpec{})
		matches = sectionPropertiesElement.FindAllStringIndex(documentXML, -1)
	}
	if len(matches) == 0 {
		return documentXML
	}

	type replacement struct {
		start int
		end   int
		text  string
	}
	var replacements []replacement
	if page.FrontFormat != "" && len(matches) > 1 {
		first := matches[0]
		section := replaceSectionPageNumber(documentXML[first[0]:first[1]], page.FrontFormat, 1)
		replacements = append(replacements, replacement{start: first[0], end: first[1], text: section})
	}
	bodyFormat := page.BodyFormat
	if bodyFormat == "" && (page.Policy == "body_arabic_footer_center" || page.Policy == "front_roman_body_arabic_center" || page.Policy == "nuaa_dash_arabic_bottom_right") {
		bodyFormat = "decimal"
	}
	bodyStart := page.BodyStart
	if bodyStart == 0 {
		bodyStart = 1
	}
	if bodyFormat != "" {
		last := matches[len(matches)-1]
		section := replaceSectionPageNumber(documentXML[last[0]:last[1]], bodyFormat, bodyStart)
		replacements = append(replacements, replacement{start: last[0], end: last[1], text: section})
	}
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		documentXML = documentXML[:r.start] + r.text + documentXML[r.end:]
	}
	return documentXML
}

func replaceSectionPageNumber(section, format string, start int) string {
	section = normalizeSectionElement(section)
	section = pageNumberTypeElement.ReplaceAllString(section, "")
	return insertAfterSectionStart(section, buildPageNumberType(format, start))
}

func needsPageFooter(page PageNumberingPolicySpec) bool {
	return page.Policy != "" || page.BodyWrapper != "" || page.BodyFormat != "" || page.BodyStart > 0
}

func defaultHeaderText(text string) string {
	if strings.TrimSpace(text) == "" || text == "chapter" {
		return "chapter"
	}
	return text
}

func BuildHeaderXML(text string, spec HeaderFooterPolicySpec) string {
	return headerXML(text, spec)
}

func BuildPageFooterXML(page PageNumberingPolicySpec) string {
	return footerXML(page)
}

func headerXML(text string, spec HeaderFooterPolicySpec) string {
	if spec.FontEastAsia == "" {
		spec.FontEastAsia = "宋体"
	}
	if spec.FontSizeHalf <= 0 {
		spec.FontSizeHalf = 18
	}
	border := ""
	switch spec.HeaderLine {
	case "double":
		border = `<w:pBdr><w:bottom w:val="double" w:sz="4" w:space="1" w:color="auto"/></w:pBdr>`
	case "", "none":
	default:
		border = `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="auto"/></w:pBdr>`
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr>%s<w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="%s" w:ascii="Times New Roman" w:hAnsi="Times New Roman"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr><w:t>%s</w:t></w:r></w:p></w:hdr>`, border, spec.FontEastAsia, spec.FontSizeHalf, spec.FontSizeHalf, escapeText(text))
}

func footerXML(page PageNumberingPolicySpec) string {
	if page.BodyWrapper == "dash" || page.Policy == "nuaa_dash_arabic_bottom_right" {
		return `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:t>-</w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t>-</w:t></w:r></w:p></w:ftr>`
	}
	if page.BodyWrapper == "chinese_total" || page.Policy == "chinese_page_total" {
		runPr := `<w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr>`
		return `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="宋体" w:hAnsi="Times New Roman"/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr></w:pPr>` +
			`<w:r>` + runPr + `<w:t>第 </w:t></w:r>` +
			`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
			`<w:r>` + runPr + `<w:t> 页 共 </w:t></w:r>` +
			`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> NUMPAGES \* MERGEFORMAT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>` +
			`<w:r>` + runPr + `<w:t> 页</w:t></w:r>` +
			`</w:p></w:ftr>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?><w:ftr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:ftr>`
}

func headingNumberingXML(levels []string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:abstractNum w:abstractNumId="9000">`)
	for index := range levels {
		builder.WriteString(fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%s"/><w:pStyle w:val="ThesisHeading%d"/></w:lvl>`, index, headingLevelText(index), index+1))
	}
	builder.WriteString(`</w:abstractNum><w:num w:numId="9000"><w:abstractNumId w:val="9000"/></w:num></w:numbering>`)
	return builder.String()
}

func headingStylesXML(levels []string) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	for index := range levels {
		builder.WriteString(fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="ThesisHeading%d"><w:name w:val="Thesis Heading %d"/><w:pPr><w:outlineLvl w:val="%d"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="9000"/></w:numPr></w:pPr></w:style>`, index+1, index+1, index, index))
	}
	builder.WriteString(`</w:styles>`)
	return builder.String()
}

func headingLevelText(index int) string {
	switch index {
	case 0:
		return `%1`
	case 1:
		return `%1.%2`
	case 2:
		return `%1.%2.%3`
	default:
		return `%1.%2.%3.%` + strconv.Itoa(index+1)
	}
}

func escapeText(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(text)
}
