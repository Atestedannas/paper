package service

import (
	"encoding/json"
	"fmt"

	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// BillingService 计费服务接口
type BillingService interface {
	// 服务定价管理
	GetAllServicePricings() ([]model.ServicePricing, error)
	GetServicePricingByType(serviceType string) (*model.ServicePricing, error)
	CreateServicePricing(pricing *model.ServicePricing) error
	UpdateServicePricing(id string, updates map[string]interface{}) error
	DeleteServicePricing(id string) error
	ToggleServicePricing(id string, enabled bool) error
	SetServiceFree(id string, isFree bool) error

	// 套餐管理
	GetAllPlans() ([]model.PricingPlan, error)
	GetPlansByServiceType(serviceType string) ([]model.PricingPlan, error)
	CreatePlan(plan *model.PricingPlan) error
	UpdatePlan(id string, updates map[string]interface{}) error
	DeletePlan(id string) error

	// 快捷方法
	GetServiceConfig() (map[string]interface{}, error)
	IsServiceFree(serviceType string) (bool, error)
	GetServicePrice(serviceType string) (float64, error)
}

// billingService 计费服务实现
type billingService struct{}

// NewBillingService 创建计费服务实例
func NewBillingService() BillingService {
	return &billingService{}
}

// GetAllServicePricings 获取所有服务定价
func (s *billingService) GetAllServicePricings() ([]model.ServicePricing, error) {
	var pricings []model.ServicePricing
	err := database.DB.Order("sort_order ASC").Find(&pricings).Error
	return pricings, err
}

// GetServicePricingByType 根据服务类型获取定价
func (s *billingService) GetServicePricingByType(serviceType string) (*model.ServicePricing, error) {
	var pricing model.ServicePricing
	err := database.DB.Where("service_type = ?", serviceType).First(&pricing).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &pricing, nil
}

// CreateServicePricing 创建服务定价
func (s *billingService) CreateServicePricing(pricing *model.ServicePricing) error {
	if pricing.Currency == "" {
		pricing.Currency = "CNY"
	}
	return database.DB.Create(pricing).Error
}

// UpdateServicePricing 更新服务定价
func (s *billingService) UpdateServicePricing(id string, updates map[string]interface{}) error {
	return database.DB.Model(&model.ServicePricing{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteServicePricing 删除服务定价
func (s *billingService) DeleteServicePricing(id string) error {
	return database.DB.Delete(&model.ServicePricing{}, "id = ?", id).Error
}

// ToggleServicePricing 启用/禁用服务
func (s *billingService) ToggleServicePricing(id string, enabled bool) error {
	return database.DB.Model(&model.ServicePricing{}).Where("id = ?", id).Update("is_enabled", enabled).Error
}

// SetServiceFree 设置服务是否免费
func (s *billingService) SetServiceFree(id string, isFree bool) error {
	return database.DB.Model(&model.ServicePricing{}).Where("id = ?", id).Update("is_free", isFree).Error
}

// GetAllPlans 获取所有套餐
func (s *billingService) GetAllPlans() ([]model.PricingPlan, error) {
	var plans []model.PricingPlan
	err := database.DB.Where("is_active = ?", true).Order("sort_order ASC").Find(&plans).Error
	return plans, err
}

// GetPlansByServiceType 根据服务类型获取套餐
func (s *billingService) GetPlansByServiceType(serviceType string) ([]model.PricingPlan, error) {
	var plans []model.PricingPlan
	err := database.DB.Where("service_type = ? AND is_active = ?", serviceType, true).Order("sort_order ASC").Find(&plans).Error
	return plans, err
}

// CreatePlan 创建套餐
func (s *billingService) CreatePlan(plan *model.PricingPlan) error {
	if plan.Currency == "" {
		plan.Currency = "CNY"
	}
	return database.DB.Create(plan).Error
}

// UpdatePlan 更新套餐
func (s *billingService) UpdatePlan(id string, updates map[string]interface{}) error {
	return database.DB.Model(&model.PricingPlan{}).Where("id = ?", id).Updates(updates).Error
}

// DeletePlan 删除套餐
func (s *billingService) DeletePlan(id string) error {
	return database.DB.Delete(&model.PricingPlan{}, "id = ?", id).Error
}

// GetServiceConfig 获取完整的服务配置（用于前端显示）
func (s *billingService) GetServiceConfig() (map[string]interface{}, error) {
	pricings, err := s.GetAllServicePricings()
	if err != nil {
		return nil, err
	}

	plans, err := s.GetAllPlans()
	if err != nil {
		return nil, err
	}

	// 转换为 JSON 再解析回来，确保格式一致
	pricingJSON, _ := json.Marshal(pricings)
	var pricingList []map[string]interface{}
	json.Unmarshal(pricingJSON, &pricingList)

	planJSON, _ := json.Marshal(plans)
	var planList []map[string]interface{}
	json.Unmarshal(planJSON, &planList)

	// 按服务类型分组
	servicesMap := make(map[string]interface{})
	for _, p := range pricingList {
		serviceType := p["service_type"].(string)
		servicesMap[serviceType] = p
	}

	// 按服务类型分组套餐
	plansMap := make(map[string][]map[string]interface{})
	for _, p := range planList {
		serviceType := p["service_type"].(string)
		plansMap[serviceType] = append(plansMap[serviceType], p)
	}

	config := map[string]interface{}{
		"services": servicesMap,
		"plans":    plansMap,
	}

	return config, nil
}

// IsServiceFree 检查服务是否免费
func (s *billingService) IsServiceFree(serviceType string) (bool, error) {
	pricing, err := s.GetServicePricingByType(serviceType)
	if err != nil {
		return false, err
	}
	if pricing == nil {
		// 默认返回 true（未配置的服务视为免费）
		return true, nil
	}
	return pricing.IsFree, nil
}

// GetServicePrice 获取服务价格
func (s *billingService) GetServicePrice(serviceType string) (float64, error) {
	pricing, err := s.GetServicePricingByType(serviceType)
	if err != nil {
		return 0, err
	}
	if pricing == nil {
		return 0, nil
	}
	return pricing.Price, nil
}

// GetBillingService 获取全局计费服务实例
var billingServiceInstance BillingService

func GetBillingService() BillingService {
	if billingServiceInstance == nil {
		billingServiceInstance = NewBillingService()
	}
	return billingServiceInstance
}

// InitDefaultServices 初始化默认服务配置
func InitDefaultServices() error {
	defaultServices := []model.ServicePricing{
		{
			ServiceType:  "paper_check",
			ServiceName:  "论文格式检查",
			PricingModel: model.PricingModelCount,
			IsEnabled:    true,
			IsFree:       true,
			Price:        0,
			FreeCount:    5,
			Description:  "对上传的论文进行格式检查，生成检查报告",
			SortOrder:    1,
		},
		{
			ServiceType:  "paper_download",
			ServiceName:  "论文下载",
			PricingModel: model.PricingModelCount,
			IsEnabled:    true,
			IsFree:       false,
			Price:        1.00,
			Description:  "下载检查后的论文文件",
			SortOrder:    2,
		},
		{
			ServiceType:  "paper_fix",
			ServiceName:  "格式修复",
			PricingModel: model.PricingModelCount,
			IsEnabled:    true,
			IsFree:       false,
			Price:        2.00,
			Description:  "自动修复论文格式问题",
			SortOrder:    3,
		},
		{
			ServiceType:  "template_download",
			ServiceName:  "模板下载",
			PricingModel: model.PricingModelMonth,
			IsEnabled:    true,
			IsFree:       false,
			Price:        9.90,
			Description:  "下载格式模板文件",
			SortOrder:    4,
		},
	}

	for _, service := range defaultServices {
		var existing model.ServicePricing
		err := database.DB.Where("service_type = ?", service.ServiceType).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			// 不存在则创建
			if err := database.DB.Create(&service).Error; err != nil {
				fmt.Printf("创建默认服务 %s 失败: %v\n", service.ServiceName, err)
			}
		}
	}

	return nil
}
