package fileprocessor

import (
	"testing"

	"gitee.com/greatmusicians/unioffice/document"
	"gitee.com/greatmusicians/unioffice/schema/soo/wml"
)

func TestIsAbstractCategory(t *testing.T) {
	tests := []struct {
		category string
		want     bool
	}{
		{category: "abstract", want: true},
		{category: "en_abstract", want: true},
		{category: "english_abstract", want: true},
		{category: "body", want: false},
		{category: "references", want: false},
	}

	for _, tt := range tests {
		got := isAbstractCategory(tt.category)
		if got != tt.want {
			t.Fatalf("isAbstractCategory(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestBuildParagraphSpecPatchFormatsNumberedHeading(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	run := para.AddRun()
	run.AddText("1.1 研究背景")
	run.X().RPr = wml.NewCT_RPr()
	run.X().RPr.RFonts = wml.NewCT_Fonts()
	font := "宋体"
	run.X().RPr.RFonts.EastAsiaAttr = &font
	run.X().RPr.Sz = wml.NewCT_HpsMeasure()
	size := uint64(30)
	run.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &size
	patch := buildParagraphSpecPatch(para, ParagraphFormatSpec{
		FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 30,
	})
	if patch.FontEastAsia != "黑体" || patch.FontAscii != "Times New Roman" {
		t.Fatalf("numbered heading font patch = %+v", patch)
	}
}

func TestApplySpecToParaNormalizesFontSlotsAcrossSplitRuns(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	first := para.AddRun()
	first.AddText("1.1")
	second := para.AddRun()
	second.AddText(" 研究背景")
	for _, run := range []*wml.CT_R{first.X(), second.X()} {
		run.RPr = wml.NewCT_RPr()
		run.RPr.RFonts = wml.NewCT_Fonts()
		oldEA, oldAscii := "宋体", "SimSun"
		run.RPr.RFonts.EastAsiaAttr = &oldEA
		run.RPr.RFonts.AsciiAttr = &oldAscii
	}
	NewAIFormatApplier(NewEnhancedProcessor()).ApplySpecToPara(para, ParagraphFormatSpec{
		FontEastAsia: "黑体", FontAscii: "Times New Roman", FontSizeHalfPt: 30,
	})
	for _, run := range []*wml.CT_R{first.X(), second.X()} {
		if run.RPr == nil || run.RPr.RFonts == nil ||
			*run.RPr.RFonts.EastAsiaAttr != "黑体" || *run.RPr.RFonts.AsciiAttr != "Times New Roman" {
			t.Fatalf("run font slots were not normalized: %+v", run.RPr)
		}
	}
}

func TestApplySpecToParaNormalizesShortOutlierRun(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	short := para.AddRun()
	short.AddText("Key words:")
	short.X().RPr = wml.NewCT_RPr()
	short.X().RPr.Sz = wml.NewCT_HpsMeasure()
	shortSize := uint64(30)
	short.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &shortSize
	long := para.AddRun()
	long.AddText(" Community type 2 diabetes")
	long.X().RPr = wml.NewCT_RPr()
	long.X().RPr.Sz = wml.NewCT_HpsMeasure()
	longSize := uint64(24)
	long.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &longSize

	NewAIFormatApplier(NewEnhancedProcessor()).ApplySpecToPara(para, ParagraphFormatSpec{FontSizeHalfPt: 24})
	if got := *short.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber; got != 24 {
		t.Fatalf("short outlier run size = %d, want 24", got)
	}
}

func TestApplySpecToParaNormalizesWhitespaceRunSize(t *testing.T) {
	doc := document.New()
	para := doc.AddParagraph()
	run := para.AddRun()
	run.AddText(" ")
	run.X().RPr = wml.NewCT_RPr()
	run.X().RPr.Sz = wml.NewCT_HpsMeasure()
	old := uint64(24)
	run.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &old

	NewAIFormatApplier(NewEnhancedProcessor()).ApplySpecToPara(para, ParagraphFormatSpec{FontSizeHalfPt: 21})
	if got := *run.X().RPr.Sz.ValAttr.ST_UnsignedDecimalNumber; got != 21 {
		t.Fatalf("whitespace run size = %d, want 21", got)
	}
}
