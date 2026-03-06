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

	fmt.Println("Database connected successfully\n")

	// 获取所有菜单
	var menus []model.Menu
	if err := database.DB.Find(&menus).Error; err != nil {
		log.Fatalf("Failed to get menus: %v", err)
	}

	fmt.Println("=== 数据库中的菜单数据 ===")
	for i, menu := range menus {
		fmt.Printf("\n%d. %s (%s)\n", i+1, menu.Title, menu.Name)
		fmt.Printf("   路径：%s\n", menu.Path)
		fmt.Printf("   组件：%s\n", menu.Component)
		fmt.Printf("   类型：%s\n", menu.MenuType)
		fmt.Printf("   可见：%v\n", menu.Visible)
	}

	// 获取 admin 角色
	var adminRole model.Role
	if err := database.DB.Where("code = ?", "admin").First(&adminRole).Error; err != nil {
		log.Printf("Warning: admin role not found: %v", err)
	} else {
		fmt.Println("\n\n=== admin 角色的菜单 ===")
		var adminMenus []model.Menu
		if err := database.DB.Model(&adminRole).Association("Menus").Find(&adminMenus); err != nil {
			log.Printf("Warning: Failed to get admin menus: %v", err)
		} else {
			fmt.Printf("admin 角色有 %d 个菜单:\n", len(adminMenus))
			for i, menu := range adminMenus {
				fmt.Printf("  %d. %s - %s (组件：%s)\n", i+1, menu.Title, menu.Path, menu.Component)
			}
		}
	}
}
