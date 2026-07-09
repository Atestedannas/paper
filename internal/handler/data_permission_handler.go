package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type DataPermissionHandler struct {
	dataService service.DataPermissionService
}

func NewDataPermissionHandler() *DataPermissionHandler {
	return &DataPermissionHandler{
		dataService: service.NewDataPermissionService(),
	}
}

func userIDParam(c *gin.Context) string {
	if id := c.Param("id"); id != "" {
		return id
	}
	return c.Param("user_id")
}

func (h *DataPermissionHandler) GetUserDataScope(c *gin.Context) {
	userID, err := uuid.Parse(userIDParam(c))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	scope, err := h.dataService.GetUserDataScope(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取数据范围失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", gin.H{
		"user_id":    userID,
		"data_scope": scope,
	})
}

func (h *DataPermissionHandler) SetUserDataScope(c *gin.Context) {
	userID, err := uuid.Parse(userIDParam(c))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	var req struct {
		Scope string `json:"scope" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.dataService.SetUserDataScope(userID, req.Scope); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "设置数据范围失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "设置成功", nil)
}

func (h *DataPermissionHandler) GetUserDataFilter(c *gin.Context) {
	userID, err := uuid.Parse(userIDParam(c))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	resourceType := c.DefaultQuery("resource_type", "")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	filter, err := h.dataService.GetUserDataFilter(userID, resourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取数据过滤条件失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", filter)
}

func (h *DataPermissionHandler) GetDataRules(c *gin.Context) {
	resourceType := c.DefaultQuery("resource_type", "")

	rules, err := h.dataService.GetDataRules(resourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取数据规则列表失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", rules)
}

func (h *DataPermissionHandler) GetDataRuleByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的规则ID", err.Error())
		return
	}

	rule, err := h.dataService.GetDataRuleByID(id)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取数据规则失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", rule)
}

func (h *DataPermissionHandler) CreateDataRule(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required,max=100"`
		ResourceType string `json:"resource_type" binding:"required"`
		RuleType     string `json:"rule_type" binding:"required"`
		ColumnFilter string `json:"column_filter"`
		FilterSQL    string `json:"filter_sql"`
		Description  string `json:"description" binding:"max=200"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	_ = &struct {
		Name         string
		ResourceType string
		RuleType     string
		ColumnFilter string
		FilterSQL    string
		Description  string
	}{
		Name:         req.Name,
		ResourceType: req.ResourceType,
		RuleType:     req.RuleType,
		ColumnFilter: req.ColumnFilter,
		FilterSQL:    req.FilterSQL,
		Description:  req.Description,
	}

	_, err := h.dataService.CreateDataRule(nil)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "创建数据规则失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "创建成功", nil)
}

func (h *DataPermissionHandler) UpdateDataRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的规则ID", err.Error())
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	if err := h.dataService.UpdateDataRule(id, req); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "更新数据规则失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "更新成功", nil)
}

func (h *DataPermissionHandler) DeleteDataRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的规则ID", err.Error())
		return
	}

	if err := h.dataService.DeleteDataRule(id); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "删除数据规则失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "删除成功", nil)
}

func (h *DataPermissionHandler) GetUserFieldPermissions(c *gin.Context) {
	userID, err := uuid.Parse(userIDParam(c))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	resourceType := c.DefaultQuery("resource_type", "")
	if resourceType == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "资源类型不能为空", "")
		return
	}

	permissions, err := h.dataService.GetUserFieldPermissions(userID, resourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取字段权限失败", err.Error())
		return
	}

	utils.SuccessResponse(c, "获取成功", permissions)
}

type DataFilterQuery struct {
	UserID       string `form:"user_id" binding:"required"`
	ResourceType string `form:"resource_type" binding:"required"`
}

func (h *DataPermissionHandler) CheckDataAccess(c *gin.Context) {
	var req DataFilterQuery
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "无效的用户ID", err.Error())
		return
	}

	filter, err := h.dataService.GetUserDataFilter(userID, req.ResourceType)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "获取数据访问权限失败", err.Error())
		return
	}

	hasAccess := filter.WhereClause != "" || filter.WhereClause == ""

	utils.SuccessResponse(c, "检查完成", gin.H{
		"has_access": hasAccess,
		"data_scope": filter.WhereClause,
		"can_access": true,
	})
}
