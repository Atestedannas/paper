package handler

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

func bindFixByTemplateRequest(c *gin.Context) (fixByTemplateRequest, error) {
	var req fixByTemplateRequest
	if err := c.ShouldBind(&req); err != nil {
		return fixByTemplateRequest{}, err
	}
	return req, nil
}

func (h *PaperHandler) fixByTemplateQuickV2(paper *model.Paper, templateID int64) (interface{}, error) {
	fixedPath, err := h.paperService.QuickV2Fix(paper.FilePath, templateID)
	if err != nil {
		return nil, err
	}
	paper.CorrectedFilePath = fixedPath
	paper.Status = "corrected"
	database.DB.Save(paper)
	return map[string]interface{}{"corrected_file_path": fixedPath}, nil
}

func mergeFixResultCorrectedPathIntoResponse(response gin.H, fixResult interface{}) {
	if result, ok := fixResult.(*service.CorrectionResult); ok && result != nil {
		response["corrected_file_path"] = result.CorrectedFilePath
		return
	}
	if resultMap, ok := fixResult.(map[string]interface{}); ok {
		if path, exists := resultMap["corrected_file_path"]; exists {
			response["corrected_file_path"] = path
		}
	}
}

func (h *PaperHandler) buildFixByTemplateResponse(paper *model.Paper, fixResult interface{}, checkResult *model.CheckResult) gin.H {
	response := gin.H{
		"message":      "格式修复完成",
		"fix_result":   fixResult,
		"download_url": fmt.Sprintf("/api/v1/papers/%s/corrected-file", paper.ID.String()),
	}
	if checkResult != nil {
		response["check_result"] = checkResult
		if diffReport, err := h.formatComparisonService.GenerateFormatDifferences(checkResult.ID); err == nil {
			response["format_comparison"] = diffReport
		}
	}
	mergeFixResultCorrectedPathIntoResponse(response, fixResult)
	return response
}

// FixByTemplate 根据模板修复论文
func (h *PaperHandler) FixByTemplate(c *gin.Context) {
	userID, _ := c.Get("user_id")
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid paper id")
		return
	}
	req, err := bindFixByTemplateRequest(c)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}
	paper, err := h.paperService.GetPaperByID(userID.(uuid.UUID), paperID)
	if err != nil {
		utils.InternalServerError(c, "论文不存在或无权限访问: "+err.Error())
		return
	}

	var fixResult interface{}
	var checkResult *model.CheckResult

	switch {
	case req.TemplateID != 0:
		fixResult, err = h.fixByTemplateQuickV2(paper, req.TemplateID)
		if err != nil {
			utils.InternalServerError(c, "V2格式修正失败: "+err.Error())
			return
		}
	case req.FormatConfigJSON != "":
		var requirements map[string]interface{}
		if err := json.Unmarshal([]byte(req.FormatConfigJSON), &requirements); err != nil {
			utils.BadRequest(c, "格式配置JSON解析失败: "+err.Error())
			return
		}
		fixResult, err = h.paperService.FixPaperFormatByParsedRequirements(userID.(uuid.UUID), paper.ID, requirements)
		if err != nil {
			utils.InternalServerError(c, "格式修正失败: "+err.Error())
			return
		}
	default:
		utils.BadRequest(c, "必须提供template_id或format_config_json")
		return
	}

	utils.Success(c, h.buildFixByTemplateResponse(paper, fixResult, checkResult))
}
