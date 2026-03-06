package main

import (
	"fmt"
	"log"

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

	fmt.Println("Database connected successfully\n")

	// 获取 admin 角色
	var adminRole model.Role
	if err := database.DB.Where("code = ?", "admin").First(&adminRole).Error; err != nil {
		log.Fatalf("admin role not found: %v", err)
	}

	// 获取 admin 角色的菜单
	var adminMenus []model.Menu
	if err := database.DB.Model(&adminRole).Association("Menus").Find(&adminMenus); err != nil {
		log.Fatalf("Failed to get admin menus: %v", err)
	}

	fmt.Println("=== admin 角色的菜单组件路径 ===")
	for i, menu := range adminMenus {
		// 前端期望的路径格式
		expectedPath := fmt.Sprintf("../../views/%s", menu.Component)
		fmt.Printf("%d. %s\n", i+1, expectedPath)
		fmt.Printf("   数据库组件值：%s\n", menu.Component)
		fmt.Printf("   前端期望路径：%s\n", expectedPath)
	}
}
