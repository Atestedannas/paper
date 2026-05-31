package ooxmlpatch

import (
	"fmt"
	"strings"
)

type TablePropertiesSpec struct {
	WidthTwips            int
	Alignment             string
	FixedLayout           bool
	ThreeLine             bool
	CellMarginTwips       int
	CellMarginTopTwips    int
	CellMarginStartTwips  int
	CellMarginBottomTwips int
	CellMarginEndTwips    int
}

type TableCellPropertiesSpec struct {
	WidthTwips        int
	GridSpan          int
	VMerge            string
	VMergeContinue    bool
	VerticalAlign     string
	IncludeAllBorders bool
}

func BuildTableProperties(spec TablePropertiesSpec) string {
	var builder strings.Builder
	builder.WriteString(`<w:tblPr>`)
	if spec.WidthTwips > 0 {
		builder.WriteString(fmt.Sprintf(`<w:tblW w:w="%d" w:type="dxa"/>`, spec.WidthTwips))
	}
	if spec.Alignment != "" {
		builder.WriteString(fmt.Sprintf(`<w:jc w:val="%s"/>`, spec.Alignment))
	}
	if spec.FixedLayout {
		builder.WriteString(`<w:tblLayout w:type="fixed"/>`)
	}
	if spec.ThreeLine {
		builder.WriteString(buildThreeLineBorders(TableBordersSpec{TopSize: 12, HeaderSize: 4, BottomSize: 12}))
	}
	top, start, bottom, end := tableCellMargins(spec)
	if top > 0 || start > 0 || bottom > 0 || end > 0 {
		builder.WriteString(fmt.Sprintf(`<w:tblCellMar><w:top w:w="%d" w:type="dxa"/><w:start w:w="%d" w:type="dxa"/><w:bottom w:w="%d" w:type="dxa"/><w:end w:w="%d" w:type="dxa"/></w:tblCellMar>`, top, start, bottom, end))
	}
	builder.WriteString(`</w:tblPr>`)
	return builder.String()
}

func tableCellMargins(spec TablePropertiesSpec) (int, int, int, int) {
	top := spec.CellMarginTopTwips
	start := spec.CellMarginStartTwips
	bottom := spec.CellMarginBottomTwips
	end := spec.CellMarginEndTwips
	if spec.CellMarginTwips > 0 {
		if top == 0 {
			top = spec.CellMarginTwips
		}
		if start == 0 {
			start = spec.CellMarginTwips
		}
		if bottom == 0 {
			bottom = spec.CellMarginTwips
		}
		if end == 0 {
			end = spec.CellMarginTwips
		}
	}
	return top, start, bottom, end
}

func BuildTableCellProperties(spec TableCellPropertiesSpec) string {
	var builder strings.Builder
	builder.WriteString(`<w:tcPr>`)
	if spec.WidthTwips > 0 {
		builder.WriteString(fmt.Sprintf(`<w:tcW w:w="%d" w:type="dxa"/>`, spec.WidthTwips))
	}
	if spec.GridSpan > 1 {
		builder.WriteString(fmt.Sprintf(`<w:gridSpan w:val="%d"/>`, spec.GridSpan))
	}
	if spec.VMergeContinue || spec.VMerge != "" {
		if spec.VMerge == "" {
			builder.WriteString(`<w:vMerge/>`)
		} else {
			builder.WriteString(fmt.Sprintf(`<w:vMerge w:val="%s"/>`, spec.VMerge))
		}
	}
	if spec.VerticalAlign != "" {
		builder.WriteString(fmt.Sprintf(`<w:vAlign w:val="%s"/>`, spec.VerticalAlign))
	}
	if spec.IncludeAllBorders {
		builder.WriteString(`<w:tcBorders><w:top w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:left w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:bottom w:val="single" w:color="000000" w:sz="4" w:space="0"/><w:right w:val="single" w:color="000000" w:sz="4" w:space="0"/></w:tcBorders>`)
	}
	builder.WriteString(`</w:tcPr>`)
	return builder.String()
}
