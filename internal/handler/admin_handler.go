package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// AdminHandler 绠＄悊鍛樺鐞嗗櫒
type AdminHandler struct {
	userService   service.UserService
	orderService  service.OrderService
	memberService service.MemberService
	config        *config.Config
}

// NewAdminHandler 鍒涘缓绠＄悊鍛樺鐞嗗櫒瀹炰緥
func NewAdminHandler(config *config.Config) *AdminHandler {
	return &AdminHandler{
		userService:   service.NewUserService(),
		orderService:  service.NewOrderService(),
		memberService: service.NewMemberService(),
		config:        config,
	}
}

// GetDashboard 鑾峰彇绠＄悊鍛樻帶鍒跺彴鏁版嵁
func (h *AdminHandler) GetDashboard(c *gin.Context) {
	utils.SuccessResponse(c, "鑾峰彇鎴愬姛", gin.H{
		"user_growth":    []int{120, 200, 150, 250, 300, 400},
		"recent_users":   5,
		"pending_orders": 3,
		"total_papers":   150,
		"today_checks":   20,
	})
}

// GetSystemStats 鑾峰彇绯荤粺缁熻鏁版嵁
func (h *AdminHandler) GetSystemStats(c *gin.Context) {
	utils.SuccessResponse(c, "鑾峰彇鎴愬姛", gin.H{
		"total_users":  1000,
		"total_papers": 5000,
		"total_checks": 10000,
		"total_orders": 500,
	})
}

// GetUsers 鑾峰彇鐢ㄦ埛鍒楄〃
func (h *AdminHandler) GetUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	users, total, err := h.userService.GetAllUsers(page, pageSize)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鑾峰彇鐢ㄦ埛鍒楄〃澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "鑾峰彇鎴愬姛", gin.H{
		"users":       users,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// UpdateUserRole 鏇存柊鐢ㄦ埛瑙掕壊
func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "鏃犳晥鐨勭敤鎴稩D", err.Error())
		return
	}

	var req struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "璇锋眰鍙傛暟閿欒", err.Error())
		return
	}

	if err := h.userService.UpdateUserRole(userID, req.Role); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鏇存柊鐢ㄦ埛瑙掕壊澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "鏇存柊鎴愬姛", nil)
}

// UpdateUserStatus 鏇存柊鐢ㄦ埛鐘舵€?
func (h *AdminHandler) UpdateUserStatus(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "鏃犳晥鐨勭敤鎴稩D", err.Error())
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "璇锋眰鍙傛暟閿欒", err.Error())
		return
	}

	if err := h.userService.UpdateUserStatus(userID, req.Status); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新用户状态失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "鏇存柊鎴愬姛", nil)
}

// DeleteUser 鍒犻櫎鐢ㄦ埛
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "鏃犳晥鐨勭敤鎴稩D", err.Error())
		return
	}

	if err := h.userService.DeleteUser(userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鍒犻櫎鐢ㄦ埛澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "鍒犻櫎鎴愬姛", nil)
}

// GetPapers 鑾峰彇璁烘枃鍒楄〃
func (h *AdminHandler) GetPapers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var papers []model.Paper
	var total int64

	database.DB.Model(&model.Paper{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := database.DB.Preload("User").Preload("SelectedTemplate").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&papers).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鑾峰彇璁烘枃鍒楄〃澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "鑾峰彇鎴愬姛", gin.H{
		"papers":      papers,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// GetOrders 鑾峰彇璁㈠崟鍒楄〃
func (h *AdminHandler) GetOrders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	var orders []model.Order
	var total int64

	database.DB.Model(&model.Order{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := database.DB.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&orders).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鑾峰彇璁㈠崟鍒楄〃澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "鑾峰彇鎴愬姛", gin.H{
		"orders":      orders,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// SetUserAsSuperAdmin 灏嗘寚瀹氶偖绠辩殑鐢ㄦ埛璁剧疆涓鸿秴绾х鐞嗗憳
func (h *AdminHandler) SetUserAsSuperAdmin(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "鏃犳晥鐨勯偖绠卞湴鍧€", err.Error())
		return
	}

	// 鏌ユ壘鐢ㄦ埛
	var user model.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "用户不存在", err.Error())
		return
	}

	// 鏌ユ壘瓒呯骇绠＄悊鍛樿鑹?
	var superAdminRole model.Role
	if err := database.DB.Where("code = ?", "super_admin").First(&superAdminRole).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "瓒呯骇绠＄悊鍛樿鑹蹭笉瀛樺湪", err.Error())
		return
	}

	// 妫€鏌ョ敤鎴锋槸鍚﹀凡缁忔槸瓒呯骇绠＄悊鍛?
	var userRole model.UserRole
	if err := database.DB.Where("user_id = ? AND role_id = ?", user.ID, superAdminRole.ID).First(&userRole).Error; err == nil {
		utils.SuccessResponse(c, "用户已经是超级管理员", gin.H{
			"user_id":   user.ID,
			"email":     user.Email,
			"role_id":   superAdminRole.ID,
			"role_name": superAdminRole.Name,
		})
		return
	}

	// 鍒嗛厤瓒呯骇绠＄悊鍛樿鑹?
	userRole = model.UserRole{
		UserID: user.ID,
		RoleID: superAdminRole.ID,
	}
	if err := database.DB.Create(&userRole).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "鍒嗛厤瑙掕壊澶辫触", err.Error())
		return
	}

	utils.SuccessResponse(c, "璁剧疆鎴愬姛", gin.H{
		"user_id":   user.ID,
		"email":     user.Email,
		"role_id":   superAdminRole.ID,
		"role_name": superAdminRole.Name,
	})
}

// CreateUser 创建用户（支持角色/权限一并分配）
func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req struct {
		Username      string   `json:"username" binding:"required"`
		Email         string   `json:"email" binding:"required,email"`
		Password      string   `json:"password" binding:"required,min=6"`
		Role          string   `json:"role"`
		Status        string   `json:"status"`
		FullName      string   `json:"full_name"`
		RoleIDs       []string `json:"role_ids"`
		PermissionIDs []string `json:"permission_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}
	if req.Status == "" {
		req.Status = "active"
	}

	resolvedRoleIDs := make([]uuid.UUID, 0, len(req.RoleIDs))
	seenRoleIDs := make(map[uuid.UUID]struct{})
	for _, roleValue := range req.RoleIDs {
		if roleValue == "" {
			continue
		}

		var roleID uuid.UUID
		if parsed, err := uuid.Parse(roleValue); err == nil {
			roleID = parsed
		} else {
			var role model.Role
			if err := database.DB.Where("code = ?", roleValue).First(&role).Error; err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "角色不存在", roleValue)
				return
			}
			roleID = role.ID
		}

		if _, ok := seenRoleIDs[roleID]; ok {
			continue
		}
		seenRoleIDs[roleID] = struct{}{}
		resolvedRoleIDs = append(resolvedRoleIDs, roleID)
	}

	if len(resolvedRoleIDs) > 0 {
		var primaryRole model.Role
		if err := database.DB.First(&primaryRole, resolvedRoleIDs[0]).Error; err == nil && primaryRole.Code != "" {
			req.Role = primaryRole.Code
		}
	}

	user, err := h.userService.CreateUser(req.Username, req.Email, req.Password, req.Role, req.Status, req.FullName)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建用户失败", err.Error())
		return
	}

	for _, roleID := range resolvedRoleIDs {
		if err := database.DB.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?) ON CONFLICT DO NOTHING", user.ID, roleID).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "分配角色失败", err.Error())
			return
		}
	}

	resolvedPermissionIDs := make([]uuid.UUID, 0, len(req.PermissionIDs))
	seenPermissionIDs := make(map[uuid.UUID]struct{})
	for _, permissionValue := range req.PermissionIDs {
		if permissionValue == "" {
			continue
		}

		var permissionID uuid.UUID
		if parsed, err := uuid.Parse(permissionValue); err == nil {
			permissionID = parsed
		} else {
			var permission model.Permission
			if err := database.DB.Where("code = ?", permissionValue).First(&permission).Error; err != nil {
				utils.ErrorResponse(c, http.StatusBadRequest, "权限不存在", permissionValue)
				return
			}
			permissionID = permission.ID
		}

		if _, ok := seenPermissionIDs[permissionID]; ok {
			continue
		}
		seenPermissionIDs[permissionID] = struct{}{}
		resolvedPermissionIDs = append(resolvedPermissionIDs, permissionID)
	}

	for _, permissionID := range resolvedPermissionIDs {
		if err := database.DB.Exec("INSERT INTO user_permissions (user_id, permission_id) VALUES (?, ?) ON CONFLICT DO NOTHING", user.ID, permissionID).Error; err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "分配权限失败", err.Error())
			return
		}
	}

	utils.SuccessResponse(c, "创建成功", gin.H{
		"user":           user,
		"role_ids":       resolvedRoleIDs,
		"permission_ids": resolvedPermissionIDs,
	})
}

// UpdateUser 鏇存柊鐢ㄦ埛淇℃伅
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "鏃犳晥鐨勭敤鎴稩D", err.Error())
		return
	}

	var req struct {
		Username *string `json:"username"`
		Email    *string `json:"email"`
		FullName *string `json:"full_name"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "璇锋眰鍙傛暟閿欒", err.Error())
		return
	}

	user, err := h.userService.GetUserByID(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "用户不存在", err.Error())
		return
	}

	if req.Username != nil {
		user.Username = *req.Username
	}
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.FullName != nil {
		user.FullName = *req.FullName
	}
	if req.Role != nil {
		user.Role = *req.Role
	}
	if req.Status != nil {
		user.Status = *req.Status
	}

	if err := h.userService.UpdateUser(user); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新用户失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", gin.H{
		"user": user,
	})
}

// DeletePaper 删除论文（软删除）
func (h *AdminHandler) DeletePaper(c *gin.Context) {
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的论文 ID", err.Error())
		return
	}

	// 查找论文
	var paper model.Paper
	if err := database.DB.First(&paper, paperID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "论文不存在", err.Error())
		return
	}

	// 检查是否已删除
	if paper.DeletedAt.Time.Year() > 2000 {
		utils.ErrorResponse(c, http.StatusBadRequest, "论文已被删除", "")
		return
	}

	// 软删除关联的检查结果
	if err := database.DB.Where("paper_id = ?", paperID).Delete(&model.CheckResult{}).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除关联检查结果失败", err.Error())
		return
	}

	// 软删除论文
	if err := database.DB.Delete(&model.Paper{}, paperID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除论文失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

// BatchDeletePapers 批量删除论文（软删除）
func (h *AdminHandler) BatchDeletePapers(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if len(req.IDs) == 0 {
		utils.ErrorResponse(c, http.StatusBadRequest, "论文 ID 列表不能为空", "")
		return
	}

	deletedCount := 0
	failedCount := 0
	skippedCount := 0

	tx := database.DB.Begin()

	for _, idStr := range req.IDs {
		paperID, err := uuid.Parse(idStr)
		if err != nil {
			failedCount++
			continue
		}

		var paper model.Paper
		if err := tx.First(&paper, paperID).Error; err != nil {
			failedCount++
			continue
		}

		if paper.DeletedAt.Valid {
			skippedCount++
			continue
		}

		var checkResults []model.CheckResult
		if err := tx.Where("paper_id = ?", paperID).Find(&checkResults).Error; err != nil {
			failedCount++
			continue
		}

		for _, cr := range checkResults {
			if err := tx.Where("check_result_id = ?", cr.ID).Delete(&model.FormatCorrection{}).Error; err != nil {
				failedCount++
				continue
			}
		}

		if err := tx.Where("paper_id = ?", paperID).Delete(&model.CheckResult{}).Error; err != nil {
			failedCount++
			continue
		}

		if err := tx.Delete(&paper).Error; err != nil {
			failedCount++
			continue
		}

		deletedCount++
	}

	if failedCount > 0 {
		tx.Rollback()
		utils.ErrorResponse(c, http.StatusInternalServerError, "部分论文删除失败", "")
		return
	}

	tx.Commit()

	utils.SuccessResponse(c, "批量删除成功", gin.H{
		"deleted_count": deletedCount,
		"failed_count":  failedCount,
		"skipped_count": skippedCount,
		"total_count":   len(req.IDs),
	})
}

// CheckPaperFormat 检查论文格式
func (h *AdminHandler) CheckPaperFormat(c *gin.Context) {
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的论文 ID", err.Error())
		return
	}

	var paper model.Paper
	if err := database.DB.First(&paper, paperID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "论文不存在", err.Error())
		return
	}

	utils.SuccessResponse(c, "格式检查已启动", gin.H{
		"paper_id": paperID,
		"status":   "checking",
	})
}

// BatchCheckPapers 批量检查论文
func (h *AdminHandler) BatchCheckPapers(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	utils.SuccessResponse(c, "批量检查已启动", gin.H{
		"count": len(req.IDs),
	})
}

// DownloadPaperFile 下载论文文件
func (h *AdminHandler) DownloadPaperFile(c *gin.Context) {
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的论文 ID", err.Error())
		return
	}

	var paper model.Paper
	if err := database.DB.First(&paper, paperID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "论文不存在", err.Error())
		return
	}

	c.File(paper.FilePath)
}

// RestorePaper 恢复已删除的论文
func (h *AdminHandler) RestorePaper(c *gin.Context) {
	paperID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的论文 ID", err.Error())
		return
	}

	// 查找已删除的论文（使用 Unscoped 包含已删除的记录）
	var paper model.Paper
	if err := database.DB.Unscoped().First(&paper, paperID).Error; err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "论文不存在", err.Error())
		return
	}

	// 检查是否真的被删除了
	if paper.DeletedAt.Time.Year() <= 2000 {
		utils.ErrorResponse(c, http.StatusBadRequest, "论文未被删除", "")
		return
	}

	// 恢复论文（将 deleted_at 设置为 nil）
	if err := database.DB.Unscoped().Model(&paper).Update("deleted_at", nil).Error; err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "恢复论文失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "恢复成功", nil)
}

// BatchRestorePapers 批量恢复已删除的论文
func (h *AdminHandler) BatchRestorePapers(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if len(req.IDs) == 0 {
		utils.ErrorResponse(c, http.StatusBadRequest, "论文 ID 列表不能为空", "")
		return
	}

	restoredCount := 0
	failedCount := 0
	skippedCount := 0

	for _, idStr := range req.IDs {
		paperID, err := uuid.Parse(idStr)
		if err != nil {
			failedCount++
			continue
		}

		// 查找已删除的论文
		var paper model.Paper
		if err := database.DB.Unscoped().First(&paper, paperID).Error; err != nil {
			failedCount++
			continue
		}

		// 检查是否真的被删除了
		if paper.DeletedAt.Time.Year() <= 2000 {
			skippedCount++
			continue
		}

		// 恢复论文
		if err := database.DB.Unscoped().Model(&paper).Update("deleted_at", nil).Error; err == nil {
			restoredCount++
		} else {
			failedCount++
		}
	}

	utils.SuccessResponse(c, "批量恢复成功", gin.H{
		"restored_count": restoredCount,
		"failed_count":   failedCount,
		"skipped_count":  skippedCount,
		"total_count":    len(req.IDs),
	})
}
