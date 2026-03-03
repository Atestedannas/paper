package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

type TaskHandler struct{}

func NewTaskHandler() *TaskHandler {
	return &TaskHandler{}
}

// GetTaskStatus 获取任务状态
func (h *TaskHandler) GetTaskStatus(c *gin.Context) {
	taskID := c.Param("id")

	// 尝试解析为UUID
	id, err := uuid.Parse(taskID)
	if err != nil {
		utils.BadRequest(c, "Invalid task ID")
		return
	}

	// 1. 尝试查找论文 (如果是论文处理任务)
	var paper model.Paper
	if err := database.DB.First(&paper, id).Error; err == nil {
		utils.Success(c, gin.H{
			"id":         paper.ID,
			"type":       "paper_process",
			"status":     paper.Status, // uploaded, processing, checked, corrected
			"progress":   calculateProgress(paper.Status),
			"updated_at": paper.UpdatedAt,
		})
		return
	}

	// 2. 尝试查找检查结果 (如果是检查任务)
	var checkResult model.CheckResult
	if err := database.DB.First(&checkResult, id).Error; err == nil {
		utils.Success(c, gin.H{
			"id":         checkResult.ID,
			"type":       "check_process",
			"status":     checkResult.Status,
			"progress":   100, // CheckResult通常是完成态
			"updated_at": checkResult.UpdatedAt,
		})
		return
	}

	if err == gorm.ErrRecordNotFound {
		utils.NotFound(c, "Task not found")
		return
	}

	utils.InternalServerError(c, "Error checking task status")
}

func calculateProgress(status string) int {
	switch status {
	case "uploaded":
		return 20
	case "processing":
		return 50
	case "checked":
		return 80
	case "corrected":
		return 100
	default:
		return 0
	}
}
