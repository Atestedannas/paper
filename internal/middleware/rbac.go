package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// RBACMiddleware RBAC权限验证中间件
type RBACMiddleware struct {
	rbacService service.RBACService
}

// NewRBACMiddleware 创建RBAC中间件实例
func NewRBACMiddleware() *RBACMiddleware {
	rbacService, err := service.NewRBACService()
	if err != nil {
		panic(err)
	}
	return &RBACMiddleware{
		rbacService: rbacService,
	}
}

// RequirePermission 需要特定权限的中间件
func (m *RBACMiddleware) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 检查用户权限
		hasPermission, err := m.rbacService.HasPermission(userID.(uuid.UUID), resource, action)
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

// RequireRole 需要特定角色的中间件
func (m *RBACMiddleware) RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 获取用户角色
		userRoles, err := m.rbacService.GetUserRoles(userID.(uuid.UUID))
		if err != nil {
			utils.ErrorResponse(c, 500, "获取用户角色失败", err.Error())
			c.Abort()
			return
		}

		// 检查用户是否拥有指定角色
		hasRole := false
		for _, userRole := range userRoles {
			for _, requiredRole := range roles {
				if userRole.Code == requiredRole {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			utils.ErrorResponse(c, 403, "角色不足", "您没有执行此操作的角色权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAnyPermission 需要任意一个权限的中间件
func (m *RBACMiddleware) RequireAnyPermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 获取用户所有权限
		userPermissions, err := m.rbacService.GetUserPermissions(userID.(uuid.UUID))
		if err != nil {
			utils.ErrorResponse(c, 500, "获取用户权限失败", err.Error())
			c.Abort()
			return
		}

		// 检查用户是否拥有任意一个所需权限
		hasPermission := false
		userPermMap := make(map[string]bool)
		for _, perm := range userPermissions {
			userPermMap[perm.Code] = true
		}

		for _, requiredPerm := range permissions {
			if userPermMap[requiredPerm] {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			utils.ErrorResponse(c, 403, "权限不足", "您没有执行此操作的权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireAllPermissions 需要所有权限的中间件
func (m *RBACMiddleware) RequireAllPermissions(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 获取用户所有权限
		userPermissions, err := m.rbacService.GetUserPermissions(userID.(uuid.UUID))
		if err != nil {
			utils.ErrorResponse(c, 500, "获取用户权限失败", err.Error())
			c.Abort()
			return
		}

		// 检查用户是否拥有所有所需权限
		userPermMap := make(map[string]bool)
		for _, perm := range userPermissions {
			userPermMap[perm.Code] = true
		}

		for _, requiredPerm := range permissions {
			if !userPermMap[requiredPerm] {
				utils.ErrorResponse(c, 403, "权限不足", "您缺少执行此操作所需的权限: "+requiredPerm)
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// DataPermissionMiddleware 数据权限中间件
func (m *RBACMiddleware) DataPermissionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 这里可以实现数据权限控制逻辑
		// 例如：根据用户角色控制其能访问的数据范围
		// 1. 部门数据隔离
		// 2. 个人数据访问控制
		// 3. 数据字段权限控制

		// 获取用户角色以确定数据访问范围
		userRoles, err := m.rbacService.GetUserRoles(userID.(uuid.UUID))
		if err != nil {
			utils.ErrorResponse(c, 500, "获取用户角色失败", err.Error())
			c.Abort()
			return
		}

		// 将用户角色信息存入上下文，供后续处理器使用
		c.Set("user_roles", userRoles)

		c.Next()
	}
}

// AdminOnly 仅管理员可访问的中间件
func (m *RBACMiddleware) AdminOnly() gin.HandlerFunc {
	return m.RequireRole("admin", "system_admin")
}

// CheckResourceOwnership 检查资源所有权中间件
func (m *RBACMiddleware) CheckResourceOwnership(resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 获取资源ID（从路径参数或其他地方）
		resourceID := c.Param("id")
		if resourceID == "" {
			// 尝试从其他地方获取资源ID
			resourceID = c.Query("id")
		}

		if resourceID == "" {
			// 如果无法获取资源ID，允许通过（可能在列表接口中）
			c.Next()
			return
		}

		// 这里可以实现资源所有权检查逻辑
		// 例如：检查用户是否是资源的创建者
		// 具体实现取决于资源类型和业务逻辑

		// 检查用户是否拥有绕过所有权检查的权限
		hasBypassPermission, err := m.rbacService.HasPermission(userID.(uuid.UUID), resourceType+":ownership_bypass", "read")
		if err != nil {
			utils.ErrorResponse(c, 500, "权限检查失败", err.Error())
			c.Abort()
			return
		}

		if hasBypassPermission {
			c.Next()
			return
		}

		// 如果没有绕过权限，则需要检查资源所有权
		// 这里需要根据具体的资源类型和业务逻辑来实现

		c.Next()
	}
}

// PathBasedPermission 路径基础权限检查中间件
func (m *RBACMiddleware) PathBasedPermission() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户ID
		userID, exists := c.Get("user_id")
		if !exists {
			utils.ErrorResponse(c, 401, "未认证", "用户未登录")
			c.Abort()
			return
		}

		// 获取请求路径和方法
		path := c.Request.URL.Path
		method := c.Request.Method

		// 将路径和方法转换为权限资源和动作
		// 例如：GET /api/v1/users -> resource: "user", action: "read"
		// 这里需要根据实际的路径模式进行解析

		// 简单的路径解析示例
		resource := parseResourceFromPath(path)
		action := parseActionFromMethod(method)

		// 检查权限
		hasPermission, err := m.rbacService.HasPermission(userID.(uuid.UUID), resource, action)
		if err != nil {
			utils.ErrorResponse(c, 500, "权限检查失败", err.Error())
			c.Abort()
			return
		}

		if !hasPermission {
			utils.ErrorResponse(c, 403, "权限不足", "您没有访问此资源的权限")
			c.Abort()
			return
		}

		c.Next()
	}
}

// 辅助函数：从路径解析资源
func parseResourceFromPath(path string) string {
	// 简单的路径解析逻辑
	// 例如：/api/v1/users -> user
	// /api/v1/orders -> order
	// /api/v1/papers -> paper

	// 移除前缀
	path = strings.TrimPrefix(path, "/api/v1/")
	path = strings.TrimPrefix(path, "/api/v2/")

	// 获取第一个路径段作为资源名
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		resource := parts[0]
		// 移除版本号等前缀
		if strings.HasPrefix(resource, "v") && len(resource) > 1 && resource[1] >= '0' && resource[1] <= '9' {
			if len(parts) > 1 {
				resource = parts[1]
			}
		}
		return resource
	}
	return "unknown"
}
