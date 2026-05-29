package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
)

// ReviewDiffs GET /api/papers/:id/review-diffs
// 返回最近一次格式修正后的差异报告（供人工审核）
func (h *PaperHandler) ReviewDiffs(c *gin.Context) {
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "无效的论文ID")
		return
	}
	userID, ok := c.Get("user_id")
	if !ok {
		utils.Unauthorized(c, "未登录")
		return
	}

	var result model.CheckResult
	if err := database.DB.
		Where("paper_id = ? AND user_id = ?", paperID, userID.(uuid.UUID)).
		Order("created_at DESC").First(&result).Error; err != nil {
		utils.NotFound(c, "未找到检查记录，请先进行格式修正")
		return
	}

	diffReportRaw := result.DiffReport
	if diffReportRaw == "" || diffReportRaw == "{}" {
		utils.Success(c, gin.H{
			"paper_id":    paperID,
			"diff_report": nil,
			"message":     "暂无差异报告，请先进行格式修正",
		})
		return
	}

	utils.Success(c, gin.H{
		"paper_id":    paperID,
		"diff_report": json.RawMessage(diffReportRaw),
	})
}

// ApplySelectedDiffs POST /api/papers/:id/apply-diffs
// 对用户选中的段落重新强制应用模板格式，生成新的修正文档
func (h *PaperHandler) ApplySelectedDiffs(c *gin.Context) {
	if legacyWritePathDisabled() {
		utils.ErrorResponse(c, http.StatusGone, legacyWritePathMessage, "")
		return
	}

	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "无效的论文ID")
		return
	}
	userID, ok := c.Get("user_id")
	if !ok {
		utils.Unauthorized(c, "未登录")
		return
	}
	var req ApplySelectedDiffsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	uid := userID.(uuid.UUID)

	var checkResult model.CheckResult
	if err := database.DB.
		Where("paper_id = ? AND user_id = ?", paperID, uid).
		Order("created_at DESC").First(&checkResult).Error; err != nil {
		utils.NotFound(c, "未找到检查记录，请先进行格式检查")
		return
	}

	result, fixErr := h.paperService.FixPaperFormat(uid, paperID, checkResult.ID)
	if fixErr != nil {
		utils.InternalServerError(c, "应用修正失败: "+fixErr.Error())
		return
	}

	utils.Success(c, result)
}
