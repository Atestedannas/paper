package main

import (
	"fmt"
	"log"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

func main() {
	// Load configuration
	configPath := ".env"
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize database
	if err := database.InitDatabase(cfg); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	fmt.Println("Database connected successfully")

	// 1. 检查菜单数量
	var menuCount int64
	if err := database.DB.Model(&model.Menu{}).Count(&menuCount).Error; err != nil {
		log.Fatalf("Failed to count menus: %v", err)
	}
	fmt.Printf("总菜单数：%d\n", menuCount)

	// 2. 获取所有菜单
	var menus []model.Menu
	if err := database.DB.Find(&menus).Error; err != nil {
		log.Fatalf("Failed to get menus: %v", err)
	}

	fmt.Println("\n=== 所有菜单列表 ===")
	for i, menu := range menus {
		fmt.Printf("%d. %s (%s) - %s\n", i+1, menu.Title, menu.Name, menu.Path)
	}

	// 3. 获取 admin 角色
	var adminRole model.Role
	if err := database.DB.Where("code = ?", "admin").First(&adminRole).Error; err != nil {
		log.Printf("Warning: admin role not found: %v", err)
	} else {
		fmt.Printf("\n=== admin 角色信息 ===\n")
		fmt.Printf("角色 ID: %s\n", adminRole.ID)
		fmt.Printf("角色名称：%s\n", adminRole.Name)
		fmt.Printf("角色代码：%s\n", adminRole.Code)

		// 获取 admin 角色的菜单
		var adminMenus []model.Menu
		if err := database.DB.Model(&adminRole).Association("Menus").Find(&adminMenus); err != nil {
			log.Printf("Warning: Failed to get admin menus: %v", err)
		} else {
			fmt.Printf("\nadmin 角色已有菜单数：%d\n", len(adminMenus))
			if len(adminMenus) > 0 {
				fmt.Println("admin 角色已有菜单:")
				for i, menu := range adminMenus {
					fmt.Printf("  %d. %s (%s)\n", i+1, menu.Title, menu.Name)
				}
			}
		}

		// 4. 为 admin 角色分配所有菜单
		fmt.Println("\n=== 为 admin 角色分配所有菜单 ===")
		if err := database.DB.Model(&adminRole).Association("Menus").Append(menus); err != nil {
			log.Printf("Error: Failed to assign menus: %v", err)
		} else {
			fmt.Println("✓ 成功为 admin 角色分配所有菜单")

			// 验证分配结果
			var updatedMenus []model.Menu
			if err := database.DB.Model(&adminRole).Association("Menus").Find(&updatedMenus); err == nil {
				fmt.Printf("✓ admin 角色现在有 %d 个菜单\n", len(updatedMenus))
			}
		}
	}

	// 5. 获取 super_admin 角色
	var superAdminRole model.Role
	if err := database.DB.Where("code = ?", "super_admin").First(&superAdminRole).Error; err != nil {
		log.Printf("Warning: super_admin role not found: %v", err)
	} else {
		fmt.Printf("\n=== super_admin 角色信息 ===\n")
		fmt.Printf("角色 ID: %s\n", superAdminRole.ID)
		fmt.Printf("角色名称：%s\n", superAdminRole.Name)
		fmt.Printf("角色代码：%s\n", superAdminRole.Code)

		// 获取 super_admin 角色的菜单
		var superAdminMenus []model.Menu
		if err := database.DB.Model(&superAdminRole).Association("Menus").Find(&superAdminMenus); err != nil {
			log.Printf("Warning: Failed to get super_admin menus: %v", err)
		} else {
			fmt.Printf("super_admin 角色已有菜单数：%d\n", len(superAdminMenus))
		}

		// 为 super_admin 角色分配所有菜单
		fmt.Println("\n=== 为 super_admin 角色分配所有菜单 ===")
		if err := database.DB.Model(&superAdminRole).Association("Menus").Append(menus); err != nil {
			log.Printf("Error: Failed to assign menus: %v", err)
		} else {
			fmt.Println("✓ 成功为 super_admin 角色分配所有菜单")

			// 验证分配结果
			var updatedMenus []model.Menu
			if err := database.DB.Model(&superAdminRole).Association("Menus").Find(&updatedMenus); err == nil {
				fmt.Printf("✓ super_admin 角色现在有 %d 个菜单\n", len(updatedMenus))
			}
		}
	}

	// 6. 获取所有用户并显示其角色
	fmt.Println("\n=== 用户角色分配情况 ===")
	var users []model.User
	if err := database.DB.Preload("Roles").Find(&users).Error; err != nil {
		log.Printf("Error: Failed to get users: %v", err)
	} else {
		for _, user := range users {
			fmt.Printf("用户：%s (%s)\n", user.Username, user.Email)
			if len(user.Roles) > 0 {
				fmt.Printf("  角色:")
				for _, role := range user.Roles {
					fmt.Printf(" - %s (%s)", role.Name, role.Code)
				}
				fmt.Println()
			} else {
				fmt.Println("  角色：无")
			}
		}
	}

}
