package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/handler"
	"github.com/paper-format-checker/backend/internal/middleware"
)

func main() {
	// 加载配置
	configPath := ".env"
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 检查端口是否被占用，如果被占用则尝试关闭占用进程
	if isPortInUse(cfg.Server.Port) {
		killProcessUsingPort(cfg.Server.Port)
	}

	// 初始化数据库（演示环境下允许失败）
	database.InitDatabase(cfg)

	// 修复 orders 表 member_level_id 字段的外键约束
	if err := database.ResetOrdersMemberLevel(); err != nil {
		log.Printf("Warning: Failed to reset member_level_id: %v", err)
	} else {
		log.Println("Successfully reset member_level_id constraint")
	}

	// 执行数据库迁移和初始化（在启动时显式调用）
	//if err := database.PerformMigration(); err != nil {
	//	log.Printf("Warning: Failed to perform database migration: %v", err)
	//}

	// 创建Gin路由
	router := gin.Default()

	// 配置CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// 添加API限流中间件
	router.Use(middleware.RateLimitMiddleware())

	// 初始化处理器
	authHandler := handler.NewAuthHandler(cfg)
	memberHandler := handler.NewMemberHandler()
	orderHandler := handler.NewOrderHandler()
	paymentHandler := handler.NewPaymentHandler(cfg)
	paperHandler := handler.NewPaperHandler(cfg)
	contactHandler := handler.NewContactHandler()
	adminHandler := handler.NewAdminHandler(cfg)
	adminSystemHandler := handler.NewAdminSystemHandler()
	universityHandler := handler.NewUniversityHandler()
	adminUniversityHandler := handler.NewAdminUniversityHandler()
	rbacHandler := handler.NewRBACHandler()
	configHandler := handler.NewConfigHandler()
	billingHandler := handler.NewBillingHandler()

	// 新增处理器
	adminOrderHandler := handler.NewAdminOrderHandler()
	paperHistoryHandler := handler.NewPaperHistoryHandler()
	batchHandler := handler.NewBatchHandler()
	paymentCheckHandler := handler.NewPaymentCheckHandler()
	sandboxHandler := handler.NewSandboxHandler(cfg)

	// API路由 - 保留原有路由结构，同时添加v1版本支持
	api := router.Group("/api")
	{
		// 公开路由（不需要认证）
		api.GET("/config/public/paper-check", configHandler.GetPaperCheckConfigPublic) // 公开获取论文格式检查配置
		api.GET("/config/public/contact", configHandler.GetContactInfo)                // 公开获取联系信息
		api.GET("/config/public/billing", billingHandler.GetServiceConfig)             // 公开获取计费配置
		api.GET("/config/public/billing/check", billingHandler.CheckServicePricing)    // 公开检查服务定价

		// 认证路由
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken) // 添加刷新令牌路由
			auth.GET("/profile", middleware.AuthMiddleware(cfg), authHandler.GetProfile)
			auth.PUT("/profile", middleware.AuthMiddleware(cfg), authHandler.UpdateProfile)
			auth.PUT("/password", middleware.AuthMiddleware(cfg), authHandler.ChangePassword)

			// 微信登录
			auth.GET("/wechat/login-url", authHandler.GetWechatAuthURL)
			auth.GET("/wechat/callback", authHandler.WechatAuthCallback)

			// 支付宝登录
			auth.GET("/alipay/login-url", authHandler.GetAlipayAuthURL)
			auth.POST("/alipay/callback", authHandler.AlipayAuthCallback)

			// 文件下载
			auth.GET("/papers/:id/file", middleware.AuthMiddleware(cfg), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			auth.GET("/papers/:id/corrected-file", middleware.AuthMiddleware(cfg), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)

			// 付费检查接口
			auth.GET("/payment/check-paper", middleware.AuthMiddleware(cfg), paymentCheckHandler.CheckPaperPaymentStatus)     // 检查论文服务付费状态
			auth.GET("/payment/check-service", middleware.AuthMiddleware(cfg), paymentCheckHandler.CheckServicePaymentStatus) // 检查通用服务付费状态
			auth.GET("/payment/free-checks", middleware.AuthMiddleware(cfg), paymentCheckHandler.GetUserFreeChecks)           // 获取用户剩余免费检查次数

			// 系统配置
			auth.GET("/config/system", middleware.AuthMiddleware(cfg), configHandler.GetSystemConfig)          // 获取系统配置
			auth.GET("/config/paper-check", middleware.AuthMiddleware(cfg), configHandler.GetPaperCheckConfig) // 获取论文格式检查配置
		}

		// 会员路由
		member := api.Group("/member", middleware.AuthMiddleware(cfg))
		{
			member.GET("/levels", memberHandler.GetAllMemberLevels)
			member.GET("/levels/:id", memberHandler.GetMemberLevelByID)
			member.GET("/info", memberHandler.GetMemberInfo)
			member.GET("/status", memberHandler.CheckMemberStatus)
			member.GET("/remaining-checks", memberHandler.GetMemberRemainingChecks)

			// 管理员路由
			admin := member.Group("/admin", middleware.AdminMiddleware())
			{
				admin.POST("/levels", memberHandler.CreateMemberLevel)
				admin.PUT("/levels/:id", memberHandler.UpdateMemberLevel)
			}
		}

		// 订单路由
		order := api.Group("/order", middleware.AuthMiddleware(cfg))
		{
			order.POST("", orderHandler.CreateOrder)
			order.GET("/:id", orderHandler.GetOrderByID)
			order.GET("", orderHandler.GetOrdersByUserID)
			order.PUT("/:id/cancel", orderHandler.CancelOrder)
			order.GET("/statistics", orderHandler.GetOrderStatistics)

			// 支付回调路由 (不需要认证)
			order.PUT("/:id/status", orderHandler.UpdateOrderStatus)
		}

		// 支付路由
		payment := api.Group("/payment", middleware.AuthMiddleware(cfg))
		{
			payment.POST("", paymentHandler.CreatePayment)
			payment.POST("/wechat", paymentHandler.GenerateWechatPayment)
			payment.POST("/alipay", paymentHandler.GenerateAlipayPayment)
			payment.GET("/:id", paymentHandler.GetPaymentByID)

			// 支付回调路由 (不需要认证)
			payment.POST("/wechat/callback", paymentHandler.HandleWechatCallback)
			payment.POST("/alipay/callback", paymentHandler.HandleAlipayCallback)
		}

		// 沙箱测试路由 (管理员专用)
		sandbox := api.Group("/sandbox", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			sandbox.GET("/status", sandboxHandler.GetSandboxStatus)
			sandbox.GET("/config", sandboxHandler.GetSandboxConfig)
			sandbox.GET("/guide", sandboxHandler.GetSandboxTestGuide)
			sandbox.POST("/payment", sandboxHandler.CreateSandboxPayment)
			sandbox.POST("/query", sandboxHandler.QuerySandboxPayment)
			sandbox.POST("/simulate/wechat", sandboxHandler.SimulateWechatPayment)
			sandbox.POST("/simulate/alipay", sandboxHandler.SimulateAlipayPayment)
			sandbox.POST("/refund", sandboxHandler.CreateSandboxRefund)
		}

		// 论文路由
		paper := api.Group("/paper", middleware.AuthMiddleware(cfg))
		{
			paper.POST("/parse-format-requirements", paperHandler.ParseFormatRequirements)

			// 重庆工程学院格式处理
			paper.POST("/cqcec-format", paperHandler.HandleCQCECFormat)

			// 格式标准相关
			paper.GET("/standards", paperHandler.GetFormatStandards)

			// 上传并解析模板文件
			paper.POST("/upload-template", paperHandler.UploadTemplate)

			// 论文上传和管理
			paper.POST("/upload", paperHandler.UploadPaper)

			paper.GET("", paperHandler.GetPapers)
			paper.GET("/:id", paperHandler.GetPaper)
			paper.DELETE("/:id", paperHandler.DeletePaper)

			// 格式检查和修复
			paper.POST("/:id/check-format", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatCheck), paperHandler.CheckFormat)
			paper.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			paper.POST("/:id/apply-corrections", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatFix), paperHandler.FixFormat)

			// 格式对比功能
			paper.GET("/:id/compare/:check_result_id", paperHandler.ComparePaperFormats)

			// 修正后文件下载
			paper.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)

		}

		// 兼容前端复数形式的论文路由
		papers := api.Group("/papers", middleware.ConditionalAuthMiddleware(cfg, middleware.ServiceFormatCheck))
		{
			papers.GET("", paperHandler.GetPapers)
			papers.GET("/:id", paperHandler.GetPaper)
			papers.DELETE("/:id", paperHandler.DeletePaper)
			papers.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			papers.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
		}

		// 联系表单路由
		contact := api.Group("/contact")
		{
			contact.POST("/messages", contactHandler.CreateContactMessage) // 创建联系消息（不需要认证）
			// 以下路由添加管理员认证中间件
			adminContact := contact.Group("", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
			{
				adminContact.GET("/messages", contactHandler.GetContactMessages)          // 获取所有联系消息
				adminContact.GET("/messages/:id", contactHandler.GetContactMessageByID)   // 获取单个联系消息
				adminContact.PUT("/messages/:id", contactHandler.UpdateContactMessage)    // 更新联系消息
				adminContact.DELETE("/messages/:id", contactHandler.DeleteContactMessage) // 删除联系消息
			}
		}

		// 管理员路由
		admin := api.Group("/admin", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			// 控制台
			admin.GET("/dashboard", adminHandler.GetDashboard) // 获取管理员控制台数据
			admin.GET("/stats", adminHandler.GetSystemStats)   // 获取系统统计数据

			// 用户管理
			admin.GET("/users", adminHandler.GetUsers)                              // 获取用户列表
			admin.GET("/users/:id/roles", rbacHandler.GetUserRolesByID)             // 获取用户角色列表
			admin.GET("/users/:id/permissions", rbacHandler.GetUserPermissionsByID) // 获取用户权限列表
			admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)               // 更新用户角色
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)           // 更新用户状态
			admin.DELETE("/users/:id", adminHandler.DeleteUser)                     // 删除用户
			admin.POST("/users/set-super-admin", adminHandler.SetUserAsSuperAdmin)  // 设置用户为超级管理员

			// 论文管理
			admin.GET("/papers", adminHandler.GetPapers) // 获取论文列表

			// RBAC权限管理
			admin.GET("/roles", rbacHandler.GetRoles)                                                             // 获取角色列表
			admin.POST("/roles", rbacHandler.CreateRole)                                                          // 创建角色
			admin.GET("/roles/:id", rbacHandler.GetRoleByID)                                                      // 获取角色详情
			admin.PUT("/roles/:id", rbacHandler.UpdateRole)                                                       // 更新角色
			admin.DELETE("/roles/:id", rbacHandler.DeleteRole)                                                    // 删除角色
			admin.GET("/permissions", rbacHandler.GetPermissions)                                                 // 获取权限列表
			admin.POST("/permissions", rbacHandler.CreatePermission)                                              // 创建权限
			admin.GET("/permissions/:id", rbacHandler.GetPermissionByID)                                          // 获取权限详情
			admin.PUT("/permissions/:id", rbacHandler.UpdatePermission)                                           // 更新权限
			admin.DELETE("/permissions/:id", rbacHandler.DeletePermission)                                        // 删除权限
			admin.PUT("/user-role-assign/:user_id/:role_id", rbacHandler.AssignRoleToUser)                        // 为用户分配角色
			admin.DELETE("/user-role-assign/:user_id/:role_id", rbacHandler.RemoveRoleFromUser)                   // 从用户移除角色
			admin.PUT("/role-permission-assign/:role_id/:permission_id", rbacHandler.AssignPermissionToRole)      // 为角色分配权限
			admin.DELETE("/role-permission-assign/:role_id/:permission_id", rbacHandler.RemovePermissionFromRole) // 从角色移除权限

		}
	}

	// 添加v1版本API路由 - 与原有路由完全相同，便于未来扩展
	apiV1 := router.Group("/api/v1")
	{
		// 公开路由（不需要认证）
		apiV1.GET("/config/public/paper-check", configHandler.GetPaperCheckConfigPublic) // 公开获取论文格式检查配置
		apiV1.GET("/config/public/contact", configHandler.GetContactInfo)                // 公开获取联系信息

		// 认证路由
		auth := apiV1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.GET("/profile", middleware.AuthMiddleware(cfg), authHandler.GetProfile)
			auth.PUT("/profile", middleware.AuthMiddleware(cfg), authHandler.UpdateProfile)
			auth.PUT("/password", middleware.AuthMiddleware(cfg), authHandler.ChangePassword)

			// 微信登录
			auth.GET("/wechat/login-url", authHandler.GetWechatAuthURL)
			auth.GET("/wechat/callback", authHandler.WechatAuthCallback)

			// 支付宝登录
			auth.GET("/alipay/login-url", authHandler.GetAlipayAuthURL)
			auth.POST("/alipay/callback", authHandler.AlipayAuthCallback)

			// 文件下载
			auth.GET("/papers/:id/file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			auth.GET("/papers/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)

			// 系统配置
			auth.GET("/config/system", configHandler.GetSystemConfig)          // 获取系统配置
			auth.GET("/config/paper-check", configHandler.GetPaperCheckConfig) // 获取论文格式检查配置
		}

		// 会员路由
		member := apiV1.Group("/member", middleware.AuthMiddleware(cfg))
		{
			member.GET("/levels", memberHandler.GetAllMemberLevels)
			member.GET("/levels/:id", memberHandler.GetMemberLevelByID)
			member.GET("/info", memberHandler.GetMemberInfo)
			member.GET("/status", memberHandler.CheckMemberStatus)
			member.GET("/remaining-checks", memberHandler.GetMemberRemainingChecks)

			// 管理员路由
			admin := member.Group("/admin", middleware.AdminMiddleware())
			{
				admin.POST("/levels", memberHandler.CreateMemberLevel)
				admin.PUT("/levels/:id", memberHandler.UpdateMemberLevel)
			}
		}

		// 订单路由
		order := apiV1.Group("/order", middleware.AuthMiddleware(cfg))
		{
			order.POST("", orderHandler.CreateOrder)
			order.GET("/:id", orderHandler.GetOrderByID)
			order.GET("", orderHandler.GetOrdersByUserID)
			order.PUT("/:id/cancel", orderHandler.CancelOrder)
			order.GET("/statistics", orderHandler.GetOrderStatistics)
			order.PUT("/:id/status", orderHandler.UpdateOrderStatus)
		}

		// 管理员订单路由
		adminOrder := apiV1.Group("/admin/orders", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			adminOrder.GET("", adminOrderHandler.GetOrders)
			adminOrder.GET("/statistics", adminOrderHandler.GetOrderStatisticsForAdmin)
			adminOrder.GET("/:id", adminOrderHandler.GetOrderByID)
			adminOrder.PUT("/:id", adminOrderHandler.UpdateOrderStatus)
			adminOrder.DELETE("/:id", adminOrderHandler.DeleteOrder)
			adminOrder.PUT("/batch-update-status", adminOrderHandler.BatchUpdateOrderStatus)
		}

		// 支付路由
		payment := apiV1.Group("/payment", middleware.AuthMiddleware(cfg))
		{
			payment.POST("", paymentHandler.CreatePayment)
			payment.POST("/wechat", paymentHandler.GenerateWechatPayment)
			payment.POST("/alipay", paymentHandler.GenerateAlipayPayment)
			payment.GET("/:id", paymentHandler.GetPaymentByID)
			payment.POST("/wechat/callback", paymentHandler.HandleWechatCallback)
			payment.POST("/alipay/callback", paymentHandler.HandleAlipayCallback)
		}

		// 沙箱测试路由 (管理员专用)
		sandboxV1 := apiV1.Group("/sandbox", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			sandboxV1.GET("/status", sandboxHandler.GetSandboxStatus)
			sandboxV1.GET("/config", sandboxHandler.GetSandboxConfig)
			sandboxV1.GET("/guide", sandboxHandler.GetSandboxTestGuide)
			sandboxV1.POST("/payment", sandboxHandler.CreateSandboxPayment)
			sandboxV1.POST("/query", sandboxHandler.QuerySandboxPayment)
			sandboxV1.POST("/simulate/wechat", sandboxHandler.SimulateWechatPayment)
			sandboxV1.POST("/simulate/alipay", sandboxHandler.SimulateAlipayPayment)
			sandboxV1.POST("/refund", sandboxHandler.CreateSandboxRefund)
		}

		// 论文路由
		paper := apiV1.Group("/paper", middleware.AuthMiddleware(cfg))
		{
			paper.POST("/parse-format-requirements", paperHandler.ParseFormatRequirements)
			paper.POST("/cqcec-format", paperHandler.HandleCQCECFormat)
			paper.GET("/standards", paperHandler.GetFormatStandards)

			// 上传并解析模板文件
			paper.POST("/upload-template", paperHandler.UploadTemplate)
			paper.POST("/upload", paperHandler.UploadPaper)
			paper.GET("", paperHandler.GetPapers)
			paper.GET("/:id", paperHandler.GetPaper)
			paper.DELETE("/:id", paperHandler.DeletePaper)
			paper.POST("/:id/check-format", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatCheck), paperHandler.CheckFormat)
			paper.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			paper.POST("/:id/apply-corrections", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatFix), paperHandler.FixFormat)
			paper.GET("/:id/compare/:check_result_id", paperHandler.ComparePaperFormats)
			paper.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
		}

		// 论文历史记录路由
		paperHistory := apiV1.Group("/paper-history", middleware.AuthMiddleware(cfg))
		{
			paperHistory.GET("", paperHistoryHandler.GetPaperCheckRecords)
			paperHistory.GET("/:id", paperHistoryHandler.GetPaperCheckRecordByID)
		}

		// 管理论文历史记录路由
		adminPaperHistory := apiV1.Group("/admin/paper-history", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			adminPaperHistory.GET("", paperHistoryHandler.GetPaperCheckRecordsForAdmin)
			adminPaperHistory.GET("/:id", paperHistoryHandler.GetPaperCheckRecordByIDForAdmin)
			adminPaperHistory.DELETE("/:id", paperHistoryHandler.DeletePaperCheckRecord)
		}

		// 兼容前端复数形式的论文路由
		papers := apiV1.Group("/papers", middleware.AuthMiddleware(cfg))
		{
			papers.POST("/upload", paperHandler.UploadPaper)
			papers.GET("", paperHandler.GetPapers)
			papers.GET("/:id", paperHandler.GetPaper)
			papers.DELETE("/:id", paperHandler.DeletePaper)
			papers.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			papers.GET("/:id/compare/:check_result_id", paperHandler.ComparePaperFormats)
			papers.GET("/:id/file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			papers.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
			papers.POST("/:id/fix/by-template", paperHandler.FixByTemplate)
		}

		// 联系表单路由
		contact := apiV1.Group("/contact")
		{
			contact.POST("/messages", contactHandler.CreateContactMessage)
			adminContact := contact.Group("", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
			{
				adminContact.GET("/messages", contactHandler.GetContactMessages)
				adminContact.GET("/messages/:id", contactHandler.GetContactMessageByID)
				adminContact.PUT("/messages/:id", contactHandler.UpdateContactMessage)
				adminContact.DELETE("/messages/:id", contactHandler.DeleteContactMessage)
			}
		}

		// 批量操作路由
		batch := apiV1.Group("/batch", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			batch.PUT("/update-status", batchHandler.BatchUpdateStatus)
			batch.DELETE("/delete", batchHandler.BatchDelete)
			batch.POST("/quick-action", batchHandler.QuickAction)
		}

		// 管理员路由
		admin := apiV1.Group("/admin", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			admin.GET("/dashboard", adminHandler.GetDashboard)
			admin.GET("/stats", adminHandler.GetSystemStats)
			admin.GET("/users", adminHandler.GetUsers)
			admin.GET("/users/:id/roles", rbacHandler.GetUserRolesByID)             // 获取用户角色列表
			admin.GET("/users/:id/permissions", rbacHandler.GetUserPermissionsByID) // 获取用户权限列表
			admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)
			admin.DELETE("/users/:id", adminHandler.DeleteUser)
			admin.POST("/users/set-super-admin", adminHandler.SetUserAsSuperAdmin) // 设置用户为超级管理员
			admin.GET("/papers", adminHandler.GetPapers)

			// 联系方式管理
			admin.GET("/support/contact", configHandler.GetContactInfo) // 获取联系信息

			// 支付策略配置
			admin.GET("/settings/payment-config", adminSystemHandler.GetPaymentConfig)
			admin.PUT("/settings/payment-config", adminSystemHandler.UpdatePaymentConfig)
			// 支付渠道参数配置
			admin.GET("/settings/payment/:provider", adminSystemHandler.GetPaymentProviderSettings)
			admin.PUT("/settings/payment/:provider", adminSystemHandler.UpdatePaymentProviderSettings)
			admin.POST("/settings/payment/:provider/test", adminSystemHandler.TestPaymentProviderSettings)
			// 图片上传
			admin.POST("/upload/image", adminSystemHandler.UploadImage)

			// 计费配置管理
			billing := admin.Group("/billing")
			{
				// 服务定价管理
				billing.GET("/services", billingHandler.GetServicePricings)
				billing.GET("/services/:type", billingHandler.GetServicePricing)
				billing.POST("/services", billingHandler.CreateServicePricing)
				billing.PUT("/services/:id", billingHandler.UpdateServicePricing)
				billing.DELETE("/services/:id", billingHandler.DeleteServicePricing)
				billing.PUT("/services/:id/toggle", billingHandler.ToggleServicePricing)
				billing.PUT("/services/:id/free", billingHandler.SetServiceFree)

				// 套餐管理
				billing.GET("/plans", billingHandler.GetPlans)
				billing.POST("/plans", billingHandler.CreatePlan)
				billing.PUT("/plans/:id", billingHandler.UpdatePlan)
				billing.DELETE("/plans/:id", billingHandler.DeletePlan)
			}

			// 系统安全设置
			security := admin.Group("/settings/security")
			{
				security.GET("", adminSystemHandler.GetSecuritySettings)
				security.PUT("", adminSystemHandler.UpdateSecuritySettings)
			}

			// 模板规范库管理（作为 standards 的别名）
			templates := admin.Group("/templates")
			{
				templates.POST("", paperHandler.CreateFormatStandard)
				templates.PUT("/:id", paperHandler.UpdateFormatStandard)
				templates.DELETE("/:id", paperHandler.DeleteFormatStandard)
				templates.GET("/:id", paperHandler.GetFormatStandardByID)
				templates.GET("/:id/display", paperHandler.GetFormatStandardForDisplay) // 获取格式标准用于前端展示
				templates.GET("", paperHandler.GetFormatStandards)
			}

			// 格式标准管理
			standards := admin.Group("/standards")
			{
				standards.POST("", paperHandler.CreateFormatStandard)
				standards.PUT("/:id", paperHandler.UpdateFormatStandard)
				standards.DELETE("/:id", paperHandler.DeleteFormatStandard)
				standards.GET("/:id", paperHandler.GetFormatStandardByID)
				standards.GET("", paperHandler.GetFormatStandards)
			}
		}

		// 高校相关路由
		apiV1.GET("/universities", universityHandler.GetUniversities)
		apiV1.GET("/universities/tags", universityHandler.GetTags)
		apiV1.GET("/universities/:id", universityHandler.GetUniversityDetail)
		apiV1.GET("/universities/:id/download-template", universityHandler.DownloadTemplate)

		// 管理员高校管理路由
		adminUniversities := apiV1.Group("/admin/universities", middleware.AuthMiddleware(cfg), middleware.AdminMiddleware())
		{
			adminUniversities.GET("", adminUniversityHandler.GetUniversities)
			adminUniversities.GET("/:id", adminUniversityHandler.GetUniversity)
			adminUniversities.POST("", adminUniversityHandler.CreateUniversity)
			adminUniversities.POST("/parse-template", adminUniversityHandler.ParseTemplate) // 解析模板
			adminUniversities.PUT("/:id", adminUniversityHandler.UpdateUniversity)
			adminUniversities.DELETE("/:id", adminUniversityHandler.DeleteUniversity)
			adminUniversities.PUT("", adminUniversityHandler.BatchUpdateUniversities) // 批量更新
		}
	}

	//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
	// 静态文件路由 - 提供上传的文件
	router.Static("/uploads", "./uploads")

	// 健康检查路由
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 启动服务器
	serverAddr := fmt.Sprintf(":%d", cfg.Server.Port)

	if err := http.ListenAndServe(serverAddr, router); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func isPortInUse(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

func killProcessUsingPort(port int) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// 在Windows上，我们首先需要找到使用该端口的进程ID
		findCmd := exec.Command("netstat", "-ano", "-p", "tcp")
		output, err := findCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to execute netstat: %v", err)
		}

		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, fmt.Sprintf(":%d", port)) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					pid := fields[len(fields)-1]
					// 杀死进程
					killCmd := exec.Command("taskkill", "/F", "/PID", pid)
					return killCmd.Run()
				}
			}
		}
		return fmt.Errorf("process using port %d not found", port)
	} else {
		// 在Unix/Linux系统上使用fuser命令
		cmd = exec.Command("fuser", "-k", strconv.Itoa(port)+"/tcp")
		return cmd.Run()
	}
}
