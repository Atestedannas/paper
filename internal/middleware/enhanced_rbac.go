package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type EnhancedRBACMiddleware struct {
	rbacService     service.EnhancedRBACService
	dataPermService service.DataPermissionService
}

func NewEnhancedRBACMiddleware() *EnhancedRBACMiddleware {
	rbacService, _ := service.NewEnhancedRBACService()
	dataPermService := service.NewDataPermissionService()

	return &EnhancedRBACMiddleware{
		rbacService:     rbacService,
		dataPermService: dataPermService,
	}
}

func (m *EnhancedRBACMiddleware) RequirePermissionWithDataFilter(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		uid := userID.(uuid.UUID)

		hasPermission, err := m.rbacService.HasPermission(uid, resource, action)
		if err != nil {
			utils.ErrorResponse(c, 500, "权限检查失败", err.Error())
			c.Abort()
			return
		}

		if !hasPermission {
			utils.ErrorResponse(c, 403, "权限不足", "您没有执行此操作的权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

func (m *EnhancedRBACMiddleware) DataPermissionFilter(resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}

		uid := userID.(uuid.UUID)

		filter, err := m.dataPermService.GetUserDataFilter(uid, resourceType)
		if err != nil {
			utils.ErrorResponse(c, 500, "获取数据权限失败", err.Error())
			c.Abort()
			return
		}

		if filter.WhereClause != "" {
			c.Set("data_filter", filter)
		}

		c.Next()
	}
}

func (m *EnhancedRBACMiddleware) ApplyDataFilterToQuery(resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}

		uid := userID.(uuid.UUID)

		filter, err := m.dataPermService.GetUserDataFilter(uid, resourceType)
		if err != nil || filter.WhereClause == "" {
			c.Next()
			return
		}

		filterClause := filter.WhereClause
		params := make([]interface{}, 0, len(filter.Parameters))
		for _, v := range filter.Parameters {
			params = append(params, v)
		}

		c.Set("data_filter_clause", filterClause)
		c.Set("data_filter_params", params)
		c.Set("data_filter_applied", true)

		c.Next()
	}
}

func (m *EnhancedRBACMiddleware) GetDataFilterFromContext(c *gin.Context) (string, []interface{}, bool) {
	filterClause, exists := c.Get("data_filter_clause")
	if !exists {
		return "", nil, false
	}

	params, exists := c.Get("data_filter_params")
	if !exists {
		return "", nil, false
	}

	return filterClause.(string), params.([]interface{}), true
}

func (m *EnhancedRBACMiddleware) AutoCheckAPIPermission() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		uid := userID.(uuid.UUID)

		path := c.Request.URL.Path
		method := c.Request.Method

		permission := generatePermissionCode(path, method)

		parts := strings.Split(permission, ":")
		if len(parts) < 2 {
			c.Next()
			return
		}

		resource := parts[0]
		action := parts[1]

		if action == "list" {
			action = "read"
		}

		hasPermission, err := m.rbacService.HasPermission(uid, resource, action)
		if err != nil {
			utils.ErrorResponse(c, 500, "权限检查失败", err.Error())
			c.Abort()
			return
		}

		if !hasPermission {
			utils.ErrorResponse(c, 403, "权限不足", "您没有访问此API的权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

func (m *EnhancedRBACMiddleware) RequireRoleWithInheritance(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		uid := userID.(uuid.UUID)

		userRoles, err := m.rbacService.GetUserRoles(uid)
		if err != nil {
			utils.ErrorResponse(c, 500, "获取用户角色失败", err.Error())
			c.Abort()
			return
		}

		hasRequiredRole := false
		for _, userRole := range userRoles {
			for _, requiredRole := range roles {
				if userRole.Code == requiredRole {
					hasRequiredRole = true
					break
				}
			}
			if hasRequiredRole {
				break
			}

			roleIDs, err := m.rbacService.GetRoleHierarchy(userRole.ID)
			if err == nil {
				for _, roleID := range roleIDs {
					for _, requiredRole := range roles {
						role, _ := m.rbacService.GetRoleByID(roleID)
						if role != nil && role.Code == requiredRole {
							hasRequiredRole = true
							break
						}
					}
					if hasRequiredRole {
						break
					}
				}
			}

			if hasRequiredRole {
				break
			}
		}

		if !hasRequiredRole {
			utils.ErrorResponse(c, 403, "角色不足", "您没有执行此操作的角色权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

func generatePermissionCode(path, method string) string {
	path = strings.TrimPrefix(path, "/api/v1/")
	path = strings.TrimPrefix(path, "/api/v2/")

	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return "unknown:unknown"
	}

	resource := parts[0]

	if len(parts) > 1 {
		if strings.HasPrefix(parts[1], "v") && len(parts[1]) > 1 && parts[1][1] >= '0' && parts[1][1] <= '9' {
			if len(parts) > 2 {
				resource = parts[1]
			}
		}
	}

	action := parseActionFromMethod(method)

	if len(parts) > 2 && parts[2] != "" {
		detailID := parts[2]
		if detailID == "" || strings.HasPrefix(detailID, "{") {
			if action == "read" {
				action = "detail"
			}
		}
	}

	return resource + ":" + action
}

func parseActionFromMethod(method string) string {
	switch method {
	case "GET":
		if strings.Contains(method, "LIST") {
			return "list"
		}
		return "read"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "unknown"
	}
}
