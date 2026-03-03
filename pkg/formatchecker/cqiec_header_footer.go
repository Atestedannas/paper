package formatchecker

type HeaderFooterConfig struct {
	HeaderDistance    float64           `json:"header_distance"`     // 页眉距离顶边
	FooterDistance    float64           `json:"footer_distance"`     // 页脚距离底边
	FontName          string            `json:"font_name"`           // 字体名称
	FontSize          string            `json:"font_size"`           // 字号
	Underline         bool              `json:"underline"`           // 是否带下划线
	StartFromAbstract bool              `json:"start_from_abstract"` // 是否从摘要页开始
	PrintMode         PrintMode         `json:"print_mode"`          // 打印模式
	FrontMatterHeader FrontMatterHeader `json:"front_matter_header"` // 前言部分页眉
	MainBodyHeader    MainBodyHeader    `json:"main_body_header"`    // 主体部分页眉
	PageNumberConfig  PageNumberConfig  `json:"page_number_config"`  // 页码配置
}

type PrintMode string

const (
	PrintModeSingleSided PrintMode = "single_sided" // 单面打印
	PrintModeDoubleSided PrintMode = "double_sided" // 双面打印
)

type FrontMatterHeader struct {
	Enable    bool   `json:"enable"`     // 是否启用
	Content   string `json:"content"`    // 页眉内容
	LeftPage  string `json:"left_page"`  // 左页页眉内容
	RightPage string `json:"right_page"` // 右页页眉内容
}

type MainBodyHeader struct {
	Enable    bool   `json:"enable"`     // 是否启用
	LeftPage  string `json:"left_page"`  // 左页页眉内容
	RightPage string `json:"right_page"` // 右页页眉内容
}

type PageNumberConfig struct {
	FrontMatterFormat   PageNumberFormat `json:"front_matter_format"`    // 前言部分页码格式
	MainBodyFormat      PageNumberFormat `json:"main_body_format"`       // 主体部分页码格式
	FrontMatterStartNum int              `json:"front_matter_start_num"` // 前言部分起始页码
	MainBodyStartNum    int              `json:"main_body_start_num"`    // 主体部分起始页码
	Position            string           `json:"position"`               // 页码位置
}

type PageNumberFormat string

const (
	PageNumberRoman  PageNumberFormat = "roman"  // 罗马数字
	PageNumberArabic PageNumberFormat = "arabic" // 阿拉伯数字
	PageNumberNone   PageNumberFormat = "none"   // 无页码
)

func CQCECHeaderFooterConfig() HeaderFooterConfig {
	return HeaderFooterConfig{
		HeaderDistance:    1.6,
		FooterDistance:    2.1,
		FontName:          "宋体",
		FontSize:          "五号",
		Underline:         true,
		StartFromAbstract: true,
		PrintMode:         PrintModeSingleSided,
		FrontMatterHeader: FrontMatterHeader{
			Enable:    false,
			Content:   "",
			LeftPage:  "",
			RightPage: "",
		},
		MainBodyHeader: MainBodyHeader{
			Enable:    true,
			LeftPage:  "重庆工程学院本科生毕业设计（论文）",
			RightPage: "",
		},
		PageNumberConfig: PageNumberConfig{
			FrontMatterFormat:   PageNumberRoman,
			MainBodyFormat:      PageNumberArabic,
			FrontMatterStartNum: 1,
			MainBodyStartNum:    1,
			Position:            "center",
		},
	}
}

func CQCECHeaderFooterConfigDoubleSided() HeaderFooterConfig {
	config := CQCECHeaderFooterConfig()
	config.PrintMode = PrintModeDoubleSided
	config.MainBodyHeader = MainBodyHeader{
		Enable:    true,
		LeftPage:  "重庆工程学院本科生毕业设计（论文）",
		RightPage: "{章节名称}",
	}
	return config
}
