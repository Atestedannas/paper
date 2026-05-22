package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/handler"
	"github.com/paper-format-checker/backend/internal/logger"
	"github.com/paper-format-checker/backend/internal/middleware"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
)

func main() {
	// Load configuration
	configPath := ".env"
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	if err := logger.InitLogrusJSONFile(""); err != nil {
		log.Printf("Warning: logrus JSON file init failed: %v", err)
	}
	log.Printf("RBAC model: %s", cfg.RBAC.Model)

	// Check if port is in use, if so try to kill the process using it
	if isPortInUse(cfg.Server.Port) {
		if err := killProcessUsingPort(cfg.Server.Port); err != nil {
			log.Printf("Warning: failed to kill process using port %d: %v", cfg.Server.Port, err)
		}
		time.Sleep(500 * time.Millisecond)
		if isPortInUse(cfg.Server.Port) {
			log.Fatalf("Port %d is still in use after kill attempt", cfg.Server.Port)
		}
	}

	// Initialize database (allow failure in demo mode)
	if err := database.InitDatabase(cfg); err != nil {
		log.Printf("Warning: Failed to initialize database: %v", err)
	}

	// Auto migrate Token related tables
	if database.DB != nil {
		if err := database.DB.AutoMigrate(&model.TokenBlacklist{}, &model.RefreshToken{}); err != nil {
			log.Printf("Warning: Failed to migrate token tables: %v", err)
		} else {
			log.Println("Successfully migrated token tables")
		}

		// Initialize Casbin
		casbinService := service.NewCasbinService()
		if err := casbinService.Init(); err != nil {
			log.Printf("Warning: Failed to initialize Casbin: %v", err)
		} else {
			log.Println("Successfully initialized Casbin")
		}
	}

	// Fix foreign key constraint for orders table member_level_id field
	if err := database.ResetOrdersMemberLevel(); err != nil {
		log.Printf("Warning: Failed to reset member_level_id: %v", err)
	} else {
		log.Println("Successfully reset member_level_id constraint")
	}

	// Execute database migration and initialization (explicitly call at startup)
	if err := database.PerformMigration(); err != nil {
		log.Printf("Warning: Failed to perform database migration: %v", err)
	}

	// Create Gin router
	router := gin.Default()

	// Configure CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Add API rate limiting middleware
	router.Use(middleware.RateLimitMiddleware())

	// Initialize handlers
	authHandler := handler.NewAuthHandler(cfg, database.DB)
	memberHandler := handler.NewMemberHandler()
	orderHandler := handler.NewOrderHandler()
	paymentHandler := handler.NewPaymentHandler(cfg)
	paperHandler := handler.NewPaperHandler(cfg)
	contactHandler := handler.NewContactHandler()
	cmsHandler := handler.NewCmsHandler()
	adminHandler := handler.NewAdminHandler(cfg)
	adminSystemHandler := handler.NewAdminSystemHandler()
	universityHandler := handler.NewUniversityHandler()
	adminUniversityHandler := handler.NewAdminUniversityHandler()
	adminTemplateHandler := handler.NewAdminTemplateHandler()
	rbacHandler := handler.NewRBACHandler()
	configHandler := handler.NewConfigHandler()
	billingHandler := handler.NewBillingHandler()
	paperWorkflowHandler := handler.NewPaperWorkflowHandler(service.NewPaperWorkflowService(database.DB))

	// New handlers
	adminOrderHandler := handler.NewAdminOrderHandler()
	paperHistoryHandler := handler.NewPaperHistoryHandler()
	batchHandler := handler.NewBatchHandler()
	paymentCheckHandler := handler.NewPaymentCheckHandler()
	sandboxHandler := handler.NewSandboxHandler(cfg)

	// RBAC enhanced handlers
	menuHandler := handler.NewMenuHandler()
	casbinHandler := handler.NewCasbinHandler()

	// ACL handlers
	aclHandler := handler.NewACLHandler()

	// API routes - keep original route structure, add v1 version support
	api := router.Group("/api")
	{
		// Public routes (no authentication required)
		api.GET("/config/public/paper-check", configHandler.GetPaperCheckConfigPublic) // Publicly get paper format check config
		api.GET("/config/public/contact", configHandler.GetContactInfo)                // Publicly get contact info
		api.GET("/config/public/billing", billingHandler.GetServiceConfig)             // Publicly get billing config
		api.GET("/config/public/billing/check", billingHandler.CheckServicePricing)    // Publicly check service pricing

		// 支付异步通知（支付宝/微信回调不能走 JWT）
		api.POST("/payment/wechat/callback", paymentHandler.HandleWechatCallback)
		api.POST("/payment/alipay/callback", paymentHandler.HandleAlipayCallback)

		api.PUT("/order/:id/status", orderHandler.UpdateOrderStatus)
		// Authentication routes
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)                                       // Add refresh token route
			auth.POST("/logout", middleware.AuthMiddleware(cfg, database.DB), authHandler.Logout) // Add logout route
			auth.GET("/profile", middleware.AuthMiddleware(cfg, database.DB), authHandler.GetProfile)
			auth.PUT("/profile", middleware.AuthMiddleware(cfg, database.DB), authHandler.UpdateProfile)
			auth.PUT("/password", middleware.AuthMiddleware(cfg, database.DB), authHandler.ChangePassword)

			// Alipay login (GET for platform redirect, POST for API call)
			auth.GET("/wechat/login-url", authHandler.GetWechatAuthURL)
			auth.GET("/wechat/callback", authHandler.WechatAuthCallback)
			auth.POST("/wechat/callback", authHandler.WechatAuthCallback)
			auth.GET("/alipay/login-url", authHandler.GetAlipayAuthURL)
			auth.GET("/alipay/qr-session", authHandler.GetAlipayQRSession)
			auth.GET("/alipay/qr-session/status", authHandler.GetAlipayQRSessionStatus)
			auth.GET("/alipay/qr-session/:session_id", authHandler.GetAlipayQRSessionStatus)
			auth.GET("/alipay/login", authHandler.RedirectAlipayLogin)
			auth.GET("/alipay/callback", authHandler.AlipayAuthCallback)
			auth.POST("/alipay/callback", authHandler.AlipayAuthCallback)
			auth.GET("/alipay/qr-callback", authHandler.AlipayAuthCallback)
			auth.POST("/alipay/qr-callback", authHandler.AlipayAuthCallback)

			// File download
			auth.GET("/papers/:id/file", middleware.AuthMiddleware(cfg, database.DB), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			auth.GET("/papers/:id/corrected-file", middleware.AuthMiddleware(cfg, database.DB), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)

			// Payment check APIs
			auth.GET("/payment/check-paper", middleware.AuthMiddleware(cfg, database.DB), paymentCheckHandler.CheckPaperPaymentStatus)     // Check paper service payment status
			auth.GET("/payment/check-service", middleware.AuthMiddleware(cfg, database.DB), paymentCheckHandler.CheckServicePaymentStatus) // Check general service payment status
			auth.GET("/payment/free-checks", middleware.AuthMiddleware(cfg, database.DB), paymentCheckHandler.GetUserFreeChecks)           // Get user remaining free check count

			// System config
			auth.GET("/config/system", middleware.AuthMiddleware(cfg, database.DB), configHandler.GetSystemConfig)          // Get system config
			auth.GET("/config/paper-check", middleware.AuthMiddleware(cfg, database.DB), configHandler.GetPaperCheckConfig) // Get paper format check config
		}

		// Member routes
		member := api.Group("/member", middleware.AuthMiddleware(cfg, database.DB))
		{
			member.GET("/levels", memberHandler.GetAllMemberLevels)
			member.GET("/levels/:id", memberHandler.GetMemberLevelByID)
			member.GET("/info", memberHandler.GetMemberInfo)
			member.GET("/status", memberHandler.CheckMemberStatus)
			member.GET("/remaining-checks", memberHandler.GetMemberRemainingChecks)

			// Admin routes
			admin := member.Group("/admin", middleware.AdminMiddleware())
			{
				admin.POST("/levels", memberHandler.CreateMemberLevel)
				admin.PUT("/levels/:id", memberHandler.UpdateMemberLevel)
			}
		}

		// Order routes
		order := api.Group("/order", middleware.AuthMiddleware(cfg, database.DB))
		{
			order.POST("", orderHandler.CreateOrder)
			order.GET("/:id", orderHandler.GetOrderByID)
			order.GET("", orderHandler.GetOrdersByUserID)
			order.PUT("/:id/cancel", orderHandler.CancelOrder)
			order.GET("/statistics", orderHandler.GetOrderStatistics)

			// Payment callback routes (no authentication required)
			//order.PUT("/:id/status", orderHandler.UpdateOrderStatus)
		}

		// Payment routes
		payment := api.Group("/payment", middleware.AuthMiddleware(cfg, database.DB))
		{
			payment.POST("", paymentHandler.CreatePayment)
			payment.POST("/wechat", paymentHandler.GenerateWechatPayment)
			payment.POST("/alipay", paymentHandler.GenerateAlipayPayment)
			payment.GET("/:id", paymentHandler.GetPaymentByID)
		}

		// Sandbox test routes (admin only)
		sandbox := api.Group("/sandbox", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
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

		// Paper routes
		paper := api.Group("/paper", middleware.AuthMiddleware(cfg, database.DB))
		{
			paper.POST("/parse-format-requirements", paperHandler.ParseFormatRequirements)

			// Chongqing Institute of Technology format processing
			paper.POST("/cqcec-format", paperHandler.HandleCQCECFormat)

			// Format standard related
			paper.GET("/standards", paperHandler.GetFormatStandards)

			// Upload and parse template file
			paper.POST("/upload-template", paperHandler.UploadTemplate)

			// Paper upload and management
			// POST /upload → PaperHandler.UploadPaper：multipart 上传论文文件，付费门闸、校验、落库后异步格式检查/修正
			paper.POST("/upload", paperHandler.UploadPaper)

			paper.GET("", paperHandler.GetPapers)
			paper.GET("/:id", paperHandler.GetPaper)
			paper.DELETE("/:id", paperHandler.DeletePaper)

			// Format check and fix
			paper.POST("/:id/check-format", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatCheck), paperHandler.CheckFormat)
			paper.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			paper.POST("/:id/apply-corrections", middleware.PaymentMiddleware(cfg, middleware.ServiceFormatFix), paperHandler.FixFormat)

			// Format comparison feature
			paper.GET("/:id/compare/:check_result_id", paperHandler.ComparePaperFormats)

			// Corrected file download
			paper.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
		}

		// Compatible with frontend plural form paper routes
		papers := api.Group("/papers", middleware.ConditionalAuthMiddleware(cfg, database.DB, middleware.ServiceFormatCheck))
		{
			papers.GET("", paperHandler.GetPapers)
			papers.GET("/:id", paperHandler.GetPaper)
			papers.DELETE("/:id", paperHandler.DeletePaper)
			papers.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			papers.GET("/:id/export-report", middleware.PaymentMiddleware(cfg, middleware.ServiceReportDownload), paperHandler.DownloadCheckReport)
			papers.GET("/:id/export-report-html", middleware.PaymentMiddleware(cfg, middleware.ServiceReportDownload), paperHandler.DownloadCheckReportHTML)
			papers.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
			papers.GET("/:id/review-diffs", paperHandler.ReviewDiffs)
			papers.POST("/:id/apply-diffs", paperHandler.ApplySelectedDiffs)
		}

		// Contact form routes
		contact := api.Group("/contact")
		{
			contact.POST("/messages", contactHandler.CreateContactMessage) // Create contact message (no authentication)
			// Add admin authentication middleware to following routes
			adminContact := contact.Group("", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
			{
				adminContact.GET("/messages", contactHandler.GetContactMessages)          // Get all contact messages
				adminContact.GET("/messages/:id", contactHandler.GetContactMessageByID)   // Get single contact message
				adminContact.PUT("/messages/:id", contactHandler.UpdateContactMessage)    // Update contact message
				adminContact.DELETE("/messages/:id", contactHandler.DeleteContactMessage) // Delete contact message
			}
		}

		// Admin routes
		admin := api.Group("/admin", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware(), middleware.AdminRBACMiddleware())
		{
			// Dashboard
			admin.GET("/dashboard", adminHandler.GetDashboard) // Get admin dashboard data
			admin.GET("/stats", adminHandler.GetSystemStats)   // Get system statistics

			// User management
			admin.GET("/users", adminHandler.GetUsers)                                       // Get user list
			admin.POST("/users", adminHandler.CreateUser)                                    // Create user
			admin.PUT("/users/:id", adminHandler.UpdateUser)                                 // Update user
			admin.GET("/users/:id/roles", rbacHandler.GetUserRolesByID)                      // Get user role list
			admin.GET("/users/:id/permissions", rbacHandler.GetUserPermissionsByID)          // Get user permission list (all)
			admin.GET("/users/:id/direct-permissions", rbacHandler.GetUserDirectPermissions) // Get user direct permissions only
			admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)                        // Update user role
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)                    // Update user status
			admin.DELETE("/users/:id", adminHandler.DeleteUser)                              // Delete user
			admin.POST("/users/set-super-admin", adminHandler.SetUserAsSuperAdmin)           // Set user as super admin

			// Paper management
			admin.GET("/papers", adminHandler.GetPapers)
			admin.DELETE("/papers/:id", adminHandler.DeletePaper)
			admin.POST("/papers/batch-delete", adminHandler.BatchDeletePapers)
			admin.POST("/papers/batch-force-delete", adminHandler.BatchForceDeletePapers)
			admin.POST("/papers/batch-check", adminHandler.BatchCheckPapers)
			admin.POST("/papers/:id/check-format", adminHandler.CheckPaperFormat)
			admin.GET("/papers/:id/file", adminHandler.DownloadPaperFile)

			// Paper restore endpoints (soft delete recovery)
			admin.POST("/papers/:id/restore", adminHandler.RestorePaper)
			admin.POST("/papers/batch-restore", adminHandler.BatchRestorePapers)

			// RBAC permission management
			admin.GET("/roles", rbacHandler.GetRoles)    // Get role list
			admin.POST("/roles", rbacHandler.CreateRole) // Create role

			// Enhanced RBAC routes - Menu management (must be before /roles/:id to avoid conflict)
			admin.GET("/roles/:id/menus", menuHandler.GetRoleMenus)
			admin.POST("/roles/:id/menus", menuHandler.AssignMenusToRole)
			admin.GET("/roles/:id/users", rbacHandler.GetUsersByRole)

			admin.GET("/roles/:id", rbacHandler.GetRoleByID)                                                      // Get role details
			admin.PUT("/roles/:id", rbacHandler.UpdateRole)                                                       // Update role
			admin.DELETE("/roles/:id", rbacHandler.DeleteRole)                                                    // Delete role
			admin.GET("/roles/:id/permissions", rbacHandler.GetRolePermissions)                                   // Get role permissions
			admin.GET("/permissions", rbacHandler.GetPermissions)                                                 // Get permission list
			admin.POST("/permissions", rbacHandler.CreatePermission)                                              // Create permission
			admin.GET("/permissions/:id", rbacHandler.GetPermissionByID)                                          // Get permission details
			admin.PUT("/permissions/:id", rbacHandler.UpdatePermission)                                           // Update permission
			admin.DELETE("/permissions/:id", rbacHandler.DeletePermission)                                        // Delete permission
			admin.PUT("/user-role-assign/:user_id/:role_id", rbacHandler.AssignRoleToUser)                        // Assign role to user
			admin.DELETE("/user-role-assign/:user_id/:role_id", rbacHandler.RemoveRoleFromUser)                   // Remove role from user
			admin.PUT("/role-permission-assign/:role_id/:permission_id", rbacHandler.AssignPermissionToRole)      // Assign permission to role
			admin.DELETE("/role-permission-assign/:role_id/:permission_id", rbacHandler.RemovePermissionFromRole) // Remove permission from role
			admin.PUT("/user-permission-assign/:user_id/:permission_id", rbacHandler.AssignPermissionToUser)      // Assign permission to user
			admin.DELETE("/user-permission-assign/:user_id/:permission_id", rbacHandler.RemovePermissionFromUser) // Remove permission from user

			// Enhanced RBAC routes - Menu management
			admin.GET("/menus/tree", menuHandler.GetMenuTree)
			admin.GET("/menus/user-tree", menuHandler.GetUserMenus) // 获取当前登录用户的菜单树
			admin.GET("/menus", menuHandler.GetAllMenus)
			admin.GET("/menus/user", menuHandler.GetUserMenus)
			admin.POST("/menus", menuHandler.CreateMenu)
			admin.PUT("/menus/:id", menuHandler.UpdateMenu)
			admin.DELETE("/menus/:id", menuHandler.DeleteMenu)
			admin.GET("/menus/:id", menuHandler.GetMenuByID)

			// Casbin strategy management
			admin.POST("/casbin/enforce", casbinHandler.Enforce)
			admin.POST("/casbin/policy", casbinHandler.AddPolicy)
			admin.DELETE("/casbin/policy", casbinHandler.RemovePolicy)
			admin.POST("/casbin/grouping-policy", casbinHandler.AddGroupingPolicy)
			admin.DELETE("/casbin/grouping-policy", casbinHandler.RemoveGroupingPolicy)
			admin.GET("/casbin/policy/all", casbinHandler.GetPolicy)
			admin.POST("/casbin/policy/load", casbinHandler.LoadPolicy)
			admin.POST("/casbin/policy/save", casbinHandler.SavePolicy)
			admin.GET("/casbin/user/permissions", casbinHandler.GetPermissionsForUser)
			admin.GET("/casbin/user/roles", casbinHandler.GetRolesForUser)

			// ACL 行级权限管理
			admin.POST("/acl/grant", aclHandler.GrantAccess)
			admin.POST("/acl/revoke", aclHandler.RevokeAccess)
			admin.GET("/acl/can-access/:user_id", aclHandler.CanAccess)
			admin.GET("/acl/accessible/:user_id", aclHandler.GetAccessibleResources)
			admin.GET("/acl/resource", aclHandler.GetResourceACL)
			admin.GET("/acl/user/:user_id", aclHandler.GetUserACLs)
			admin.DELETE("/acl/resource", aclHandler.DeleteResourceACL)
		}
	}

	// Add v1 version API routes - same as original routes, for future extension
	apiV1 := router.Group("/api/v1")
	{
		// Public routes (no authentication)
		apiV1.GET("/config/public/paper-check", configHandler.GetPaperCheckConfigPublic) // Publicly get paper format check config
		apiV1.GET("/config/public/contact", configHandler.GetContactInfo)                // Publicly get contact info

		apiV1.POST("/payment/wechat/callback", paymentHandler.HandleWechatCallback)
		apiV1.POST("/payment/alipay/callback", paymentHandler.HandleAlipayCallback)

		// Authentication routes
		auth := apiV1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.POST("/logout", middleware.AuthMiddleware(cfg, database.DB), authHandler.Logout)
			auth.GET("/profile", middleware.AuthMiddleware(cfg, database.DB), authHandler.GetProfile)
			auth.PUT("/profile", middleware.AuthMiddleware(cfg, database.DB), authHandler.UpdateProfile)
			auth.PUT("/password", middleware.AuthMiddleware(cfg, database.DB), authHandler.ChangePassword)

			// OAuth login (GET for platform redirect, POST for API call)
			auth.GET("/wechat/login-url", authHandler.GetWechatAuthURL)
			auth.GET("/wechat/callback", authHandler.WechatAuthCallback)
			auth.POST("/wechat/callback", authHandler.WechatAuthCallback)
			auth.GET("/alipay/login-url", authHandler.GetAlipayAuthURL)
			auth.GET("/alipay/qr-session", authHandler.GetAlipayQRSession)
			auth.GET("/alipay/qr-session/status", authHandler.GetAlipayQRSessionStatus)
			auth.GET("/alipay/qr-session/:session_id", authHandler.GetAlipayQRSessionStatus)
			auth.GET("/alipay/login", authHandler.RedirectAlipayLogin)
			auth.GET("/alipay/callback", authHandler.AlipayAuthCallback)
			auth.POST("/alipay/callback", authHandler.AlipayAuthCallback)
			auth.GET("/alipay/qr-callback", authHandler.AlipayAuthCallback)
			auth.POST("/alipay/qr-callback", authHandler.AlipayAuthCallback)

			// File download
			auth.GET("/papers/:id/file", middleware.AuthMiddleware(cfg, database.DB), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			auth.GET("/papers/:id/corrected-file", middleware.AuthMiddleware(cfg, database.DB), middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)

			// System config
			auth.GET("/config/system", configHandler.GetSystemConfig)          // Get system config
			auth.GET("/config/paper-check", configHandler.GetPaperCheckConfig) // Get paper format check config
		}

		// Member routes
		member := apiV1.Group("/member", middleware.AuthMiddleware(cfg, database.DB))
		{
			member.GET("/levels", memberHandler.GetAllMemberLevels)
			member.GET("/levels/:id", memberHandler.GetMemberLevelByID)
			member.GET("/info", memberHandler.GetMemberInfo)
			member.GET("/status", memberHandler.CheckMemberStatus)
			member.GET("/remaining-checks", memberHandler.GetMemberRemainingChecks)

			// Admin routes
			admin := member.Group("/admin", middleware.AdminMiddleware())
			{
				admin.POST("/levels", memberHandler.CreateMemberLevel)
				admin.PUT("/levels/:id", memberHandler.UpdateMemberLevel)
			}
		}

		// Order routes
		order := apiV1.Group("/order", middleware.AuthMiddleware(cfg, database.DB))
		{
			order.POST("", orderHandler.CreateOrder)
			order.GET("/:id", orderHandler.GetOrderByID)
			order.GET("", orderHandler.GetOrdersByUserID)
			order.PUT("/:id/cancel", orderHandler.CancelOrder)
			order.GET("/statistics", orderHandler.GetOrderStatistics)
			order.PUT("/:id/status", orderHandler.UpdateOrderStatus)
		}

		// Admin order routes
		adminOrder := apiV1.Group("/admin/orders", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
		{
			adminOrder.GET("", adminOrderHandler.GetOrders)
			adminOrder.GET("/statistics", adminOrderHandler.GetOrderStatisticsForAdmin)
			adminOrder.GET("/:id", adminOrderHandler.GetOrderByID)
			adminOrder.PUT("/:id", adminOrderHandler.UpdateOrderStatus)
			adminOrder.DELETE("/:id", adminOrderHandler.DeleteOrder)
			adminOrder.PUT("/batch-update-status", adminOrderHandler.BatchUpdateOrderStatus)
		}

		// Payment routes
		payment := apiV1.Group("/payment", middleware.AuthMiddleware(cfg, database.DB))
		{
			payment.POST("", paymentHandler.CreatePayment)
			payment.POST("/wechat", paymentHandler.GenerateWechatPayment)
			payment.POST("/alipay", paymentHandler.GenerateAlipayPayment)
			payment.GET("/:id", paymentHandler.GetPaymentByID)
		}
		// Sandbox test routes (admin only)
		sandboxV1 := apiV1.Group("/sandbox", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
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

		// Paper routes
		paper := apiV1.Group("/paper", middleware.AuthMiddleware(cfg, database.DB))
		{
			paper.POST("/parse-format-requirements", paperHandler.ParseFormatRequirements)
			paper.POST("/cqcec-format", paperHandler.HandleCQCECFormat)
			paper.GET("/standards", paperHandler.GetFormatStandards)

			// Upload and parse template file
			paper.POST("/upload-template", paperHandler.UploadTemplate)

			// Paper upload and management
			// POST /upload → PaperHandler.UploadPaper：multipart 上传论文文件，付费门闸、校验、落库后异步格式检查/修正
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

		// Paper history record routes
		paperHistory := apiV1.Group("/paper-history", middleware.AuthMiddleware(cfg, database.DB))
		{
			paperHistory.GET("", paperHistoryHandler.GetPaperCheckRecords)
			paperHistory.GET("/:id", paperHistoryHandler.GetPaperCheckRecordByID)
		}

		// Admin paper history record routes
		adminPaperHistory := apiV1.Group("/admin/paper-history", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
		{
			adminPaperHistory.GET("", paperHistoryHandler.GetPaperCheckRecordsForAdmin)
			adminPaperHistory.GET("/:id", paperHistoryHandler.GetPaperCheckRecordByIDForAdmin)
			adminPaperHistory.DELETE("/:id", paperHistoryHandler.DeletePaperCheckRecord)
		}

		// Compatible with frontend plural form paper routes
		papers := apiV1.Group("/papers", middleware.AuthMiddleware(cfg, database.DB))
		{
			// POST /papers/upload → 与 /paper/upload 相同处理函数 UploadPaper
			papers.POST("/upload", paperHandler.UploadPaper)
			papers.GET("", paperHandler.GetPapers)
			papers.GET("/:id", paperHandler.GetPaper)
			papers.DELETE("/:id", paperHandler.DeletePaper)
			papers.GET("/:id/check-result", paperHandler.GetPaperCheckResults)
			papers.GET("/:id/compare/:check_result_id", paperHandler.ComparePaperFormats)
			papers.GET("/:id/export-report", middleware.PaymentMiddleware(cfg, middleware.ServiceReportDownload), paperHandler.DownloadCheckReport)
			papers.GET("/:id/export-report-html", middleware.PaymentMiddleware(cfg, middleware.ServiceReportDownload), paperHandler.DownloadCheckReportHTML)
			papers.GET("/:id/file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetPaperFile)
			papers.GET("/:id/corrected-file", middleware.PaymentMiddleware(cfg, middleware.ServicePaperDownload), paperHandler.GetCorrectedPaperFile)
			papers.POST("/:id/fix/by-template", paperHandler.FixByTemplate)
			papers.GET("/:id/review-diffs", paperHandler.ReviewDiffs)
			papers.POST("/:id/apply-diffs", paperHandler.ApplySelectedDiffs)
		}

		// Template import route (for admin template library)
		templates := apiV1.Group("/templates", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
		{
			templates.POST("/import", paperHandler.UploadTemplate)
		}

		// Contact form routes
		contact := apiV1.Group("/contact")
		{
			contact.POST("/messages", contactHandler.CreateContactMessage) // Create contact message (no authentication)
			// Add admin authentication middleware to following routes
			adminContact := contact.Group("", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
			{
				adminContact.GET("/messages", contactHandler.GetContactMessages)
				adminContact.GET("/messages/:id", contactHandler.GetContactMessageByID)
				adminContact.PUT("/messages/:id", contactHandler.UpdateContactMessage)
				adminContact.DELETE("/messages/:id", contactHandler.DeleteContactMessage)
			}
		}

		// CMS 帖子路由
		cmsPublic := apiV1.Group("/cms")
		{
			cmsPublic.GET("/posts", cmsHandler.ListPosts)
			cmsPublic.GET("/posts/:id", cmsHandler.GetPost)
		}
		cmsAuth := apiV1.Group("/cms", middleware.AuthMiddleware(cfg, database.DB))
		{
			cmsAuth.POST("/posts", cmsHandler.CreatePost)
			cmsAuth.POST("/posts/:id/replies", cmsHandler.CreateReply)
			cmsAuth.DELETE("/posts/:id", cmsHandler.DeletePost)
			cmsAuth.DELETE("/posts/:id/replies/:replyId", cmsHandler.DeleteReply)
		}

		// General upload routes (auth required, no admin needed)
		upload := apiV1.Group("/upload", middleware.AuthMiddleware(cfg, database.DB))
		{
			upload.POST("/image", adminSystemHandler.UploadImage)
		}

		// Batch operation routes
		batch := apiV1.Group("/batch", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
		{
			batch.PUT("/update-status", batchHandler.BatchUpdateStatus)
			batch.DELETE("/delete", batchHandler.BatchDelete)
			batch.POST("/quick-action", batchHandler.QuickAction)
		}

		// Admin routes
		admin := apiV1.Group("/admin", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware(), middleware.AdminRBACMiddleware())
		{
			admin.GET("/dashboard", adminHandler.GetDashboard)
			admin.GET("/stats", adminHandler.GetSystemStats)
			admin.GET("/users", adminHandler.GetUsers)
			admin.POST("/users", adminHandler.CreateUser)                                    // Create user
			admin.PUT("/users/:id", adminHandler.UpdateUser)                                 // Update user
			admin.GET("/users/:id/roles", rbacHandler.GetUserRolesByID)                      // 閼惧嘲褰囥劍鍩涚憴鎺曞閸掓銆?			admin.GET("/users/:id/permissions", rbacHandler.GetUserPermissionsByID) // 閼惧嘲褰囬悽銊﹀煕閺夊啴閸掓
			admin.GET("/users/:id/direct-permissions", rbacHandler.GetUserDirectPermissions) // 鑾峰彇鐢ㄦ埛鐩存帴鍒嗛厤鐨勬潈闄?			admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)
			admin.DELETE("/users/:id", adminHandler.DeleteUser)
			admin.POST("/users/set-super-admin", adminHandler.SetUserAsSuperAdmin) // 璁剧疆鐢ㄦ埛涓鸿秴绾х鐞嗗憳
			admin.GET("/papers", adminHandler.GetPapers)
			admin.DELETE("/papers/:id", adminHandler.DeletePaper)
			admin.POST("/papers/batch-delete", adminHandler.BatchDeletePapers)
			admin.POST("/papers/batch-force-delete", adminHandler.BatchForceDeletePapers)
			admin.POST("/papers/batch-check", adminHandler.BatchCheckPapers)
			admin.POST("/papers/:id/check-format", adminHandler.CheckPaperFormat)
			admin.GET("/papers/:id/file", adminHandler.DownloadPaperFile)

			// Paper restore endpoints (soft delete recovery)
			admin.POST("/papers/:id/restore", adminHandler.RestorePaper)
			admin.POST("/papers/batch-restore", adminHandler.BatchRestorePapers)

			// Contact method management
			admin.GET("/support/contact", configHandler.GetContactInfo)    // Get contact info
			admin.PUT("/support/contact", configHandler.UpdateContactInfo) // Update contact info

			// Payment strategy configuration
			admin.GET("/settings/payment-config", adminSystemHandler.GetPaymentConfig)
			admin.PUT("/settings/payment-config", adminSystemHandler.UpdatePaymentConfig)
			// Payment channel parameter configuration
			admin.GET("/settings/payment/:provider", adminSystemHandler.GetPaymentProviderSettings)
			admin.PUT("/settings/payment/:provider", adminSystemHandler.UpdatePaymentProviderSettings)
			admin.POST("/settings/payment/:provider/test", adminSystemHandler.TestPaymentProviderSettings)

			// Image upload (admin)
			admin.POST("/upload/image", adminSystemHandler.UploadImage)

			// Billing configuration management
			billing := admin.Group("/billing")
			{
				// Service pricing management
				billing.GET("/services", billingHandler.GetServicePricings)
				billing.GET("/services/:type", billingHandler.GetServicePricing)
				billing.POST("/services", billingHandler.CreateServicePricing)
				billing.PUT("/services/:id", billingHandler.UpdateServicePricing)
				billing.DELETE("/services/:id", billingHandler.DeleteServicePricing)
				billing.PUT("/services/:id/toggle", billingHandler.ToggleServicePricing)
				billing.PUT("/services/:id/free", billingHandler.SetServiceFree)

				// Package management
				billing.GET("/plans", billingHandler.GetPlans)
				billing.POST("/plans", billingHandler.CreatePlan)
				billing.PUT("/plans/:id", billingHandler.UpdatePlan)
				billing.DELETE("/plans/:id", billingHandler.DeletePlan)
			}

			// System security settings
			security := admin.Group("/settings/security")
			{
				security.GET("", adminSystemHandler.GetSecuritySettings)
				security.PUT("", adminSystemHandler.UpdateSecuritySettings)
			}

			// Template library management (as alias for standards)
			templates := admin.Group("/templates")
			{
				templates.GET("", adminTemplateHandler.GetTemplates)
				templates.POST("", adminTemplateHandler.CreateTemplate)
				templates.GET("/:id", adminTemplateHandler.GetTemplate)
				templates.PUT("/:id", adminTemplateHandler.UpdateTemplate)
				templates.DELETE("/:id", adminTemplateHandler.DeleteTemplate)
				templates.PUT("/:id/toggle", adminTemplateHandler.ToggleTemplate)
				templates.GET("/:id/versions", adminTemplateHandler.GetTemplateVersions)
				templates.POST("/:id/versions/:versionId/promote", adminTemplateHandler.PromoteTemplateVersion)
				templates.POST("/parse-paper", adminTemplateHandler.ParsePaperToTemplate)
				templates.GET("/:id/usage-stats", adminTemplateHandler.GetTemplateUsageStats)

				// legacy aliases
				templates.GET("/:id/display", paperHandler.GetFormatStandardForDisplay)
			}

			// Format standard management
			standards := admin.Group("/standards")
			{
				standards.POST("", paperHandler.CreateFormatStandard)
				standards.PUT("/:id", paperHandler.UpdateFormatStandard)
				standards.DELETE("/:id", paperHandler.DeleteFormatStandard)
				standards.GET("/:id", paperHandler.GetFormatStandardByID)
				standards.GET("", paperHandler.GetFormatStandards)
			}

			// RBAC permission management
			admin.GET("/roles", rbacHandler.GetRoles)    // Get role list
			admin.POST("/roles", rbacHandler.CreateRole) // Create role

			// Enhanced RBAC routes - Menu management (must be before /roles/:id to avoid conflict)
			admin.GET("/roles/:id/menus", menuHandler.GetRoleMenus)
			admin.POST("/roles/:id/menus", menuHandler.AssignMenusToRole)
			admin.GET("/roles/:id/users", rbacHandler.GetUsersByRole)

			admin.GET("/roles/:id", rbacHandler.GetRoleByID)                                                      // Get role details
			admin.PUT("/roles/:id", rbacHandler.UpdateRole)                                                       // Update role
			admin.DELETE("/roles/:id", rbacHandler.DeleteRole)                                                    // Delete role
			admin.GET("/roles/:id/permissions", rbacHandler.GetRolePermissions)                                   // Get role permissions
			admin.GET("/permissions", rbacHandler.GetPermissions)                                                 // Get permission list
			admin.POST("/permissions", rbacHandler.CreatePermission)                                              // Create permission
			admin.GET("/permissions/:id", rbacHandler.GetPermissionByID)                                          // Get permission details
			admin.PUT("/permissions/:id", rbacHandler.UpdatePermission)                                           // Update permission
			admin.DELETE("/permissions/:id", rbacHandler.DeletePermission)                                        // Delete permission
			admin.PUT("/user-role-assign/:user_id/:role_id", rbacHandler.AssignRoleToUser)                        // Assign role to user
			admin.DELETE("/user-role-assign/:user_id/:role_id", rbacHandler.RemoveRoleFromUser)                   // Remove role from user
			admin.PUT("/role-permission-assign/:role_id/:permission_id", rbacHandler.AssignPermissionToRole)      // Assign permission to role
			admin.DELETE("/role-permission-assign/:role_id/:permission_id", rbacHandler.RemovePermissionFromRole) // Remove permission from role
			admin.PUT("/user-permission-assign/:user_id/:permission_id", rbacHandler.AssignPermissionToUser)      // Assign permission to user
			admin.DELETE("/user-permission-assign/:user_id/:permission_id", rbacHandler.RemovePermissionFromUser) // Remove permission from user

			// Enhanced RBAC routes - Menu management
			admin.GET("/menus", menuHandler.GetAllMenus)
			admin.GET("/menus/tree", menuHandler.GetMenuTree)
			admin.GET("/menus/user-tree", menuHandler.GetUserMenus)
			admin.GET("/menus/user", menuHandler.GetUserMenus)
			admin.POST("/menus", menuHandler.CreateMenu)
			admin.PUT("/menus/:id", menuHandler.UpdateMenu)
			admin.DELETE("/menus/:id", menuHandler.DeleteMenu)
			admin.GET("/menus/:id", menuHandler.GetMenuByID)

			// Casbin strategy management
			admin.POST("/casbin/enforce", casbinHandler.Enforce)
			admin.POST("/casbin/policy", casbinHandler.AddPolicy)
			admin.DELETE("/casbin/policy", casbinHandler.RemovePolicy)
			admin.POST("/casbin/grouping-policy", casbinHandler.AddGroupingPolicy)
			admin.DELETE("/casbin/grouping-policy", casbinHandler.RemoveGroupingPolicy)
			admin.GET("/casbin/policy/all", casbinHandler.GetPolicy)
			admin.POST("/casbin/policy/load", casbinHandler.LoadPolicy)
			admin.POST("/casbin/policy/save", casbinHandler.SavePolicy)
			admin.GET("/casbin/user/permissions", casbinHandler.GetPermissionsForUser)
			admin.GET("/casbin/user/roles", casbinHandler.GetRolesForUser)

			// ACL 行级权限管理
			admin.POST("/acl/grant", aclHandler.GrantAccess)
			admin.POST("/acl/revoke", aclHandler.RevokeAccess)
			admin.GET("/acl/can-access/:user_id", aclHandler.CanAccess)
			admin.GET("/acl/accessible/:user_id", aclHandler.GetAccessibleResources)
			admin.GET("/acl/resource", aclHandler.GetResourceACL)
			admin.GET("/acl/user/:user_id", aclHandler.GetUserACLs)
			admin.DELETE("/acl/resource", aclHandler.DeleteResourceACL)
		}

		// University related routes
		apiV1.GET("/universities", universityHandler.GetUniversities)
		apiV1.GET("/universities/tags", universityHandler.GetTags)
		apiV1.GET("/universities/:id", universityHandler.GetUniversityDetail)
		apiV1.GET("/universities/:id/download-template", universityHandler.DownloadTemplate)

		// Admin university management routes
		adminUniversities := apiV1.Group("/admin/universities", middleware.AuthMiddleware(cfg, database.DB), middleware.AdminMiddleware())
		{
			adminUniversities.GET("", adminUniversityHandler.GetUniversities)
			adminUniversities.GET("/:id", adminUniversityHandler.GetUniversity)
			adminUniversities.POST("", adminUniversityHandler.CreateUniversity)
			adminUniversities.POST("/parse-template", adminUniversityHandler.ParseTemplate) // Parse template
			adminUniversities.PUT("/:id", adminUniversityHandler.UpdateUniversity)
			adminUniversities.DELETE("/:id", adminUniversityHandler.DeleteUniversity)
			adminUniversities.PUT("", adminUniversityHandler.BatchUpdateUniversities) // Batch update
		}
	}

	apiV2 := router.Group("/api/v2", middleware.AuthMiddleware(cfg, database.DB))
	{
		apiV2.POST("/templates/compile", paperWorkflowHandler.CompileTemplate)
		apiV2.POST("/papers", paperWorkflowHandler.CreatePaperJob)
		apiV2.POST("/jobs/:job_id/run", paperWorkflowHandler.RunJob)
		apiV2.GET("/jobs/:job_id", paperWorkflowHandler.GetJob)
		apiV2.GET("/jobs/:job_id/download", paperWorkflowHandler.DownloadJob)
	}

	// Health check route
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"rbac":   database.GetRBACHealthStatus(cfg.RBAC.Model),
		})
	})
	router.GET("/health/rbac", func(c *gin.Context) {
		c.JSON(http.StatusOK, database.GetRBACHealthStatus(cfg.RBAC.Model))
	})

	// Start server
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
	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell", "-Command",
			fmt.Sprintf("Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess | ForEach-Object { Stop-Process -Id $_ -Force }", port))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("windows kill failed: %w, output: %s", err, string(output))
		}
		return nil
	}

	// Unix-like fallback
	cmd := exec.Command("sh", "-c", fmt.Sprintf("lsof -ti tcp:%d | xargs -r kill -9", port))
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	fallback := exec.Command("fuser", "-k", fmt.Sprintf("%d/tcp", port))
	fallbackOutput, fallbackErr := fallback.CombinedOutput()
	if fallbackErr != nil {
		return fmt.Errorf("unix kill failed: %v; lsof output: %s; fuser output: %s", err, string(output), string(fallbackOutput))
	}
	return nil
}
