package database

import (
	"encoding/json"
	"testing"
)

func TestCQRWSTFormatRulesKeepTemplateHeaderAndDynamicTotalPages(t *testing.T) {
	var rules map[string]interface{}
	if err := json.Unmarshal([]byte(buildCQRWSTFormatRulesJSON()), &rules); err != nil {
		t.Fatal(err)
	}
	header := rules["header"].(map[string]interface{})
	if header["content"] != "重庆人文科技学院2026届XXX专业本科毕业论文/设计" {
		t.Fatalf("header = %#v", header)
	}
	pageNumber := rules["page_number"].(map[string]interface{})
	if pageNumber["format"] != "第×页 共×页" {
		t.Fatalf("page_number = %#v", pageNumber)
	}
}
