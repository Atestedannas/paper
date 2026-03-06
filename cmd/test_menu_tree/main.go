package main

import (
	"fmt"
	"log"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/service"
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

	// 测试 GetMenuTree
	menuService := service.NewMenuService()
	tree, err := menuService.GetMenuTree()
	if err != nil {
		log.Fatalf("Failed to get menu tree: %v", err)
	}

	fmt.Println("=== GetMenuTree 返回结果 ===")
	for i, node := range tree {
		fmt.Printf("\n%d. %s (%s)\n", i+1, node.Title, node.Name)
		fmt.Printf("   路径：%s\n", node.Path)
		fmt.Printf("   组件：%s\n", node.Component)
		fmt.Printf("   类型：%s\n", node.MenuType)
		fmt.Printf("   图标：%s\n", node.Icon)
		fmt.Printf("   子节点数：%d\n", len(node.Children))
	}
}
