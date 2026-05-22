package fileprocessor

import (
	"fmt"
	"regexp"
	"strings"
)

func validateTemplatePayloadFits(templatePkg, userPkg *docxPackage, templateBlocks, userBlocks map[string]templateBlock, payload *userTemplatePayload) error {
	if templatePkg == nil {
		return fmt.Errorf("template package is nil")
	}
	if userPkg == nil {
		return fmt.Errorf("user package is nil")
	}
	if payload == nil {
		return fmt.Errorf("payload is nil")
	}

	if err := validateCoverTableShape(templatePkg, userPkg); err != nil {
		return err
	}

	if _, ok := templatePkg.entries["word/document.xml"]; !ok {
		return fmt.Errorf("missing word/document.xml")
	}
	_ = userBlocks

	return nil
}

func validateTransplantedTemplate(templatePkg, outputPkg *docxPackage, templateBlocks map[string]templateBlock) error {
	if templatePkg == nil {
		return fmt.Errorf("template package is nil")
	}
	if outputPkg == nil {
		return fmt.Errorf("output package is nil")
	}

	if err := validateCoverTableSkeletons(templatePkg, outputPkg); err != nil {
		return err
	}

	if _, ok := templatePkg.entries["word/document.xml"]; !ok {
		return fmt.Errorf("missing word/document.xml")
	}
	if _, ok := outputPkg.entries["word/document.xml"]; !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	if _, err := findTemplateBlocks(outputPkg); err != nil {
		return fmt.Errorf("output anchor structure invalid: %w", err)
	}

	return nil
}

func validateCoverTableShape(templatePkg, userPkg *docxPackage) error {
	templateBodyXML, ok := templatePkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}
	userBodyXML, ok := userPkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	templateTables := extractDocxElements(extractDocxBodyXML(string(templateBodyXML)), "w:tbl")
	userTables := extractDocxElements(extractDocxBodyXML(string(userBodyXML)), "w:tbl")
	if len(templateTables) < 2 || len(userTables) < 2 {
		return fmt.Errorf("missing cover tables")
	}

	for _, check := range []struct {
		name     string
		template string
		user     string
	}{
		{name: "cover_title_table", template: templateTables[0], user: userTables[0]},
		{name: "cover_info_table", template: templateTables[1], user: userTables[1]},
	} {
		if err := compareDocxTableGridShape(check.name, check.template, check.user); err != nil {
			return err
		}
	}

	return nil
}

func validateCoverTableSkeletons(templatePkg, outputPkg *docxPackage) error {
	templateBodyXML, ok := templatePkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}
	outputBodyXML, ok := outputPkg.entries["word/document.xml"]
	if !ok {
		return fmt.Errorf("missing word/document.xml")
	}

	templateTables := extractDocxElements(extractDocxBodyXML(string(templateBodyXML)), "w:tbl")
	outputTables := extractDocxElements(extractDocxBodyXML(string(outputBodyXML)), "w:tbl")
	if len(templateTables) < 2 || len(outputTables) < 2 {
		return fmt.Errorf("missing cover tables")
	}

	for _, check := range []struct {
		name     string
		template string
		output   string
	}{
		{name: "cover_title_table", template: templateTables[0], output: outputTables[0]},
		{name: "cover_info_table", template: templateTables[1], output: outputTables[1]},
	} {
		if err := compareDocxTableGridShape(check.name, check.template, check.output); err != nil {
			return err
		}
	}

	return nil
}

func validateReplacementRangeFits(sectionName string, templateParagraphs []docxParagraph, templateStart, templateEnd int, userParagraphs []docxParagraph, userStart, userEnd int, replacements []string) error {
	if err := ensureNoTableParagraphs(sectionName+" user", userParagraphs, userStart, userEnd); err != nil {
		return err
	}

	templateCapacity := countParagraphSlots(templateParagraphs, templateStart, templateEnd)
	if len(replacements) > templateCapacity {
		return fmt.Errorf("%s payload has %d paragraphs, template range can hold %d", sectionName, len(replacements), templateCapacity)
	}

	return nil
}

func ensureNoTableParagraphs(sectionName string, paragraphs []docxParagraph, startIndex, endIndex int) error {
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(paragraphs) {
		startIndex = len(paragraphs)
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	if endIndex < startIndex {
		endIndex = startIndex
	}
	for _, paragraph := range paragraphs[startIndex:endIndex] {
		if paragraph.inTable {
			return fmt.Errorf("%s range contains table paragraphs", sectionName)
		}
	}
	return nil
}

func countParagraphSlots(paragraphs []docxParagraph, startIndex, endIndex int) int {
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(paragraphs) {
		startIndex = len(paragraphs)
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	if startIndex >= endIndex {
		return 0
	}
	count := 0
	for range paragraphs[startIndex:endIndex] {
		count++
	}
	return count
}

func extractParagraphRangeFragment(paragraphs []docxParagraph, startIndex, endIndex int) string {
	if startIndex < 0 {
		startIndex = 0
	}
	if endIndex > len(paragraphs) {
		endIndex = len(paragraphs)
	}
	if startIndex >= endIndex {
		return ""
	}
	var builder strings.Builder
	for _, paragraph := range paragraphs[startIndex:endIndex] {
		builder.WriteString(paragraph.xml)
	}
	return builder.String()
}

func compareProtectedFragment(blockName, templateFragment, outputFragment string) error {
	if neutralizeDocxText(templateFragment) != neutralizeDocxText(outputFragment) {
		return fmt.Errorf("%s skeleton mismatch", blockName)
	}
	if countDocxTextNodes(templateFragment) != countDocxTextNodes(outputFragment) {
		return fmt.Errorf("%s text node shape mismatch: template=%d output=%d", blockName, countDocxTextNodes(templateFragment), countDocxTextNodes(outputFragment))
	}
	return nil
}

func compareDocxTableGridShape(blockName, templateFragment, outputFragment string) error {
	templateGrid := extractDocxTableCellTextGrid(templateFragment)
	outputGrid := extractDocxTableCellTextGrid(outputFragment)
	if len(templateGrid) != len(outputGrid) {
		return fmt.Errorf("%s row count mismatch: template=%d output=%d", blockName, len(templateGrid), len(outputGrid))
	}
	for rowIndex := range templateGrid {
		if len(templateGrid[rowIndex]) != len(outputGrid[rowIndex]) {
			return fmt.Errorf("%s cell count mismatch at row %d: template=%d output=%d", blockName, rowIndex, len(templateGrid[rowIndex]), len(outputGrid[rowIndex]))
		}
	}
	return nil
}

func neutralizeDocxText(xmlText string) string {
	return task5TextNodePattern.ReplaceAllString(xmlText, `${1}__TEXT__${3}`)
}

func countDocxTextNodes(xmlText string) int {
	return len(task5TextNodePattern.FindAllStringSubmatch(xmlText, -1))
}

var task5TextNodePattern = regexp.MustCompile(`(?s)(<w:t\b[^>]*>)(.*?)(</w:t>)`)
