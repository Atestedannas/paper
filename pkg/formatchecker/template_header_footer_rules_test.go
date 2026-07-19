package formatchecker

import (
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
)

func TestExtractHeaderFooterRulesChoosesTotalPageFooter(t *testing.T) {
	doc := document.New()
	defer doc.Close()
	doc.AddHeader().AddParagraph().AddRun().AddText("重庆人文科技学院2026届XXX专业本科毕业论文/设计")
	doc.AddFooter().AddParagraph().AddRun().AddText("2")
	doc.AddFooter().AddParagraph().AddRun().AddText("第0页 共12页")

	rules := map[string]interface{}{}
	NewTemplateParser().extractHeaderFooterRules(doc, nil, rules)

	header := rules["header"].(map[string]interface{})
	if header["content"] != "重庆人文科技学院2026届XXX专业本科毕业论文/设计" {
		t.Fatalf("header = %#v", header)
	}
	pageNumber := rules["page_number"].(map[string]interface{})
	if pageNumber["format"] != "第×页 共×页" || pageNumber["has_total_pages"] != true {
		t.Fatalf("page_number = %#v", pageNumber)
	}
}
