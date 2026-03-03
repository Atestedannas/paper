package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig(".env")
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}
	// 初始化数据库
	if err := database.InitDatabase(cfg); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}

	// 查询重庆工程学院的格式模板
	var template model.FormatTemplate
	if err := database.DB.Joins("JOIN universities ON format_templates.university_id = universities.id").Where("universities.name LIKE ?", "%重庆%").First(&template).Error; err != nil {
		log.Fatalf("查询模板失败: %v", err)
	}

	fmt.Println("========================================")
	fmt.Printf("模板名称: %s\n", template.Name)
	fmt.Printf("模板ID: %s\n", template.ID)
	fmt.Println("========================================")

	// 解析格式规则
	var rulesMap map[string]interface{}
	if err := json.Unmarshal([]byte(template.FormatRules), &rulesMap); err != nil {
		var jsonString string
		if err2 := json.Unmarshal([]byte(template.FormatRules), &jsonString); err2 == nil {
			_ = json.Unmarshal([]byte(jsonString), &rulesMap)
		}
	}

	if rulesMap == nil {
		log.Fatal("无法解析格式规则")
	}

	// 美化输出
	prettyJSON, _ := json.MarshalIndent(rulesMap, "", "  ")
	fmt.Println("\n原始格式规则:")
	fmt.Println(string(prettyJSON))

	// 检查关键字段
	fmt.Println("\n========================================")
	fmt.Println("关键字段检查:")
	fmt.Println("========================================")

	// 检查正文格式
	if body, ok := rulesMap["正文格式"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 '正文格式':")
		bodyJSON, _ := json.MarshalIndent(body, "  ", "  ")
		fmt.Println(string(bodyJSON))
	} else if body, ok := rulesMap["body"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 'body':")
		bodyJSON, _ := json.MarshalIndent(body, "  ", "  ")
		fmt.Println(string(bodyJSON))
	} else {
		fmt.Println("\n❌ 未找到正文格式")
	}

	// 检查标题层级格式
	if headings, ok := rulesMap["标题层级格式"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 '标题层级格式':")
		headingsJSON, _ := json.MarshalIndent(headings, "  ", "  ")
		fmt.Println(string(headingsJSON))
	} else if headings, ok := rulesMap["headings"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 'headings':")
		headingsJSON, _ := json.MarshalIndent(headings, "  ", "  ")
		fmt.Println(string(headingsJSON))
	} else {
		fmt.Println("\n❌ 未找到标题层级格式")
	}

	// 检查摘要格式
	if abstract, ok := rulesMap["摘要格式"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 '摘要格式':")
		abstractJSON, _ := json.MarshalIndent(abstract, "  ", "  ")
		fmt.Println(string(abstractJSON))
	} else if abstract, ok := rulesMap["abstract"].(map[string]interface{}); ok {
		fmt.Println("\n✅ 找到 'abstract':")
		abstractJSON, _ := json.MarshalIndent(abstract, "  ", "  ")
		fmt.Println(string(abstractJSON))
	} else {
		fmt.Println("\n❌ 未找到摘要格式")
	}

	fmt.Println("\n========================================")
	fmt.Println("所有顶层键:")
	fmt.Println("========================================")
	for key := range rulesMap {
		fmt.Printf("  - %s\n", key)
	}
}
