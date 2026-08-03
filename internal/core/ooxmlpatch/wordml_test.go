package ooxmlpatch

import (
	"strings"
	"testing"
)

func TestApplySectionPropertiesWritesISO29500PageSetup(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p><w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`

	updated, changed := ApplySectionProperties(document, SectionPropertiesSpec{
		PageWidthTwips:     11906,
		PageHeightTwips:    16838,
		MarginTopTwips:     1701,
		MarginRightTwips:   1417,
		MarginBottomTwips:  1417,
		MarginLeftTwips:    1701,
		GutterTwips:        567,
		HeaderMarginTwips:  907,
		FooterMarginTwips:  851,
		PageNumberFormat:   "decimal",
		PageNumberStart:    1,
		RemoveHeaderFooter: true,
	})

	if !changed {
		t.Fatal("ApplySectionProperties() changed = false, want true")
	}
	for _, want := range []string{
		`<w:pgSz w:w="11906" w:h="16838"/>`,
		`<w:pgMar w:top="1701" w:right="1417" w:bottom="1417" w:left="1701" w:gutter="567" w:header="907" w:footer="851"/>`,
		`<w:pgNumType w:fmt="decimal" w:start="1"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated document missing %s:\n%s", want, updated)
		}
	}
	if strings.Contains(updated, "headerReference") || strings.Contains(updated, "footerReference") {
		t.Fatalf("updated document should remove header/footer references:\n%s", updated)
	}
}

func TestApplySectionPropertiesAtUpdatesRequestedSection(t *testing.T) {
	document := `<w:document><w:body><w:p><w:pPr><w:sectPr><w:pgNumType w:fmt="decimal"/></w:sectPr></w:pPr></w:p><w:sectPr><w:pgNumType w:fmt="decimal"/></w:sectPr></w:body></w:document>`
	updated, changed := ApplySectionPropertiesAt(document, 0, SectionPropertiesSpec{PageNumberFormat: "lowerRoman", PageNumberStart: 1})
	if !changed || !strings.Contains(updated, `<w:pgNumType w:fmt="lowerRoman" w:start="1"/>`) {
		t.Fatalf("requested front section was not updated: %s", updated)
	}
	if strings.Count(updated, `w:fmt="decimal"`) != 1 {
		t.Fatalf("non-target section changed: %s", updated)
	}
}

func TestApplySectionPropertiesPreservesPageNumberingWhenOnlyPageSetupChanges(t *testing.T) {
	document := `<w:document><w:body><w:sectPr><w:pgNumType w:fmt="decimal" w:start="1"/><w:pgMar w:top="1440"/></w:sectPr></w:body></w:document>`
	updated, changed := ApplySectionProperties(document, SectionPropertiesSpec{MarginTopTwips: 720})
	if !changed {
		t.Fatal("ApplySectionProperties() changed = false, want true")
	}
	if !strings.Contains(updated, `<w:pgNumType w:fmt="decimal" w:start="1"/>`) {
		t.Fatalf("page numbering was removed while changing margins: %s", updated)
	}
}

func TestElementBodyHandlesGreaterThanInQuotedAttribute(t *testing.T) {
	element := `<w:pPr data-test="a>b"><w:jc w:val="center"/></w:pPr>`
	if got := elementBody(element); got != `<w:jc w:val="center"/>` {
		t.Fatalf("elementBody() = %q", got)
	}
}

func TestApplySettingsPropertiesWritesEvenOddHeaderSwitch(t *testing.T) {
	settings := `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:zoom w:percent="100"/></w:settings>`

	updated, changed := ApplySettingsProperties(settings, SettingsPropertiesSpec{EvenAndOddHeaders: true, UpdateFieldsOnOpen: true})

	if !changed {
		t.Fatal("ApplySettingsProperties() changed = false, want true")
	}
	if !strings.Contains(updated, `<w:evenAndOddHeaders/>`) {
		t.Fatalf("updated settings missing evenAndOddHeaders:\n%s", updated)
	}
	if !strings.Contains(updated, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("updated settings missing updateFields:\n%s", updated)
	}
	if strings.Index(updated, "<w:evenAndOddHeaders") > strings.Index(updated, "<w:updateFields") {
		t.Fatalf("evenAndOddHeaders must precede updateFields in CT_Settings order:\n%s", updated)
	}
}

func TestApplySettingsPropertiesPreservesExistingSwitches(t *testing.T) {
	settings := `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:evenAndOddHeaders/><w:compat/><w:docVars/></w:settings>`

	updated, changed := ApplySettingsProperties(settings, SettingsPropertiesSpec{UpdateFieldsOnOpen: true})

	if !changed {
		t.Fatal("ApplySettingsProperties() changed = false, want true")
	}
	if !strings.Contains(updated, `<w:evenAndOddHeaders/>`) {
		t.Fatalf("updated settings should preserve evenAndOddHeaders:\n%s", updated)
	}
	if !strings.Contains(updated, `<w:updateFields w:val="true"/>`) {
		t.Fatalf("updated settings missing updateFields:\n%s", updated)
	}
	if strings.Index(updated, "<w:updateFields") > strings.Index(updated, "<w:compat") {
		t.Fatalf("updateFields must precede compat in CT_Settings order:\n%s", updated)
	}
}

func TestApplyParagraphAndRunPropertiesWritesPreciseWordprocessingML(t *testing.T) {
	paragraph := `<w:p><w:pPr><w:jc w:val="left"/><w:spacing w:line="240"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:sz w:val="21"/></w:rPr><w:t>摘要</w:t></w:r></w:p>`

	updated, changed := ApplyParagraphProperties(paragraph, ParagraphPropertiesSpec{
		Alignment:          "center",
		LineTwips:          400,
		LineRule:           "exact",
		BeforeTwips:        240,
		AfterTwips:         240,
		FirstLineChars:     200,
		FirstLineTwips:     480,
		PageBreakBefore:    true,
		KeepNext:           true,
		SnapToGridOff:      true,
		AdjustRightIndZero: true,
		RunPropertiesInPPr: true,
		EastAsiaFont:       "黑体",
		AsciiFont:          "Times New Roman",
		HAnsiFont:          "Times New Roman",
		FontHint:           "eastAsia",
		FontSizeHalfPoints: 32,
		ComplexSizeHalfPts: 32,
		Bold:               true,
		RemoveOutlineLevel: true,
	})

	if !changed {
		t.Fatal("ApplyParagraphProperties() changed = false, want true")
	}
	for _, want := range []string{
		`<w:jc w:val="center"/>`,
		`<w:spacing`,
		`w:before="240"`,
		`w:after="240"`,
		`w:line="400"`,
		`w:lineRule="exact"`,
		`<w:ind w:firstLineChars="200" w:firstLine="480"/>`,
		`<w:pageBreakBefore/>`,
		`<w:keepNext/>`,
		`<w:snapToGrid w:val="0"/>`,
		`<w:adjustRightInd w:val="0"/>`,
		`<w:rFonts`,
		`w:eastAsia="黑体"`,
		`w:ascii="Times New Roman"`,
		`w:hAnsi="Times New Roman"`,
		`w:hint="eastAsia"`,
		`<w:sz w:val="32"/>`,
		`<w:szCs w:val="32"/>`,
		`<w:b/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated paragraph missing %s:\n%s", want, updated)
		}
	}
}

func TestApplyRunPropertiesCanWriteSuperscriptCitation(t *testing.T) {
	run := `<w:r><w:rPr><w:sz w:val="24"/></w:rPr><w:t>[1]</w:t></w:r>`

	updated, changed := ApplyRunProperties(run, RunPropertiesSpec{
		EastAsiaFont:       "宋体",
		AsciiFont:          "Times New Roman",
		HAnsiFont:          "Times New Roman",
		FontSizeHalfPoints: 18,
		VerticalAlign:      "superscript",
	})

	if !changed {
		t.Fatal("ApplyRunProperties() changed = false, want true")
	}
	for _, want := range []string{
		`<w:rFonts`,
		`w:eastAsia="宋体"`,
		`w:ascii="Times New Roman"`,
		`w:hAnsi="Times New Roman"`,
		`<w:sz w:val="18"/>`,
		`<w:vertAlign w:val="superscript"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated run missing %s:\n%s", want, updated)
		}
	}
}

func TestApplyRunPropertiesOnlyReplacesSpecifiedProperties(t *testing.T) {
	run := `<w:r><w:rPr><w:rFonts w:eastAsia="宋体"/><w:b/><w:i/><w:color w:val="FF0000"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:vertAlign w:val="superscript"/></w:rPr><w:t>[1]</w:t></w:r>`

	unchanged, changed := ApplyRunProperties(run, RunPropertiesSpec{})
	if changed || unchanged != run {
		t.Fatalf("empty spec changed run properties:\n%s", unchanged)
	}

	updated, changed := ApplyRunProperties(run, RunPropertiesSpec{FontSizeHalfPoints: 28})
	if !changed {
		t.Fatal("ApplyRunProperties() changed = false, want true")
	}
	for _, preserved := range []string{
		`<w:rFonts w:eastAsia="宋体"/>`,
		`<w:b/>`,
		`<w:i/>`,
		`<w:color w:val="FF0000"/>`,
		`<w:szCs w:val="24"/>`,
		`<w:vertAlign w:val="superscript"/>`,
	} {
		if !strings.Contains(updated, preserved) {
			t.Fatalf("updated run removed unspecified property %s:\n%s", preserved, updated)
		}
	}
	if !strings.Contains(updated, `<w:sz w:val="28"/>`) || strings.Contains(updated, `<w:sz w:val="24"/>`) {
		t.Fatalf("updated run did not replace specified properties:\n%s", updated)
	}

	updated, changed = ApplyRunProperties(run, RunPropertiesSpec{VerticalAlign: "subscript"})
	if !changed ||
		!strings.Contains(updated, `<w:vertAlign w:val="subscript"/>`) ||
		strings.Contains(updated, `<w:vertAlign w:val="superscript"/>`) {
		t.Fatalf("updated run did not replace explicit vertical alignment:\n%s", updated)
	}
}

func TestApplyParagraphPropertiesPreservesUnspecifiedPropertiesAndAttributes(t *testing.T) {
	paragraph := `<w:p><w:pPr><w:keepNext w:val="false" data-custom="keep"/><w:spacing w:before="120" w:after="240" w:line="360" data-custom="spacing"/><w:ind w:left="720" w:firstLine="480" data-custom="indent"/><w:jc w:val="left"/><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsiaTheme="minorEastAsia" w:cs="Arial" data-custom="fonts"/><w:vertAlign w:val="superscript"/></w:rPr></w:pPr><w:r><w:t>x</w:t></w:r></w:p>`

	updated, changed := ApplyParagraphProperties(paragraph, ParagraphPropertiesSpec{Alignment: "center"})
	if !changed {
		t.Fatal("ApplyParagraphProperties() changed = false, want true")
	}
	for _, preserved := range []string{
		`<w:keepNext w:val="false" data-custom="keep"/>`,
		`w:before="120"`,
		`w:after="240"`,
		`w:line="360"`,
		`data-custom="spacing"`,
		`w:left="720"`,
		`w:firstLine="480"`,
		`data-custom="indent"`,
		`w:ascii="Calibri"`,
		`w:hAnsi="Calibri"`,
		`w:eastAsiaTheme="minorEastAsia"`,
		`w:cs="Arial"`,
		`data-custom="fonts"`,
		`<w:vertAlign w:val="superscript"/>`,
	} {
		if !strings.Contains(updated, preserved) {
			t.Fatalf("alignment-only patch removed unspecified property %s:\n%s", preserved, updated)
		}
	}
	if !strings.Contains(updated, `<w:jc w:val="center"/>`) {
		t.Fatalf("alignment was not updated:\n%s", updated)
	}
}

func TestApplyRunPropertiesMergesFontSlotsAndPreservesUnknownAttributes(t *testing.T) {
	run := `<w:r><w:rPr><w:rFonts w:asciiTheme="minorHAnsi" w:hAnsi="Old Latin" w:eastAsiaTheme="minorEastAsia" w:csTheme="minorBidi" w:hint="eastAsia" data-custom="keep"/><w:vertAlign w:val="subscript"/></w:rPr><w:t>x</w:t></w:r>`

	updated, changed := ApplyRunProperties(run, RunPropertiesSpec{
		AsciiFont:   "Template Latin",
		ComplexFont: "Template Complex",
	})
	if !changed {
		t.Fatal("ApplyRunProperties() changed = false, want true")
	}
	for _, want := range []string{
		`w:ascii="Template Latin"`,
		`w:hAnsi="Old Latin"`,
		`w:eastAsiaTheme="minorEastAsia"`,
		`w:cs="Template Complex"`,
		`w:hint="eastAsia"`,
		`data-custom="keep"`,
		`<w:vertAlign w:val="subscript"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("font-slot patch removed or missed %s:\n%s", want, updated)
		}
	}
	for _, removed := range []string{`w:asciiTheme=`, `w:csTheme=`, `w:cstheme=`} {
		if strings.Contains(updated, removed) {
			t.Fatalf("concrete font must remove only its competing theme slot %s:\n%s", removed, updated)
		}
	}
}

func TestApplySectionPropertiesMergesOnlyRequestedPageAttributes(t *testing.T) {
	document := `<w:document><w:body><w:sectPr><w:pgSz w:w="12240" w:h="15840" w:orient="landscape" data-custom="size"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" data-custom="margin"/><w:pgNumType w:fmt="lowerRoman" w:start="3"/></w:sectPr></w:body></w:document>`

	updated, changed := ApplySectionProperties(document, SectionPropertiesSpec{MarginTopTwips: 720})
	if !changed {
		t.Fatal("ApplySectionProperties() changed = false, want true")
	}
	for _, want := range []string{
		`w:top="720"`,
		`w:right="1440"`,
		`w:bottom="1440"`,
		`w:left="1440"`,
		`w:header="720"`,
		`w:footer="720"`,
		`data-custom="margin"`,
		`w:w="12240"`,
		`w:h="15840"`,
		`w:orient="landscape"`,
		`data-custom="size"`,
		`w:fmt="lowerRoman"`,
		`w:start="3"`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("partial section patch removed %s:\n%s", want, updated)
		}
	}
}

func TestApplyRunPropertiesHonorsBooleanTriState(t *testing.T) {
	run := `<w:r><w:rPr><w:b/><w:bCs/><w:i/><w:iCs/><w:vertAlign w:val="superscript"/></w:rPr><w:t>x</w:t></w:r>`

	updated, changed := ApplyRunProperties(run, RunPropertiesSpec{
		BoldSet: true, ItalicSet: true,
	})
	if !changed {
		t.Fatal("ApplyRunProperties() changed = false, want true")
	}
	for _, want := range []string{
		`<w:b w:val="false"/>`,
		`<w:bCs w:val="false"/>`,
		`<w:i w:val="false"/>`,
		`<w:iCs w:val="false"/>`,
		`<w:vertAlign w:val="superscript"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("explicit false missing %s:\n%s", want, updated)
		}
	}
	for _, unwanted := range []string{`<w:b/>`, `<w:bCs/>`, `<w:i/>`, `<w:iCs/>`} {
		if strings.Contains(updated, unwanted) {
			t.Fatalf("old true property remained %s:\n%s", unwanted, updated)
		}
	}

	updated, _ = ApplyRunProperties(updated, RunPropertiesSpec{
		Bold: true, BoldSet: true, Italic: true, ItalicSet: true,
	})
	for _, want := range []string{`<w:b/>`, `<w:bCs/>`, `<w:i/>`, `<w:iCs/>`} {
		if !strings.Contains(updated, want) {
			t.Fatalf("explicit true missing %s:\n%s", want, updated)
		}
	}

	paragraph, changed := ApplyParagraphProperties(`<w:p><w:pPr/><w:r><w:t>x</w:t></w:r></w:p>`, ParagraphPropertiesSpec{
		RunPropertiesInPPr: true,
		BoldSet:            true,
		ItalicSet:          true,
	})
	if !changed ||
		!strings.Contains(paragraph, `<w:b w:val="false"/>`) ||
		!strings.Contains(paragraph, `<w:iCs w:val="false"/>`) {
		t.Fatalf("paragraph run properties lost explicit false state:\n%s", paragraph)
	}
}

func TestApplyThreeLineTableBordersPreservesGridAndRemovesVerticalBorders(t *testing.T) {
	table := `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders><w:left w:val="single"/><w:right w:val="single"/><w:insideV w:val="single"/></w:tblBorders></w:tblPr><w:tblGrid><w:gridCol w:w="2400"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>H</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`

	updated, changed := ApplyThreeLineTableBorders(table, TableBordersSpec{TopSize: 12, HeaderSize: 6, BottomSize: 12})

	if !changed {
		t.Fatal("ApplyThreeLineTableBorders() changed = false, want true")
	}
	for _, want := range []string{
		`<w:tblW w:w="0" w:type="auto"/>`,
		`<w:tblGrid><w:gridCol w:w="2400"/></w:tblGrid>`,
		`<w:top w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
		`<w:insideH w:val="single" w:sz="6" w:space="0" w:color="000000"/>`,
		`<w:bottom w:val="single" w:sz="12" w:space="0" w:color="000000"/>`,
		`<w:left w:val="nil"/>`,
		`<w:right w:val="nil"/>`,
		`<w:insideV w:val="nil"/>`,
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated table missing %s:\n%s", want, updated)
		}
	}
}
