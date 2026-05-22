package formatchecker

// docx_style_resolver.go
//
// 解决 Word 样式继承链问题：
// Word 文档中，字体/字号/行距通常定义在"段落样式"（style）中，
// 而非每个 run 的内联属性。原来的 getRunFontName/getRunFontSize/getLineSpacing
// 只读取内联属性，导致大量漏检（返回空值 → fontsMatch 误判为匹配）。
//
// 本文件提供：
//   1. docxStyleCache   —— 解析 word/styles.xml 得到每个样式的字体/字号/行距
//   2. loadDocxStyleCache(docPath) —— 从 DOCX ZIP 加载样式缓存
//   3. DOCXChecker.resolveRunFont/resolveRunSize/resolveLineSpacing —— 带继承链的属性解析

import (
	"archive/zip"
	"regexp"
	"strconv"
	"strings"

	"gitee.com/greatmusicians/unioffice/document"
)

// ─── 数据结构 ───────────────────────────────────────────────────────────────

// styleProps 某个 Word 样式的原始属性（未经继承合并）
type styleProps struct {
	EastAsiaFont string  // w:rFonts w:eastAsia
	AsciiFont    string  // w:rFonts w:ascii
	FontSizePt   float64 // w:sz w:val / 2
	LinePt       float64 // w:spacing w:line / 20
	Bold         bool    // w:b 存在
	BasedOn      string  // w:basedOn w:val（父样式 ID）
	Name         string  // w:name w:val（样式显示名）
}

// docxStyleCache 样式表缓存，Key 为 styleId（如 "Normal", "Heading1"）
type docxStyleCache struct {
	styles   map[string]*styleProps // styleId → 原始属性
	nameToID map[string]string      // lowercased name → styleId
	defaults styleProps             // w:docDefaults
}

// ─── 解析 ────────────────────────────────────────────────────────────────────

var (
	reStyleBlock   = regexp.MustCompile(`(?s)<w:style\b[^>]*>(.*?)</w:style>`)
	reStyleID      = regexp.MustCompile(`w:styleId="([^"]+)"`)
	reStyleName    = regexp.MustCompile(`<w:name\s+w:val="([^"]+)"`)
	reBasedOn      = regexp.MustCompile(`<w:basedOn\s+w:val="([^"]+)"`)
	reEastAsia     = regexp.MustCompile(`w:eastAsia="([^"]+)"`)
	reAsciiFont    = regexp.MustCompile(`w:ascii="([^"]+)"`)
	reFontSize     = regexp.MustCompile(`<w:sz\b[^/]*w:val="(\d+)"`)
	reLineSpacing  = regexp.MustCompile(`<w:spacing\b[^>]*w:line="(\d+)"`)
	reBold         = regexp.MustCompile(`<w:b(?:\s|/>|>)`)
	reDocDefaults  = regexp.MustCompile(`(?s)<w:docDefaults>(.*?)</w:docDefaults>`)
)

// parseStyleBlock 从单个 <w:style> XML 块解析属性
func parseStyleBlock(block string) (id string, props styleProps) {
	if m := reStyleID.FindStringSubmatch(block); len(m) > 1 {
		id = m[1]
	}
	if m := reStyleName.FindStringSubmatch(block); len(m) > 1 {
		props.Name = m[1]
	}
	if m := reBasedOn.FindStringSubmatch(block); len(m) > 1 {
		props.BasedOn = m[1]
	}
	if m := reEastAsia.FindStringSubmatch(block); len(m) > 1 {
		props.EastAsiaFont = m[1]
	}
	if m := reAsciiFont.FindStringSubmatch(block); len(m) > 1 {
		props.AsciiFont = m[1]
	}
	if m := reFontSize.FindStringSubmatch(block); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			props.FontSizePt = v / 2.0 // half-points → points
		}
	}
	if m := reLineSpacing.FindStringSubmatch(block); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			props.LinePt = v / 20.0 // twips → points
		}
	}
	props.Bold = reBold.MatchString(block)
	return
}

// buildStyleCache 解析 word/styles.xml 文本，构建样式缓存
func buildStyleCache(stylesXML string) *docxStyleCache {
	cache := &docxStyleCache{
		styles:   make(map[string]*styleProps),
		nameToID: make(map[string]string),
	}

	// 解析文档默认值
	if m := reDocDefaults.FindStringSubmatch(stylesXML); len(m) > 1 {
		_, def := parseStyleBlock(m[1])
		cache.defaults = def
	}

	// 解析每个样式块
	matches := reStyleBlock.FindAllStringSubmatch(stylesXML, -1)
	for _, full := range matches {
		outerBlock := full[0]
		id, props := parseStyleBlock(outerBlock)
		if id == "" {
			continue
		}
		p := props
		cache.styles[id] = &p
		if props.Name != "" {
			cache.nameToID[strings.ToLower(props.Name)] = id
		}
		// 同时存 lowercased id 方便查找
		cache.nameToID[strings.ToLower(id)] = id
	}
	return cache
}

// resolve 获取某个样式（含继承链）合并后的属性
// 子样式覆盖父样式（先找到先用），最后回退到 docDefaults
func (sc *docxStyleCache) resolve(styleID string) styleProps {
	if styleID == "" {
		return sc.defaults
	}
	// 规范化：先尝试精确匹配，再尝试 lowercase 映射
	realID := styleID
	if _, ok := sc.styles[realID]; !ok {
		if mapped, ok2 := sc.nameToID[strings.ToLower(styleID)]; ok2 {
			realID = mapped
		}
	}

	result := styleProps{}
	visited := map[string]bool{}
	current := realID

	for current != "" && !visited[current] {
		visited[current] = true
		p, ok := sc.styles[current]
		if !ok {
			break
		}
		if result.EastAsiaFont == "" && p.EastAsiaFont != "" {
			result.EastAsiaFont = p.EastAsiaFont
		}
		if result.AsciiFont == "" && p.AsciiFont != "" {
			result.AsciiFont = p.AsciiFont
		}
		if result.FontSizePt == 0 && p.FontSizePt > 0 {
			result.FontSizePt = p.FontSizePt
		}
		if result.LinePt == 0 && p.LinePt > 0 {
			result.LinePt = p.LinePt
		}
		if !result.Bold && p.Bold {
			result.Bold = true
		}
		current = p.BasedOn
	}

	// 回退到文档默认值
	if result.EastAsiaFont == "" {
		result.EastAsiaFont = sc.defaults.EastAsiaFont
	}
	if result.AsciiFont == "" {
		result.AsciiFont = sc.defaults.AsciiFont
	}
	if result.FontSizePt == 0 {
		result.FontSizePt = sc.defaults.FontSizePt
	}
	if result.LinePt == 0 {
		result.LinePt = sc.defaults.LinePt
	}
	return result
}

// ─── 加载 ─────────────────────────────────────────────────────────────────────

// loadDocxStyleCache 从 DOCX ZIP 文件中加载样式缓存
// 失败时返回空缓存（不影响主流程）
func loadDocxStyleCache(docPath string) *docxStyleCache {
	zr, err := zip.OpenReader(docPath)
	if err != nil {
		return &docxStyleCache{styles: make(map[string]*styleProps), nameToID: make(map[string]string)}
	}
	defer zr.Close()

	for _, f := range zr.File {
		if strings.ToLower(f.Name) != "word/styles.xml" {
			continue
		}
		content, err := readZipFileAsString(f)
		if err != nil {
			break
		}
		return buildStyleCache(content)
	}
	return &docxStyleCache{styles: make(map[string]*styleProps), nameToID: make(map[string]string)}
}

// ─── DOCXChecker 解析方法 ──────────────────────────────────────────────────────

// getParagraphStyleID 获取段落的样式 ID（来自 <w:pStyle w:val="..."/>）
func getParagraphStyleID(para document.Paragraph) string {
	pPr := para.X().PPr
	if pPr == nil || pPr.PStyle == nil {
		return ""
	}
	return pPr.PStyle.ValAttr
}

// resolveRunFont 带样式继承的字体名称解析
// 优先级：run 内联 → 段落样式继承链 → 文档默认值
func (c *DOCXChecker) resolveRunFont(run document.Run, para document.Paragraph) string {
	// 1. run 内联属性
	if font := getRunFontName(run); font != "" {
		return font
	}
	// 2. 段落样式继承链
	if c.styleCache != nil {
		if styleID := getParagraphStyleID(para); styleID != "" {
			props := c.styleCache.resolve(styleID)
			if props.EastAsiaFont != "" {
				return props.EastAsiaFont
			}
		}
		// 3. 文档默认值
		if c.styleCache.defaults.EastAsiaFont != "" {
			return c.styleCache.defaults.EastAsiaFont
		}
	}
	return ""
}

// resolveRunSize 带样式继承的字号解析（返回磅值）
func (c *DOCXChecker) resolveRunSize(run document.Run, para document.Paragraph) float64 {
	// 1. run 内联属性
	if sz := getRunFontSize(run); sz > 0 {
		return sz
	}
	// 2. run 级别的 rPr 可能有 sz（但无 Para 关联），先检查 para 级别
	if pPr := para.X().PPr; pPr != nil {
		// 段落级别的 rPr
		if pPr.RPr != nil && pPr.RPr.Sz != nil && pPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber != nil {
			return float64(*pPr.RPr.Sz.ValAttr.ST_UnsignedDecimalNumber) / 2.0
		}
	}
	// 3. 段落样式继承链
	if c.styleCache != nil {
		if styleID := getParagraphStyleID(para); styleID != "" {
			props := c.styleCache.resolve(styleID)
			if props.FontSizePt > 0 {
				return props.FontSizePt
			}
		}
		if c.styleCache.defaults.FontSizePt > 0 {
			return c.styleCache.defaults.FontSizePt
		}
	}
	return 0
}

// resolveLineSpacing 带样式继承的行距解析（返回磅值）
func (c *DOCXChecker) resolveLineSpacing(para document.Paragraph) float64 {
	// 1. 段落内联属性
	if sp := getLineSpacing(para); sp > 0 {
		return sp
	}
	// 2. 段落样式继承链
	if c.styleCache != nil {
		if styleID := getParagraphStyleID(para); styleID != "" {
			props := c.styleCache.resolve(styleID)
			if props.LinePt > 0 {
				return props.LinePt
			}
		}
		if c.styleCache.defaults.LinePt > 0 {
			return c.styleCache.defaults.LinePt
		}
	}
	return 0
}
