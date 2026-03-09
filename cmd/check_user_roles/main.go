package main

import (
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

func main() {
	configPath := ".env"
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	if err := database.InitDatabase(cfg); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	fmt.Println("Database connected successfully")

	// 检查指定的 admin 用户
	adminUserID, _ := uuid.Parse("7ec52f1f-c859-4f83-8831-640b45157c59")
	var adminUser model.User
	if err := database.DB.Preload("Roles.Menus").First(&adminUser, adminUserID).Error; err != nil {
		log.Fatalf("admin user not found: %v", err)
	}

	fmt.Println("=== admin 用户信息 ===")
	fmt.Printf("用户 ID: %s\n", adminUser.ID)
	fmt.Printf("用户名：%s\n", adminUser.Username)
	fmt.Printf("邮箱：%s\n", adminUser.Email)
	fmt.Printf("角色数：%d\n\n", len(adminUser.Roles))

	for i, role := range adminUser.Roles {
		fmt.Printf("角色 %d: %s (%s)\n", i+1, role.Name, role.Code)
		fmt.Printf("  角色 ID: %s\n", role.ID)

		// 获取角色的菜单
		var menus []model.Menu
		if err := database.DB.Model(&role).Association("Menus").Find(&menus); err != nil {
			fmt.Printf("  菜单数：获取失败 - %v\n", err)
		} else {
			fmt.Printf("  菜单数：%d\n", len(menus))
			if len(menus) > 0 {
				fmt.Println("  菜单列表:")
				for j, menu := range menus {
					fmt.Printf("    %d. %s - %s (组件：%s)\n", j+1, menu.Title, menu.Path, menu.Component)
				}
			}
		}
		fmt.Println()
	}

	// 获取所有角色
	var allRoles []model.Role
	if err := database.DB.Find(&allRoles).Error; err != nil {
		log.Fatalf("Failed to get roles: %v", err)
	}

	fmt.Println("\n=== 所有角色及其菜单 ===")
	for i, role := range allRoles {
		fmt.Printf("\n%d. %s (%s)\n", i+1, role.Name, role.Code)
		var menus []model.Menu
		if err := database.DB.Model(&role).Association("Menus").Find(&menus); err != nil {
			fmt.Printf("   菜单数：获取失败\n")
		} else {
			fmt.Printf("   菜单数：%d\n", len(menus))
		}
	}
}
