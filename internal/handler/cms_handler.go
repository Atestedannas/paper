package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/utils"
	"gorm.io/gorm"
)

type CmsHandler struct{}

func NewCmsHandler() *CmsHandler {
	return &CmsHandler{}
}

func (h *CmsHandler) ListPosts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "15"))
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := database.DB.Model(&model.CmsPost{}).Where("status != ?", "hidden").Count(&total).Error; err != nil {
		// 表可能不存在，尝试自动建表
		database.DB.AutoMigrate(&model.CmsPost{}, &model.CmsReply{})
		database.DB.Model(&model.CmsPost{}).Where("status != ?", "hidden").Count(&total)
	}

	var posts []model.CmsPost
	if err := database.DB.Where("status != ?", "hidden").
		Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&posts).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "查询失败", err.Error())
		return
	}

	type countRow struct {
		PostID uuid.UUID
		Cnt    int64
	}
	var counts []countRow
	postIDs := make([]uuid.UUID, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}
	if len(postIDs) > 0 {
		database.DB.Model(&model.CmsReply{}).
			Select("post_id, count(*) as cnt").
			Where("post_id IN ?", postIDs).
			Group("post_id").
			Scan(&counts)
	}
	cntMap := map[uuid.UUID]int64{}
	for _, r := range counts {
		cntMap[r.PostID] = r.Cnt
	}

	type postItem struct {
		model.CmsPost
		ReplyCount int64 `json:"reply_count"`
	}
	items := make([]postItem, len(posts))
	for i, p := range posts {
		items[i] = postItem{CmsPost: p, ReplyCount: cntMap[p.ID]}
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"page": page, "page_size": pageSize, "total": total, "items": items,
	})
}

func (h *CmsHandler) GetPost(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的帖子ID", "")
		return
	}
	var post model.CmsPost
	if err := database.DB.Preload("Replies", func(db *gorm.DB) *gorm.DB {
		return db.Order("created_at ASC")
	}).First(&post, "id = ?", id).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "帖子不存在", "")
		return
	}

	database.DB.Model(&post).UpdateColumn("view_count", post.ViewCount+1)
	post.ViewCount++

	utils.SuccessResponse(c, "获取成功", post)
}

func (h *CmsHandler) CreatePost(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*model.User)

	var req struct {
		Title   string `json:"title" binding:"required,min=2,max=200"`
		Content string `json:"content" binding:"required,min=5"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	post := model.CmsPost{
		UserID:   u.ID,
		Username: u.Username,
		Avatar:   u.Avatar,
		Title:    req.Title,
		Content:  req.Content,
	}
	if err := database.DB.Create(&post).Error; err != nil {
		// 表不存在时自动建表并重试
		database.DB.AutoMigrate(&model.CmsPost{}, &model.CmsReply{})
		if err2 := database.DB.Create(&post).Error; err2 != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "创建失败", err2.Error())
			return
		}
	}
	utils.SuccessResponse(c, "发布成功", post)
}

func (h *CmsHandler) CreateReply(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*model.User)

	postID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的帖子ID", "")
		return
	}

	var cnt int64
	database.DB.Model(&model.CmsPost{}).Where("id = ?", postID).Count(&cnt)
	if cnt == 0 {
		utils.ErrorResponse(c, http.StatusNotFound, "帖子不存在", "")
		return
	}

	var req struct {
		Content         string `json:"content" binding:"required,min=1"`
		ReplyToID       string `json:"reply_to_id"`
		ReplyToUsername string `json:"reply_to_username"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "参数错误", err.Error())
		return
	}

	reply := model.CmsReply{
		PostID:   postID,
		UserID:   u.ID,
		Username: u.Username,
		Avatar:   u.Avatar,
		Content:  req.Content,
		IsAdmin:  u.Role == "admin",
	}

	if req.ReplyToID != "" {
		if rid, err := uuid.Parse(req.ReplyToID); err == nil {
			reply.ReplyToID = &rid
			reply.ReplyToUsername = req.ReplyToUsername
		}
	}

	if err := database.DB.Create(&reply).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "回复失败", err.Error())
		return
	}
	utils.SuccessResponse(c, "回复成功", reply)
}

func (h *CmsHandler) DeletePost(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*model.User)

	postID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效ID", "")
		return
	}
	var post model.CmsPost
	if err := database.DB.First(&post, "id = ?", postID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "帖子不存在", "")
		return
	}
	if post.UserID != u.ID && u.Role != "admin" {
		utils.ErrorResponse(c, http.StatusForbidden, "无权删除", "")
		return
	}
	database.DB.Where("post_id = ?", postID).Delete(&model.CmsReply{})
	database.DB.Delete(&post)
	utils.SuccessResponse(c, "删除成功", nil)
}

func (h *CmsHandler) DeleteReply(c *gin.Context) {
	user, _ := c.Get("user")
	u := user.(*model.User)

	replyID, err := uuid.Parse(c.Param("replyId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效ID", "")
		return
	}
	var reply model.CmsReply
	if err := database.DB.First(&reply, "id = ?", replyID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "回复不存在", "")
		return
	}
	if reply.UserID != u.ID && u.Role != "admin" {
		utils.ErrorResponse(c, http.StatusForbidden, "无权删除", "")
		return
	}
	database.DB.Delete(&reply)
	utils.SuccessResponse(c, "删除成功", nil)
}
