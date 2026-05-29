package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"gitee.com/greatmusicians/unioffice/document"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	wzdoc "github.com/nineya/wordZero/pkg/document"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
	"github.com/paper-format-checker/backend/pkg/formatchecker"
	"gorm.io/gorm"
)

// PaperHandler 论文处理器
type PaperHandler struct {
	paperService            service.PaperService
	templateParserService   service.TemplateParserService
	formatComparisonService service.FormatComparisonService
	formatParserService     *service.FormatParserService
	settingService          service.SystemSettingService
	config                  *config.Config
}

var (
	processingQueue = make(chan struct{}, 1) // DeepSeek 单 cookie 限制并发为 1
	queueMu         sync.Mutex
	queueSize       int // 当前排队等待的数量
)

// NewPaperHandler 创建论文处理器实例
func NewPaperHandler(config *config.Config) *PaperHandler {
	fps := service.NewFormatParserService()
	fps.InitAIClient(config.DeepSeek.Cookie, config.DeepSeek.Bearer, config.DeepSeek.Enabled)

	return &PaperHandler{
		paperService:            service.NewPaperService(config),
		templateParserService:   service.NewTemplateParserService(),
		formatComparisonService: service.NewFormatComparisonService(),
		formatParserService:     fps,
		settingService:          service.GetSystemSettingService(),
		config:                  config,
	}
}

// UploadPaperRequest 上传论文请求结构体
type UploadPaperRequest struct {
	Title              string                 `form:"title" json:"title" binding:"omitempty,min=1,max=255"`
	Description        string                 `form:"description" json:"description" binding:"omitempty"`
	FormatStandardID   uuid.UUID              `form:"format_standard_id" json:"format_standard_id" binding:"omitempty"`
	TemplateID         int64                  `form:"template_id" json:"template_id" binding:"omitempty"`     // 前端传递的高校ID
	DocumentType       string                 `form:"document_type" json:"document_type" binding:"omitempty"` // 文档类型：本科论文、硕士论文
	Subject            string                 `form:"subject" json:"subject" binding:"omitempty"`             // 学科类别：文科、理科
	ParsedRequirements map[string]interface{} `form:"parsed_requirements" json:"parsed_requirements" binding:"omitempty"`
	Requirements       string                 `form:"requirements" json:"requirements" binding:"omitempty"`
}

// FormatCheckRequest 格式检查请求结构体
type FormatCheckRequest struct {
	PaperID          uuid.UUID `json:"paper_id" binding:"required"`
	FormatStandardID uuid.UUID `json:"format_standard_id" binding:"omitempty"`
}

// FormatFixRequest 格式修改请求结构体
type FormatFixRequest struct {
	PaperID     uuid.UUID `json:"paper_id" binding:"required"`
	CheckResult uuid.UUID `json:"check_result_id" binding:"required"`
	FixAll      bool      `json:"fix_all" binding:"omitempty"`
	IssueIDs    []string  `json:"issue_ids" binding:"omitempty"`
}

// fixByTemplateRequest FixByTemplate 入参（支持 multipart 与 JSON）
type fixByTemplateRequest struct {
	TemplateID       int64  `form:"template_id" json:"template_id"`
	FormatConfigJSON string `form:"format_config_json" json:"format_config_json"`
}

const (
	maxIssueIDsPerRequest  = 500
	legacyWritePathMessage = "legacy paper write path has been retired; use /api/v2/papers"
)

func legacyWritePathDisabled() bool {
	return true
}

// uploadPaperExposeDeepSeekRaw 为 true 时，上传成功响应里会多一个字段 deepseek_raw（模型去掉 ``` 围栏后的完整字符串），便于在浏览器 Network → 响应里查看。
// 开启方式：环境变量 DEEPSEEK_DEBUG_RESPONSE=1/true，或 multipart 表单字段 debug_deepseek=1/true。长文 JSON 体积大，勿在生产长期开启。
func uploadPaperExposeDeepSeekRaw(c *gin.Context) bool {
	v := strings.TrimSpace(os.Getenv("DEEPSEEK_DEBUG_RESPONSE"))
	if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
		return true
	}
	d := strings.TrimSpace(c.PostForm("debug_deepseek"))
	return d == "1" || strings.EqualFold(d, "true")
}

// formatTemplateGoldenOrFilePath 返回高校格式模板关联的范例 docx 路径（优先黄金模板），供与 DeepSeek 分段结果对照。
func (h *PaperHandler) formatTemplateGoldenOrFilePath(paper *model.Paper, req UploadPaperRequest) string {
	var tid uuid.UUID
	if paper.SelectedTemplateID != nil {
		tid = *paper.SelectedTemplateID
	} else if req.FormatStandardID != uuid.Nil {
		tid = req.FormatStandardID
	} else {
		return ""
	}
	var t model.FormatTemplate
	if err := database.DB.Where("id = ?", tid).First(&t).Error; err != nil {
		return ""
	}
	if p := strings.TrimSpace(t.GoldenTemplatePath); p != "" {
		return p
	}
	return strings.TrimSpace(t.FilePath)
}

// UploadPaper 上传论文；后台异步格式修正经 paperService → ApplyCorrectionsV2，引擎由 pkg/formatengine 编译期常量控制（与 Handler 无直接耦合）。
func (h *PaperHandler) UploadPaper(c *gin.Context) {
	if legacyWritePathDisabled() {
		utils.ErrorResponse(c, http.StatusGone, legacyWritePathMessage, "")
		return
	}

	// 付费/白名单校验；不通过时已写 HTTP 响应，此处直接返回
	if !h.uploadPaperPaymentGateAllows(c) {
		return
	}

	// 解析表单、校验文件类型与大小，组装 UploadPaperRequest
	userID, req, file, fileType, err := h.validateUploadRequest(c)
	// 校验失败时 parseAndValidateUploadFile 等已写错误响应
	if err != nil {
		return
	}

	// 落盘文件并创建 paper 记录（multipart：file + title/description 等表单字段，不是 JSON 里的 paper 对象）
	paper, err := h.performFileUpload(c, userID, req, file, fileType)
	if err != nil {
		return
	}
	// 绑定高校模板，便于解析 GoldenTemplatePath / 格式标准
	req = h.applyUniversityTemplateToUploadRequest(paper, req)

	// 仅 doc/docx 可抽文本做结构分析；PDF 等跳过
	var (
		paperSections          []service.PaperSectionSegment
		paperSectionsReady     bool
		deepseekRawBody        string
		segmentFormatHints     []service.PaperSegmentFormatHint
		templateFormatsRawBody string
		wordzeroOutPath        string
	)
	if fileType == "docx" || fileType == "doc" {
		paperpath := filepath.Clean(paper.FilePath)
		doc, derr := document.Open(paperpath)
		if derr != nil {
			log.Printf("[上传论文] 打开 docx 做章节抽取失败（不阻断上传）: %v", derr)
		} else {
			func() {
				defer doc.Close()
				var builder strings.Builder
				for _, para := range doc.Paragraphs() {
					var paraText string
					for _, run := range para.Runs() {
						paraText += run.Text()
					}
					if paraText != "" {
						builder.WriteString(paraText)
						builder.WriteString("\n")
					}
				}
				for _, table := range doc.Tables() {
					for _, row := range table.Rows() {
						for _, cell := range row.Cells() {
							for _, cellPara := range cell.Paragraphs() {
								var cellText string
								for _, run := range cellPara.Runs() {
									cellText += run.Text()
								}
								if cellText != "" {
									builder.WriteString(cellText)
									builder.WriteString(" ")
								}
							}
						}
						builder.WriteString("\n")
					}
				}
				formatSpecText := strings.TrimSpace(builder.String())
				if formatSpecText == "" {
					formatSpecText = strings.TrimSpace(req.Requirements)
				}
				if formatSpecText != "" {
					sections, rawBody, aiErr := h.formatParserService.ParseFormatWithAI(formatSpecText, service.FormatAIPromptKindPaperSections)
					deepseekRawBody = rawBody
					if aiErr != nil {
						log.Printf("[上传论文] 论文章节抽取未执行或失败（不阻断上传）: %v", aiErr)
					} else if sections != nil {
						if segs, ok := sections["segments"].([]service.PaperSectionSegment); ok {
							paperSections = segs
							paperSectionsReady = true
							log.Printf("[上传论文] 论文章节抽取完成，segments %d 条", len(paperSections))
						}
					}
				} else {
					log.Printf("[上传论文] docx 提取与 requirements 均为空，跳过论文章节抽取")
				}
			}()
		}
	}

	//  DeepSeek：对照模板论文版式，得到与学生稿 segments 一一对齐的 segment_formats（再交给 WordZero）
	if paperSectionsReady && len(paperSections) > 0 {
		tmplPath := h.formatTemplateGoldenOrFilePath(paper, req)
		var tmplPlain string
		if tmplPath != "" {
			if t, err := h.extractTextFromDOCXRobust(tmplPath); err != nil {
				log.Printf("[上传论文] 模板 docx 抽取文本失败（仍可用默认格式对齐）: %v", err)
			} else {
				tmplPlain = t
			}
		} else {
			log.Printf("[上传论文] 未解析到模板文件路径（golden/file），segment_formats 使用按 key 默认对齐")
		}
		hints, rawTF, tfErr := h.formatParserService.ParseTemplateSegmentFormatsAI(tmplPlain, paperSections)
		segmentFormatHints = hints
		templateFormatsRawBody = rawTF
		if tfErr != nil {
			log.Printf("[上传论文] 模板分段格式 DeepSeek: %v", tfErr)
		}
	}

	//  使用wordzero  和规则  和设计对应的规则来实现新的符合格式的.docx
	//  按学生稿 segments 顺序写段；每段样式与 segment_formats 同下标一一对应（来自模板 DeepSeek 或默认）。
	if paperSectionsReady && len(paperSections) > 0 && len(segmentFormatHints) == len(paperSections) {
		doc := wzdoc.New()
		if err := doc.SetPageMargins(25.4, 25.4, 25.4, 31.7); err != nil {
			log.Printf("[上传论文] WordZero SetPageMargins: %v", err)
		}
		if err := doc.SetPageSize(wzdoc.PageSizeA4); err != nil {
			log.Printf("[上传论文] WordZero SetPageSize: %v", err)
		}
		for _, seg := range paperSections {

			switch seg.Key {
			case "cover":
				fullText := seg.Text
				para := doc.AddParagraph(fullText)
				para.AddFormattedText(fullText, &wzdoc.TextFormat{
					FontFamily: "宋体",
					FontSize:   18,
					Bold:       false,
					Italic:     false,
					FontColor:  "000000",
				})

			case "abstract_cn":
				// 处理中文摘要
				para := doc.AddParagraph(seg.Text)
				// 设置段落格式：首行缩进2字符、段后两行、1.5倍行距、两端对齐
				para.SetSpacing(&wzdoc.SpacingConfig{
					FirstLineIndent: 24,  // 小四号2字符≈24pt
					AfterPara:       36,  // 段后两行（1.5倍行距下，两行=36pt）
					LineSpacing:     1.5, // 1.5倍行距
				})
				para.SetAlignment(wzdoc.AlignJustify) // 两端对齐

				// 添加“Abstract:”部分（Times New Roman 小三加粗）
				para.AddFormattedText("Abstract: ", &wzdoc.TextFormat{
					FontFamily: "Times New Roman",
					FontSize:   15, // 小三号 = 15pt
					Bold:       true,
					FontColor:  "000000",
				})

				// 添加英文摘要内容（Times New Roman 小四号）
				para.AddFormattedText(seg.Text, &wzdoc.TextFormat{
					FontFamily: "Times New Roman",
					FontSize:   12, // 小四号 = 12pt
					Bold:       false,
					FontColor:  "000000",
				})
			case "keywords_cn":
				para := doc.AddParagraph(seg.Text)
				// 设置段落格式：首行缩进2字符、1.5倍行距、段后两行
				para.SetSpacing(&wzdoc.SpacingConfig{
					FirstLineIndent: 24,  // 首行缩进2字符（小四号≈24pt）
					LineSpacing:     1.5, // 1.5倍行距
					AfterPara:       36,  // 段后两行（1.5倍行距下，两行=36pt）
				})

				// 添加“关键词：”标签（黑体小三加粗）
				para.AddFormattedText("关键词：", &wzdoc.TextFormat{
					FontFamily: "黑体", // 或 "SimHei"
					FontSize:   15,   // 小三号 = 15pt
					Bold:       true,
					FontColor:  "000000",
				})

				// 添加关键词内容（宋体小四号，假设 seg.Text 已用中文分号分隔）
				para.AddFormattedText(seg.Text, &wzdoc.TextFormat{
					FontFamily: "宋体", // 或 "SimSun"
					FontSize:   12,   // 小四号 = 12pt
					Bold:       false,
					FontColor:  "000000",
				})

			case "toc":
				// 1. 添加“目录”标题
				titlePara := doc.AddParagraph(seg.Text)
				titlePara.AddFormattedText("目录", &wzdoc.TextFormat{
					FontFamily: "黑体",
					FontSize:   16, // 三号 = 16pt
					Bold:       true,
					FontColor:  "000000",
				})
				titlePara.SetAlignment(wzdoc.AlignCenter) // 居中
				titlePara.SetSpacing(&wzdoc.SpacingConfig{
					AfterPara: 24, // 段后2行（三号字下2行约48pt？此处取24pt合适，可按需调整）
				})

				// 2. 添加目录内容（自动生成 TOC 字段）
				tocPara := doc.AddParagraph(seg.Text)
				// 设置目录段落样式：宋体五号、1.5倍行距、两端对齐
				tocPara.SetSpacing(&wzdoc.SpacingConfig{
					LineSpacing: 1.5, // 1.5倍行距
				})
				tocPara.SetAlignment(wzdoc.AlignJustify) // 两端对齐

			case "heading_1":
				para := doc.AddParagraph(seg.Text)
				// 设置段落对齐：顶格（左对齐）
				para.SetAlignment(wzdoc.AlignLeft)

				// 设置段落间距：段前24pt、段后24pt、1.5倍行距
				para.SetSpacing(&wzdoc.SpacingConfig{
					BeforePara:  24,  // 段前1行 ≈ 24pt
					AfterPara:   24,  // 段后1行 ≈ 24pt
					LineSpacing: 1.5, // 1.5倍行距
				})

				// 添加文本并应用字体格式
				para.AddFormattedText(seg.Text, &wzdoc.TextFormat{
					FontFamily: "宋体", // 或 "SimSun"
					FontSize:   16,   // 三号 = 16pt
					Bold:       true,
					FontColor:  "000000", // 黑色
				})
			case "heading_2":
				para := doc.AddParagraph(seg.Text)
				// 左对齐（顶格）
				para.SetAlignment(wzdoc.AlignLeft)

				// 设置段落间距：1.5倍行距，段前段后均为0
				para.SetSpacing(&wzdoc.SpacingConfig{
					LineSpacing: 1.5, // 1.5倍行距
					// BeforePara 和 AfterPara 不设置或设为0即无空行
				})

				// 添加文本并应用字体格式
				para.AddFormattedText(seg.Text, &wzdoc.TextFormat{
					FontFamily: "宋体", // 或 "SimSun"
					FontSize:   15,   // 小三号 = 15pt
					Bold:       true,
					FontColor:  "000000",
				})
			case "heading_3":
				para := doc.AddParagraph(seg.Text)

				// 左对齐（顶格）
				para.SetAlignment(wzdoc.AlignLeft)

				// 设置段落间距：1.5倍行距，段前段后均为0（不设置即默认为0）
				para.SetSpacing(&wzdoc.SpacingConfig{
					LineSpacing: 1.5, // 1.5倍行距
					// BeforePara 和 AfterPara 不写或写0均可
				})

				// 添加文本并应用字体格式
				para.AddFormattedText(seg.Text, &wzdoc.TextFormat{
					FontFamily: "宋体", // 或 "SimSun"
					FontSize:   14,   // 四号 = 14pt
					Bold:       true,
					FontColor:  "000000",
				})
			case "body":
				// 处理正文
				para := doc.AddParagraph(seg.Text)
				para.SetSpacing(&wzdoc.SpacingConfig{
					FirstLineIndent: 24,
					LineSpacing:     1.5,
				})
				para.SetAlignment(wzdoc.AlignJustify)

				// 添加正文示例（带注释和引用）

			}

			//txt := strings.TrimSpace(seg.Text)
			//if txt == "" {
			//	continue
			//}
			//h := segmentFormatHints[k]
			//tf := &wzdoc.TextFormat{
			//	FontFamily: h.FontFamily,
			//	FontSize:   h.FontSizePt,
			//	Bold:       h.Bold,
			//}
			//para := doc.AddFormattedParagraph(txt, tf)
			//para.SetSpacing(&wzdoc.SpacingConfig{
			//	FirstLineIndent: h.FirstLineIndentPt,
			//	LineSpacing:     h.LineSpacingMultiple,
			//	BeforePara:      h.BeforeParaPt,
			//	AfterPara:       h.AfterParaPt,
			//})

		}

		// 与配置 UploadPath 一致（默认 ./uploads），勿用 \uploads\... —— 在 Windows 上会落到「当前盘符根目录」如 C:\uploads\...，不在项目里
		dir := filepath.Join(filepath.Clean(h.config.File.UploadPath), "papers", "update")
		outPath := filepath.Join(dir, fmt.Sprintf("thesis_wz_formatted_%s.docx", paper.ID.String()))
		absOut, _ := filepath.Abs(outPath)
		log.Printf("[上传论文] WordZero 将保存到: %s（相对: %s）", absOut, filepath.ToSlash(outPath))
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[上传论文] WordZero 创建输出目录失败: %v", err)
		} else if err := doc.Save(outPath); err != nil {
			log.Printf("[上传论文] WordZero Save 失败: %v", err)
		} else {
			wordzeroOutPath = filepath.ToSlash(outPath)
			log.Printf("[上传论文] WordZero 已生成（segments+模板格式对齐）: %s", absOut)
		}
	} else if fileType == "docx" || fileType == "doc" {
		// 便于排查：未进 WordZero 时也能在控制台看到原因（成功时不会走这里）
		switch {
		case !paperSectionsReady:
			log.Printf("[上传论文] WordZero 未执行：学生稿 DeepSeek 分段未成功（paperSectionsReady=false）")
		case len(paperSections) == 0:
			log.Printf("[上传论文] WordZero 未执行：segments 为空")
		case len(segmentFormatHints) != len(paperSections):
			log.Printf("[上传论文] WordZero 未执行：section_formats 条数 %d 与 segments %d 不一致", len(segmentFormatHints), len(paperSections))
		}
	}

	database.DB.Model(paper).Update("status", "processing")

	pos := h.bumpUploadPaperQueue()
	if pos > 1 {
		log.Printf("[处理队列] paper %s 排队中（前方 %d 个任务）", paper.ID, pos-1)
	}

	go h.runUploadPaperAsyncJob(userID, paper, req)

	// 前端应使用 file_download_url + Authorization 下载原件；paper.file_path 为服务端相对路径，勿当地址栏直接打开
	resp := gin.H{
		"paper":             paper,
		"file_download_url": fmt.Sprintf("/api/v1/papers/%s/file", paper.ID.String()),
		"message":           "paper uploaded successfully, format processing started in background",
		"status":            "processing",
	}
	if paperSectionsReady {
		// 按论文阅读顺序的片段数组，便于客户端顺序拼接
		resp["sections"] = paperSections
	}
	if len(segmentFormatHints) > 0 && len(segmentFormatHints) == len(paperSections) {
		// 与 sections 按下标一一对应，供前端/WordZero 对照
		resp["section_formats"] = segmentFormatHints
	}
	if uploadPaperExposeDeepSeekRaw(c) && deepseekRawBody != "" {
		resp["deepseek_raw"] = deepseekRawBody
	}
	if uploadPaperExposeDeepSeekRaw(c) && templateFormatsRawBody != "" {
		resp["template_formats_raw"] = templateFormatsRawBody
	}
	if wordzeroOutPath != "" {
		resp["formatted_wz_docx"] = wordzeroOutPath
	}
	utils.Created(c, resp)
}

// uploadPaperPaymentGateAllows 在已写响应时返回 false（调用方应直接 return）。
func (h *PaperHandler) uploadPaperPaymentGateAllows(c *gin.Context) bool {
	// 读取系统「是否免费检查」等支付相关配置
	paymentConfig, err := h.settingService.GetPaymentConfig()
	if err != nil {
		utils.InternalServerError(c, fmt.Sprintf("获取支付配置失败: %v", err))
		return false
	}
	// 配置项可能缺失或非 bool，缺省视为免费
	isCheckFree, ok := paymentConfig["is_check_free"].(bool)
	if !ok {
		isCheckFree = true
	}
	// 免费开放则直接允许上传
	if isCheckFree {
		return true
	}
	// 收费模式下：免费用户/管理员/已有支付订单则放行
	if h.uploadPaperUserHasPaidAccess(c) {
		return true
	}
	// 否则返回 402 及价格信息 JSON
	h.respondUploadPaperPaymentRequired(c, paymentConfig, isCheckFree)
	return false
}

func (h *PaperHandler) uploadPaperUserHasPaidAccess(c *gin.Context) bool {
	// 从 JWT/会话注入的 user 对象判断白名单角色
	if userObj, exists := c.Get("user"); exists {
		if u, ok := userObj.(*model.User); ok && (u.IsFreeUser || u.Role == "admin") {
			return true // 免费用户或管理员免付费校验
		}
	}
	// 无白名单则查是否有过已支付订单
	if userID, exists := c.Get("user_id"); exists {
		var paidCount int64
		database.DB.Model(&model.Order{}).
			Where("user_id = ? AND payment_status = ?", userID, "paid").
			Count(&paidCount)
		return paidCount > 0 // 至少一笔已支付即可上传
	}
	return false
}

func (h *PaperHandler) respondUploadPaperPaymentRequired(c *gin.Context, paymentConfig map[string]interface{}, isCheckFree bool) {
	// 从配置读出检查价、修正价（类型断言失败时为 0）
	formatCheckPrice, _ := paymentConfig["format_check"].(float64)
	formatFixPrice, _ := paymentConfig["format_fix"].(float64)
	// 前端可据此展示应付金额
	paymentInfo := gin.H{
		"is_check_free":    isCheckFree,                       // 与当前配置一致回传
		"format_check":     formatCheckPrice,                  // 单项：格式检查
		"format_fix":       formatFixPrice,                    // 单项：格式修正
		"total_amount":     formatCheckPrice + formatFixPrice, // 合计参考价
		"payment_required": true,                              // 明确需先付费
	}
	paymentInfoJSON, _ := json.Marshal(paymentInfo)
	utils.ErrorResponse(c, http.StatusPaymentRequired, "论文格式检查和修正需要付费", string(paymentInfoJSON))
}

func (h *PaperHandler) bumpUploadPaperQueue() int {
	queueMu.Lock()         // 保护全局 queueSize
	defer queueMu.Unlock() // 函数任意返回路径都解锁
	queueSize++            // 当前处于「已接受上传、可能排队/处理中」的任务数 +1
	return queueSize       // 返回递增后的队列深度（含当前任务）
}

func (h *PaperHandler) runUploadPaperAsyncJob(uid interface{}, p *model.Paper, r UploadPaperRequest) {
	defer func() {
		// panic 时回写状态与错误摘要，避免记录永远卡在 processing
		if rec := recover(); rec != nil {
			log.Printf("[异步处理] paper %s panic: %v", p.ID, rec)
			database.DB.Model(p).Updates(map[string]interface{}{
				"status":      "uploaded",
				"parsed_info": fmt.Sprintf(`{"async_error":"%v"}`, rec),
			})
		}
		// 释放并发槽位并减小队列计数
		<-processingQueue
		queueMu.Lock()
		queueSize--
		queueMu.Unlock()
	}()

	// 占用信号量，限制同时处理的论文数
	processingQueue <- struct{}{}
	log.Printf("[处理队列] paper %s 开始处理", p.ID)

	// 有高校模板且 V2 快速修正成功则直接结束，不再走完整后处理
	if r.TemplateID != 0 && h.tryQuickV2FixAfterUpload(p, r) {
		return
	}

	// 格式检查、自动修正、对比报告等（c 传 nil 表示非 HTTP 请求上下文）
	response, _ := h.handlePostUploadProcessing(nil, uid, p, r)
	h.finalizeUploadPaperAsyncFromResponse(p, response)
}

func (h *PaperHandler) tryQuickV2FixAfterUpload(p *model.Paper, r UploadPaperRequest) bool {
	// 尝试基于模板一键出修正稿路径
	fixedPath, err := h.paperService.QuickV2Fix(p.FilePath, r.TemplateID)
	if err == nil && fixedPath != "" {
		database.DB.Model(p).Updates(map[string]interface{}{
			"status":              "corrected",
			"corrected_file_path": fixedPath,
		})
		return true
	}

	return false
}

func (h *PaperHandler) finalizeUploadPaperAsyncFromResponse(p *model.Paper, response gin.H) {
	// 后处理未返回有效 map
	if response == nil {
		h.markUploadAsyncFinishedNoResult(p)
		return
	}
	// 若响应里已有修正文件路径，标记为已修正
	if corrPath, ok := response["corrected_file_path"]; ok {
		if pathStr, ok := corrPath.(string); ok && pathStr != "" {
			database.DB.Model(p).Updates(map[string]interface{}{
				"status":              "corrected",
				"corrected_file_path": pathStr,
			})
			log.Printf("[处理队列] paper %s 处理完成（已修正）", p.ID)
			return
		}
	}
	// 仅有检查结果无修正稿时标记为已检查
	if _, ok := response["check_result"]; ok {
		database.DB.Model(p).Update("status", "checked")
		log.Printf("[处理队列] paper %s 处理完成（已检查）", p.ID)
		return
	}
	h.markUploadAsyncFinishedNoResult(p)
}

func (h *PaperHandler) markUploadAsyncFinishedNoResult(p *model.Paper) {
	database.DB.Model(p).Update("status", "uploaded")
	log.Printf("[处理队列] paper %s 处理完成（无结果）", p.ID)
}

// validateUploadRequest 验证上传请求参数
func (h *PaperHandler) validateUploadRequest(c *gin.Context) (interface{}, UploadPaperRequest, *multipart.FileHeader, string, error) {
	// 上传接口依赖鉴权中间件写入的 user_id
	userID, _ := c.Get("user_id")
	file, fileType, err := h.parseAndValidateUploadFile(c)
	if err != nil {
		return nil, UploadPaperRequest{}, nil, "", err
	}
	req := h.uploadRequestFromForm(c)
	// 根据 template_id 或 format_standard_id 补全 FormatStandardID
	h.attachUploadFormatStandardID(c, &req)
	return userID, req, file, fileType, nil
}

func (h *PaperHandler) parseAndValidateUploadFile(c *gin.Context) (*multipart.FileHeader, string, error) {
	// multipart 中名为 file 的字段
	file, err := c.FormFile("file")
	if err != nil {
		utils.BadRequest(c, "file is required")
		return nil, "", err
	}
	// 统一小写扩展名便于分支判断
	fileExt := strings.ToLower(filepath.Ext(file.Filename))
	var fileType string
	switch fileExt {
	case ".pdf":
		fileType = "pdf" // PDF 仅检查/预览类流程
	case ".docx":
		fileType = "docx" // 优先支持的 Word 格式
	case ".doc":
		fileType = "doc" // 旧版 Word
	default:
		utils.BadRequest(c, "invalid file type, only pdf, doc and docx are allowed")
		return nil, "", fmt.Errorf("invalid file type")
	}
	// 与配置中的 MaxSize 比较，超限则拒绝
	if file.Size > h.config.File.MaxSize {
		utils.BadRequest(c, "file size exceeds limit")
		return nil, "", fmt.Errorf("file size exceeds limit")
	}
	return file, fileType, nil
}

func (h *PaperHandler) uploadRequestFromForm(c *gin.Context) UploadPaperRequest {
	return UploadPaperRequest{
		Title:        c.PostForm("title"),                         // 论文标题
		Description:  c.PostForm("description"),                   // 说明
		TemplateID:   parseInt64OrZero(c.PostForm("template_id")), // 高校/模板侧 ID（数字）
		DocumentType: c.PostForm("document_type"),                 // 文档类型，如本科论文
		Subject:      c.PostForm("subject"),                       // 学科
		Requirements: c.PostForm("requirements"),                  // 原始格式要求文本（若有）
	}
}

func (h *PaperHandler) attachUploadFormatStandardID(c *gin.Context, req *UploadPaperRequest) {
	if req.TemplateID != 0 {
		var template model.FormatTemplate
		docType := req.DocumentType
		if docType == "" {
			docType = "本科论文" // 与模板查询默认文档类型一致
		}
		if err := database.DB.Where("university_id = ? AND is_active = ? AND document_type = ?",
			req.TemplateID, true, docType).First(&template).Error; err == nil {
			req.FormatStandardID = template.ID // 后续检查/修正用标准 UUID
		}
		return // 已按 template 解析则不再读 format_standard_id
	}
	if formatStandardIDStr := c.PostForm("format_standard_id"); formatStandardIDStr != "" {
		if formatStandardID, err := uuid.Parse(formatStandardIDStr); err == nil {
			req.FormatStandardID = formatStandardID
		}
	}
}

// 辅助函数：安全地解析int64
func parseInt64OrZero(s string) int64 {
	if s == "" {
		return 0 // 缺省表单项视为 0
	}
	if val, err := strconv.ParseInt(s, 10, 64); err == nil {
		return val
	}
	return 0 // 非数字字符串不报错，避免上传因模板 ID 填错而整体失败
}

// performFileUpload 执行文件上传操作
func (h *PaperHandler) performFileUpload(c *gin.Context, userID interface{}, req UploadPaperRequest, file *multipart.FileHeader, fileType string) (*model.Paper, error) {
	// Service 层负责存储路径、哈希与 DB 插入
	paper, err := h.paperService.UploadPaper(
		userID.(uuid.UUID),
		req.Title,
		req.Description,
		req.FormatStandardID,
		file,
		fileType,
		c, // 传递context用于保存文件
	)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return nil, err
	}

	return paper, nil // 成功则返回已持久化的 Paper 模型
}

// handlePostUploadProcessing 处理上传后的后续操作
func (h *PaperHandler) handlePostUploadProcessing(c *gin.Context, userID interface{}, paper *model.Paper, req UploadPaperRequest) (gin.H, error) {
	// 若带了高校 template_id，解析对应活跃模板并绑定到 paper
	req = h.applyUniversityTemplateToUploadRequest(paper, req)

	if req.FormatStandardID != uuid.Nil {
		checkResult, early := h.checkPaperFormatForPostUpload(userID, paper, req.FormatStandardID)
		// 检查失败时 early 非 nil，仍返回「上传成功」语义的结构体
		if early != nil {
			return early, nil
		}
		return h.buildPostUploadResponseAfterCheck(userID, paper, checkResult), nil // 有格式标准：检查 + 可能自动修正
	}

	if req.ParsedRequirements != nil && len(req.ParsedRequirements) > 0 {
		return h.buildPostUploadResponseFromParsedRequirements(userID, paper, req), nil // 无标准 UUID 但有解析后的规则 JSON
	}

	return h.buildPostUploadResponseAfterCheck(userID, paper, nil), nil // 无标准也无解析规则：仅基础响应
}

// applyUniversityTemplateToUploadRequest 按高校 TemplateID 解析格式标准并写回 paper.selected_template_id。
func (h *PaperHandler) applyUniversityTemplateToUploadRequest(paper *model.Paper, req UploadPaperRequest) UploadPaperRequest {
	if req.TemplateID == 0 {
		return req // 未选高校模板则跳过
	}

	var template model.FormatTemplate
	query := database.DB.Where("university_id = ? AND is_active = ?", req.TemplateID, true)
	if req.DocumentType != "" {
		query = query.Where("document_type = ?", req.DocumentType)
	} else {
		query = query.Where("document_type = ?", "本科论文")
	}
	if req.Subject != "" {
		query = query.Where("subject = ?", req.Subject)
	}

	if err := query.First(&template).Error; err == nil {
		h.bindPaperToFormatTemplate(paper, &req, template)
		return req
	}

	fallbackErr := database.DB.Where("university_id = ? AND is_active = ?", req.TemplateID, true).First(&template).Error
	if fallbackErr == nil {
		h.bindPaperToFormatTemplate(paper, &req, template) // 放宽学科/文档类型后的兜底模板
		return req
	}
	fmt.Printf("Warning: Could not find active template for university ID %d: %v\n", req.TemplateID, fallbackErr)
	return req
}

func (h *PaperHandler) bindPaperToFormatTemplate(paper *model.Paper, req *UploadPaperRequest, template model.FormatTemplate) {
	req.FormatStandardID = template.ID
	paper.SelectedTemplateID = &template.ID
	database.DB.Model(paper).Update("selected_template_id", template.ID) // 持久化用户选用的模板
}

// checkPaperFormatForPostUpload 成功时返回 (result, nil)；检查失败时返回 (nil, 仍视为上传成功的 gin.H)。
func (h *PaperHandler) checkPaperFormatForPostUpload(userID interface{}, paper *model.Paper, formatStandardID uuid.UUID) (*model.CheckResult, gin.H) {
	checkResult, err := h.paperService.CheckPaperFormat(userID.(uuid.UUID), paper.ID, formatStandardID)
	if err != nil {
		// 检查异常不抛给客户端为 500，而是附带在 JSON 里说明上传已成功
		return nil, gin.H{
			"paper":   paper,
			"message": "paper uploaded successfully, but format check failed",
			"error":   fmt.Sprintf("failed to perform automatic format check: %v", err),
		}
	}
	return checkResult, nil
}

func (h *PaperHandler) buildPostUploadResponseFromParsedRequirements(userID interface{}, paper *model.Paper, req UploadPaperRequest) gin.H {
	fixResult, err := h.paperService.FixPaperFormatByParsedRequirements(userID.(uuid.UUID), paper.ID, req.ParsedRequirements)
	if err != nil {
		fmt.Printf("Fix by parsed requirements failed: %v\n", err)
	}
	response := gin.H{
		"paper":   paper,
		"message": "paper uploaded and format fix completed successfully",
	}
	if result, ok := fixResult.(map[string]interface{}); ok {
		if path, exists := result["corrected_file_path"]; exists {
			response["corrected_file_path"] = path
			response["download_url"] = fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()) // 供前端拉修正稿
		}
	}
	return response
}

func (h *PaperHandler) buildPostUploadResponseAfterCheck(userID interface{}, paper *model.Paper, checkResult *model.CheckResult) gin.H {
	response := gin.H{
		"paper":        paper,
		"check_result": checkResult,
		"message":      "paper uploaded and format check completed successfully",
	}
	if checkResult != nil {
		response["comparison_url"] = fmt.Sprintf("/api/v1/papers/%s/comparison/%s", paper.ID.String(), checkResult.ID.String())
	} else {
		response["comparison_url"] = "" // 无检查记录则对比链接为空
	}
	h.appendPostUploadAutoFixAndComparison(response, userID, paper, checkResult)
	return response
}

func (h *PaperHandler) appendPostUploadAutoFixAndComparison(response gin.H, userID interface{}, paper *model.Paper, checkResult *model.CheckResult) {
	if checkResult == nil {
		return
	}
	if paper.Status != "corrected" {
		h.appendPostUploadAutoFixResult(response, userID, paper, checkResult)
	} else {
		fmt.Printf("Paper %s already corrected during upload, skipping duplicate fix\n", paper.ID.String())
	}
	if diffReport, err := h.formatComparisonService.GenerateFormatDifferences(checkResult.ID); err == nil {
		response["format_comparison"] = diffReport // 结构化差异，供前端展示
	}
}

func (h *PaperHandler) appendPostUploadAutoFixResult(response gin.H, userID interface{}, paper *model.Paper, checkResult *model.CheckResult) {
	fixResult, err := h.paperService.FixPaperFormat(userID.(uuid.UUID), paper.ID, checkResult.ID)
	if err != nil {
		fmt.Printf("Auto fix failed: %v\n", err)
		response["fix_error"] = err.Error()
		return
	}
	response["fix_result"] = fixResult
	response["download_url"] = fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String())
	if result, ok := fixResult.(*service.CorrectionResult); ok && result != nil {
		response["corrected_file_path"] = result.CorrectedFilePath
	} else if resultMap, ok := fixResult.(map[string]interface{}); ok {
		if path, exists := resultMap["corrected_file_path"]; exists {
			response["corrected_file_path"] = path // 与 finalizeUploadPaperAsyncFromResponse 联动更新 DB
		}
	}
}

// GetPapers 获取用户的论文列表
func (h *PaperHandler) GetPapers(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	// 获取论文列表
	papers, total, err := h.paperService.GetPapersByUserID(userID.(uuid.UUID), page, pageSize)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{
		"papers":    papers,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetPaper 获取论文详情
func (h *PaperHandler) GetPaper(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取论文详情
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, paper)
}

// DeletePaper 删除论文
func (h *PaperHandler) DeletePaper(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 删除论文
	if err := h.paperService.DeletePaper(paperID, userID.(uuid.UUID)); err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, gin.H{"message": "paper deleted successfully"})
}

// CheckFormat 检查论文格式
func (h *PaperHandler) CheckFormat(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 解析请求参数
	var req FormatCheckRequest
	// 设置默认值
	req.PaperID = paperID
	if err := c.ShouldBind(&req); err != nil {
		// 如果没有请求体，使用URL中的ID
		req.PaperID = paperID
	}

	// 如果请求中没有指定论文ID，使用URL中的ID
	if req.PaperID == uuid.Nil {
		req.PaperID = paperID
	}

	// 检查论文格式
	checkResult, err := h.paperService.CheckPaperFormat(userID.(uuid.UUID), req.PaperID, req.FormatStandardID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, checkResult)
}

// FixFormat 修复论文格式
func (h *PaperHandler) FixFormat(c *gin.Context) {
	if legacyWritePathDisabled() {
		utils.ErrorResponse(c, http.StatusGone, legacyWritePathMessage, "")
		return
	}

	userID, _ := c.Get("user_id")
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}
	req, err := bindFormatFixRequest(c, paperID)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	normalizedIssueIDs, err := normalizeIssueIDs(req.IssueIDs)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	if err := validateFormatFixMode(req.FixAll, normalizedIssueIDs); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	fixResult, err := h.paperService.FixPaperFormatWithOptions(
		userID.(uuid.UUID),
		req.PaperID,
		req.CheckResult,
		service.FixPaperFormatOptions{
			FixAll:   req.FixAll,
			IssueIDs: normalizedIssueIDs,
		},
	)
	if err != nil {
		if isFixInputError(err) {
			utils.BadRequest(c, err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"message": "格式修复成功",
		"result":  fixResult,
	})
}

func bindFormatFixRequest(c *gin.Context, paperID uuid.UUID) (FormatFixRequest, error) {
	var req FormatFixRequest
	req.PaperID = paperID
	if err := c.ShouldBindJSON(&req); err != nil {
		return FormatFixRequest{}, err
	}
	if req.PaperID == uuid.Nil {
		req.PaperID = paperID
	}
	if req.PaperID != paperID {
		return FormatFixRequest{}, fmt.Errorf("paper_id in body must match url id")
	}
	return req, nil
}

func validateFormatFixMode(fixAll bool, normalizedIssueIDs []string) error {
	if fixAll && len(normalizedIssueIDs) > 0 {
		return fmt.Errorf("fix_all and issue_ids cannot be provided at the same time")
	}
	if !fixAll && len(normalizedIssueIDs) == 0 {
		return fmt.Errorf("issue_ids is required when fix_all is false")
	}
	return nil
}

// GetFormatStandards 获取格式标准列表，支持按机构和文档类型过滤
func (h *PaperHandler) GetFormatStandards(c *gin.Context) {
	page, pageSize := readFormatStandardsPagination(c)
	query := formatStandardsBaseQuery(c)
	var ok bool
	query, ok = applyFormatStandardsFilters(c, query)
	if !ok {
		return
	}
	sortBy, order := resolveFormatStandardsSort(c)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		utils.InternalServerError(c, "failed to count format standards")
		return
	}

	var templates []model.FormatTemplate
	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order(sortBy + " " + order).Find(&templates).Error; err != nil {
		utils.InternalServerError(c, "failed to fetch format standards")
		return
	}

	utils.Success(c, gin.H{
		"items":     formatTemplatesToListItems(templates),
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func readFormatStandardsPagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func formatStandardsBaseQuery(c *gin.Context) *gorm.DB {
	q := database.DB.Model(&model.FormatTemplate{}).Preload("University")
	if !strings.Contains(c.FullPath(), "/admin/") {
		q = q.Where("is_active = ? AND is_public = ?", true, true)
	}
	return q
}

// applyFormatStandardsFilters 应用查询参数；若已写 400 响应则返回 ok=false。
func applyFormatStandardsFilters(c *gin.Context, query *gorm.DB) (*gorm.DB, bool) {
	if universityID := c.Query("university_id"); universityID != "" {
		uid, err := strconv.ParseInt(universityID, 10, 64)
		if err != nil {
			utils.BadRequest(c, "invalid university_id")
			return query, false
		}
		query = query.Where("university_id = ?", uid)
	}
	if documentType := strings.TrimSpace(c.Query("document_type")); documentType != "" {
		query = query.Where("document_type = ?", documentType)
	}
	if subject := strings.TrimSpace(c.Query("subject")); subject != "" {
		query = query.Where("subject = ?", subject)
	}
	if source := strings.TrimSpace(c.Query("source")); source != "" {
		query = query.Where("source = ?", source)
	}
	if isActiveStr := c.Query("is_active"); isActiveStr != "" {
		isActive, err := strconv.ParseBool(isActiveStr)
		if err != nil {
			utils.BadRequest(c, "invalid is_active")
			return query, false
		}
		query = query.Where("is_active = ?", isActive)
	}
	if isPublicStr := c.Query("is_public"); isPublicStr != "" {
		isPublic, err := strconv.ParseBool(isPublicStr)
		if err != nil {
			utils.BadRequest(c, "invalid is_public")
			return query, false
		}
		query = query.Where("is_public = ?", isPublic)
	}
	if keyword := strings.TrimSpace(c.Query("keyword")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"name ILIKE ? OR description ILIKE ? OR template_id ILIKE ?",
			like, like, like,
		)
	}
	return query, true
}

func resolveFormatStandardsSort(c *gin.Context) (sortBy, order string) {
	sortBy = c.DefaultQuery("sort_by", "created_at")
	order = strings.ToLower(c.DefaultQuery("order", "desc"))
	if order != "asc" {
		order = "desc"
	}
	allowed := map[string]bool{
		"created_at":  true,
		"updated_at":  true,
		"name":        true,
		"version":     true,
		"usage_count": true,
	}
	if !allowed[sortBy] {
		sortBy = "created_at"
	}
	return sortBy, order
}

func formatTemplatesToListItems(templates []model.FormatTemplate) []gin.H {
	items := make([]gin.H, 0, len(templates))
	for _, t := range templates {
		universityName := ""
		if t.University != nil {
			universityName = t.University.Name
		}
		items = append(items, gin.H{
			"id":            t.ID,
			"template_id":   t.TemplateID,
			"name":          t.Name,
			"university_id": t.UniversityID,
			"university":    universityName,
			"document_type": t.DocumentType,
			"subject":       t.Subject,
			"source":        t.Source,
			"version":       t.Version,
			"enabled":       t.IsActive,
			"is_active":     t.IsActive,
			"is_public":     t.IsPublic,
			"usage_count":   t.UsageCount,
			"success_rate":  t.SuccessRate,
			"description":   t.Description,
			"created_at":    t.CreatedAt,
			"updated_at":    t.UpdatedAt,
		})
	}
	return items
}

// GetPaperCheckResults 获取论文的检查结果列表
func (h *PaperHandler) GetPaperCheckResults(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取检查结果列表
	results, err := h.paperService.GetPaperCheckResults(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, results)
}

func logPaperFileDownload(label string, paperID uuid.UUID, cleanPath string) {
	absPath, _ := filepath.Abs(cleanPath)
	fmt.Printf("%s: ID=%s, Path=%s, AbsPath=%s\n", label, paperID, cleanPath, absPath)
}

func ensurePaperFileExistsForDownload(c *gin.Context, cleanPath string, notFoundMsg string) bool {
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		utils.ErrorResponse(c, http.StatusNotFound, notFoundMsg, fmt.Sprintf("Path: %s, Error: %v", cleanPath, err))
		return false
	}
	return true
}

func setLocalPaperAttachmentHeaders(c *gin.Context, cleanPath string) {
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(cleanPath)))
	switch strings.ToLower(filepath.Ext(cleanPath)) {
	case ".docx":
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".doc":
		c.Header("Content-Type", "application/msword")
	case ".pdf":
		c.Header("Content-Type", "application/pdf")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}
}

func sendLocalPaperFileAttachment(c *gin.Context, cleanPath string) {
	setLocalPaperAttachmentHeaders(c, cleanPath)
	c.File(cleanPath)
}

// GetPaperFile 获取论文文件
func (h *PaperHandler) GetPaperFile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}
	cleanPath := filepath.Clean(paper.FilePath)
	logPaperFileDownload("Download Paper", paperID, cleanPath)
	if !ensurePaperFileExistsForDownload(c, cleanPath, "文件在服务器上不存在") {
		return
	}
	sendLocalPaperFileAttachment(c, cleanPath)
}

// GetCorrectedPaperFile 获取修正后的论文文件
func (h *PaperHandler) GetCorrectedPaperFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		utils.Unauthorized(c, "未登录")
		return
	}
	uid, ok := userID.(uuid.UUID)
	if !ok {
		utils.Unauthorized(c, "invalid user id")
		return
	}
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}
	filePath, err := h.paperService.ResolveCorrectedPaperFile(uid, paperID)
	if err != nil {
		if errors.Is(err, service.ErrCorrectedFileNotFound) {
			utils.ErrorResponse(c, http.StatusNotFound, "修正后的文件在服务器上不存在", err.Error())
			return
		}
		utils.InternalServerError(c, err.Error())
		return
	}
	cleanPath := filepath.Clean(filePath)
	logPaperFileDownload("Download Corrected Paper", paperID, cleanPath)
	if !ensurePaperFileExistsForDownload(c, cleanPath, "修正后的文件在服务器上不存在") {
		return
	}
	sendLocalPaperFileAttachment(c, cleanPath)
}

// ComparePaperFormats 对比论文格式
func (h *PaperHandler) ComparePaperFormats(c *gin.Context) {
	userID, _ := c.Get("user_id")
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 获取检查结果ID
	checkResultID, err := h.resolveCheckResultID(c, userID.(uuid.UUID), paperID)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 对比论文格式
	comparison, err := h.formatComparisonService.GenerateFormatDifferences(checkResultID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, comparison)
}

// ExportCorrectedPaper 导出修正后的论文
func (h *PaperHandler) ExportCorrectedPaper(c *gin.Context) {
	if legacyWritePathDisabled() {
		utils.ErrorResponse(c, http.StatusGone, legacyWritePathMessage, "")
		return
	}

	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	// 导出修正后的论文
	filePath, err := h.paperService.ExportCorrectedPaper(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回文件
	c.File(filePath)
}

// ExportCheckReport 导出检查报告
func (h *PaperHandler) ExportCheckReport(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	checkResultID, err := h.resolveCheckResultID(c, userID.(uuid.UUID), paperID)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	format, reportType, ok := parseCheckReportQuery(c)
	if !ok {
		return
	}

	// 导出检查报告
	var report string
	switch format {
	case "json":
		report, err = h.paperService.ExportCheckReportJSON(userID.(uuid.UUID), checkResultID, reportType)
	case "html":
		report, err = h.paperService.ExportCheckReportHTML(userID.(uuid.UUID), checkResultID, reportType)
	default:
		report, err = h.paperService.ExportCheckReport(userID.(uuid.UUID), checkResultID)
	}
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回响应
	utils.Success(c, report)
}

// DownloadCheckReport 下载检查报告
func (h *PaperHandler) DownloadCheckReport(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 获取论文ID
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	checkResultID, err := h.resolveCheckResultID(c, userID.(uuid.UUID), paperID)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	format, reportType, ok := parseCheckReportQuery(c)
	if !ok {
		return
	}
	if err := h.streamCheckReportAsAttachment(c, userID.(uuid.UUID), checkResultID, format, reportType); err != nil {
		utils.InternalServerError(c, err.Error())
	}
}

// DownloadCheckReportHTML 下载HTML格式检查报告
func (h *PaperHandler) DownloadCheckReportHTML(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}

	checkResultID, err := h.resolveCheckResultID(c, userID.(uuid.UUID), paperID)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	reportType := strings.ToLower(strings.TrimSpace(c.DefaultQuery("type", "report")))
	if !isAllowedReportType(reportType) {
		utils.BadRequest(c, "invalid report type, allowed: report, correction-report")
		return
	}
	report, err := h.paperService.ExportCheckReportHTML(userID.(uuid.UUID), checkResultID, reportType)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	c.Header("Content-Disposition", "attachment; filename=check_report.html")
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(200, report)
}

func (h *PaperHandler) resolveCheckResultID(c *gin.Context, userID, paperID uuid.UUID) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Param("check_result_id"))
	if raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid check result id")
		}
		if _, err := h.paperService.GetCheckResultForPaperUser(userID, paperID, parsed); err != nil {
			return uuid.Nil, fmt.Errorf("check result not found for current paper")
		}
		return parsed, nil
	}

	if queryID := strings.TrimSpace(c.Query("check_result_id")); queryID != "" {
		parsed, err := uuid.Parse(queryID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid check result id")
		}
		if _, err := h.paperService.GetCheckResultForPaperUser(userID, paperID, parsed); err != nil {
			return uuid.Nil, fmt.Errorf("check result not found for current paper")
		}
		return parsed, nil
	}

	latestID, err := h.paperService.ResolveLatestCheckResultID(userID, paperID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve latest check result")
	}
	return latestID, nil
}

func normalizeIssueIDs(issueIDs []string) ([]string, error) {
	if len(issueIDs) > maxIssueIDsPerRequest {
		return nil, fmt.Errorf("too many issue_ids, max %d", maxIssueIDsPerRequest)
	}
	normalized := make([]string, 0, len(issueIDs))
	seen := make(map[string]struct{}, len(issueIDs))
	for _, raw := range issueIDs {
		issueID := strings.TrimSpace(raw)
		if issueID == "" {
			continue
		}
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		normalized = append(normalized, issueID)
	}
	return normalized, nil
}

func isFixInputError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "issue_ids") ||
		strings.Contains(msg, "no issue selected") ||
		strings.Contains(msg, "no fixable issues") ||
		strings.Contains(msg, "status is not completed") ||
		strings.Contains(msg, "check result")
}

func isAllowedReportType(v string) bool {
	return v == "report" || v == "correction-report"
}

func isAllowedReportFormat(v string) bool {
	return v == "txt" || v == "html" || v == "json"
}

// parseCheckReportQuery 解析 format / type 查询参数；非法时已写 BadRequest，返回 ok=false。
func parseCheckReportQuery(c *gin.Context) (format string, reportType string, ok bool) {
	format = strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "txt")))
	reportType = strings.ToLower(strings.TrimSpace(c.DefaultQuery("type", "report")))
	if !isAllowedReportFormat(format) {
		utils.BadRequest(c, "invalid report format, allowed: txt, html, json")
		return "", "", false
	}
	if !isAllowedReportType(reportType) {
		utils.BadRequest(c, "invalid report type, allowed: report, correction-report")
		return "", "", false
	}
	return format, reportType, true
}

func (h *PaperHandler) streamCheckReportAsAttachment(c *gin.Context, userID, checkResultID uuid.UUID, format, reportType string) error {
	switch format {
	case "json":
		report, err := h.paperService.ExportCheckReportJSON(userID, checkResultID, reportType)
		if err != nil {
			return err
		}
		c.Header("Content-Disposition", "attachment; filename=check_report.json")
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.String(http.StatusOK, report)
		return nil
	case "html":
		report, err := h.paperService.ExportCheckReportHTML(userID, checkResultID, reportType)
		if err != nil {
			return err
		}
		c.Header("Content-Disposition", "attachment; filename=check_report.html")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, report)
		return nil
	default:
		report, err := h.paperService.ExportCheckReport(userID, checkResultID)
		if err != nil {
			return err
		}
		c.Header("Content-Disposition", "attachment; filename=check_report.txt")
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusOK, report)
		return nil
	}
}

func (h *PaperHandler) HandleCQCECFormat(c *gin.Context) {
	// 从上下文获取用户ID
	userID, _ := c.Get("user_id")

	// 解析请求参数
	var req CQCECFormatRequest
	if err := c.ShouldBind(&req); err != nil {
		utils.BadRequest(c, "文件上传失败: "+err.Error())
		return
	}

	// 验证文件类型
	fileExt := strings.ToLower(filepath.Ext(req.File.Filename))
	if fileExt != ".docx" {
		utils.BadRequest(c, "仅支持DOCX格式文件")
		return
	}

	// 验证文件大小
	if req.File.Size > h.config.File.MaxSize {
		utils.BadRequest(c, "文件大小超过限制")
		return
	}

	// 创建临时论文记录
	tempPaperID := uuid.New()
	_ = &model.Paper{
		ID:          tempPaperID,
		UserID:      userID.(uuid.UUID),
		Title:       "重庆工程学院格式检查 - " + req.File.Filename,
		Description: "重庆工程学院本科毕业设计（论文）格式检查",
		FileName:    req.File.Filename,
		FileType:    "docx",
		Status:      "processing",
	}

	// 保存上传的文件
	filePath, err := h.saveUploadedFile(c, req.File, tempPaperID.String())
	if err != nil {
		utils.InternalServerError(c, "文件保存失败: "+err.Error())
		return
	}

	// 创建临时论文记录到数据库
	paper := &model.Paper{
		ID:                    tempPaperID,
		UserID:                userID.(uuid.UUID),
		Title:                 "重庆工程学院格式检查 - " + req.File.Filename,
		Description:           "重庆工程学院本科毕业设计（论文）格式检查",
		FileName:              req.File.Filename,
		FileType:              "docx",
		FilePath:              filePath,
		Status:                "uploaded",
		ParsedInfo:            "{}",
		AutoDetectedTemplates: "[]",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	// 保存论文记录到数据库
	if err := database.DB.Create(paper).Error; err != nil {
		utils.InternalServerError(c, "论文记录创建失败: "+err.Error())
		return
	}

	// 使用paperService进行格式检查
	checkResult, err := h.paperService.CheckPaperFormat(userID.(uuid.UUID), tempPaperID, uuid.Nil)
	if err != nil {
		utils.InternalServerError(c, "格式检查失败: "+err.Error())
		return
	}

	// 解析检查结果中的问题列表
	var issues []formatchecker.FormatIssue
	if err := json.Unmarshal([]byte(checkResult.Issues), &issues); err != nil {
		utils.InternalServerError(c, "检查结果解析失败: "+err.Error())
		return
	}

	// 生成修正建议
	_, err = h.paperService.FixPaperFormat(userID.(uuid.UUID), tempPaperID, checkResult.ID)
	if err != nil {
		// 即使修正失败，也返回检查结果
		response := CQCECFormatResponse{
			Success: true,
			Message: "格式检查完成，但修正文件生成失败: " + err.Error(),
			Issues:  issues,
		}
		utils.Success(c, response)
		return
	}

	// 返回成功响应
	response := CQCECFormatResponse{
		Success: true,
		Message: "格式检查完成",
		Issues:  issues,
	}
	utils.Success(c, response)
}

// CreateFormatStandard 创建格式标准
func (h *PaperHandler) CreateFormatStandard(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		UniversityID *int64 `json:"university_id"`
		DocumentType string `json:"document_type"`
		FormatRules  string `json:"format_rules" binding:"required"`
		Description  string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	newTemplateID := uuid.New().String()

	template := &model.FormatTemplate{
		TemplateID:   newTemplateID, // 使用纯UUID
		Name:         req.Name,
		UniversityID: req.UniversityID,
		DocumentType: req.DocumentType,
		Source:       "system",
		IsPublic:     true,
		IsActive:     true,
		FormatRules:  req.FormatRules,
		Description:  req.Description,
	}

	if err := database.DB.Create(template).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建成功", template)
}

// GetFormatStandardForDisplay 获取格式标准用于前端展示
func (h *PaperHandler) GetFormatStandardForDisplay(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.Preload("University").First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "模板不存在", err.Error())
		return
	}

	// 解析格式规则
	var formatRules map[string]interface{}
	if err := json.Unmarshal([]byte(template.FormatRules), &formatRules); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "解析格式规则失败", err.Error())
		return
	}

	// 转换为友好展示格式
	friendlyFormat := friendlyConvertFormatRulesForDisplay(formatRules)

	// 构建响应
	response := gin.H{
		"id":            template.ID,
		"name":          template.Name,
		"university":    template.University,
		"document_type": template.DocumentType,
		"description":   template.Description,
		"format_rules":  friendlyFormat,
	}

	utils.SuccessResponse(c, "获取成功", response)
}

// UpdateFormatStandard 更新格式标准
func (h *PaperHandler) UpdateFormatStandard(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var req struct {
		Name         string `json:"name"`
		UniversityID *int64 `json:"university_id"`
		DocumentType string `json:"document_type"`
		FormatRules  string `json:"format_rules"`
		Description  string `json:"description"`
		IsActive     *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	updateData := make(map[string]interface{})
	if req.Name != "" {
		updateData["name"] = req.Name
	}
	if req.UniversityID != nil {
		updateData["university_id"] = *req.UniversityID
	}
	if req.DocumentType != "" {
		updateData["document_type"] = req.DocumentType
	}
	if req.FormatRules != "" {
		updateData["format_rules"] = req.FormatRules
	}
	if req.Description != "" {
		updateData["description"] = req.Description
	}
	if req.IsActive != nil {
		updateData["is_active"] = *req.IsActive
	}

	if err := database.DB.Model(&model.FormatTemplate{}).Where("id = ?", templateUUID).Updates(updateData).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

// DeleteFormatStandard 删除格式标准
func (h *PaperHandler) DeleteFormatStandard(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	if err := database.DB.Model(&model.FormatTemplate{}).Where("id = ?", templateUUID).Update("is_active", false).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除格式标准失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// GetFormatStandardByID 根据ID获取格式标准
func (h *PaperHandler) GetFormatStandardByID(c *gin.Context) {
	templateID := c.Param("id")
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的模板ID", err.Error())
		return
	}

	var template model.FormatTemplate
	if err := database.DB.Preload("University").First(&template, "id = ?", templateUUID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "格式标准不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", template)
}
