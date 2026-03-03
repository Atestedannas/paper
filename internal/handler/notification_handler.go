package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/utils"
)

type NotificationHandler struct{}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{}
}

// SendEmailNotification 发送邮件通知
func (h *NotificationHandler) SendEmailNotification(c *gin.Context) {
	var req struct {
		To      string `json:"to" binding:"required,email"`
		Subject string `json:"subject" binding:"required"`
		Body    string `json:"body" binding:"required"`
		Type    string `json:"type"` // report, download, etc.
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// TODO: 集成真实的邮件发送服务 (e.g. SMTP, SendGrid)
	// 目前仅记录日志
	// log.Printf("Sending email to %s: %s", req.To, req.Subject)

	utils.Success(c, gin.H{"message": "Email notification queued"})
}
