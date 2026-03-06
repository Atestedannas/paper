package service

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/casbin/casbin/v2"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/paper-format-checker/backend/internal/database"
)

// CasbinService Casbin 服务接口
type CasbinService interface {
	// 初始化 Casbin 引擎
	Init() error

	// 加载策略
	LoadPolicy() error

	// 保存策略
	SavePolicy() error

	// 添加策略
	AddPolicy(sub, obj, act string) error

	// 移除策略
	RemovePolicy(sub, obj, act string) error

	// 添加角色继承
	AddGroupingPolicy(user, role string) error

	// 移除角色继承
	RemoveGroupingPolicy(user, role string) error

	// 检查权限
	Enforce(sub, obj, act string) (bool, error)

	// 获取用户所有权限
	GetImplicitPermissionsForUser(user string) ([][]string, error)

	// 获取用户所有角色
	GetImplicitRolesForUser(user string) ([]string, error)

	// 批量添加策略
	AddPolicies(rules [][]string) error

	// 批量移除策略
	RemovePolicies(rules [][]string) error

	// 获取所有策略
	GetPolicy() ([][]string, error)

	// 清除所有策略
	ClearPolicy() error

	// 获取 Enforcer 实例
	GetEnforcer() *casbin.Enforcer
}

// casbinService Casbin 服务实现
type casbinService struct {
	enforcer *casbin.Enforcer
	once     sync.Once
}

var (
	casbinServiceInstance *casbinService
	casbinOnce            sync.Once
)

// NewCasbinService 创建 Casbin 服务实例
func NewCasbinService() CasbinService {
	casbinOnce.Do(func() {
		casbinServiceInstance = &casbinService{}
	})
	return casbinServiceInstance
}

// GetEnforcer 获取 Enforcer 实例
func (s *casbinService) GetEnforcer() *casbin.Enforcer {
	return s.enforcer
}

// Init 初始化 Casbin 引擎
func (s *casbinService) Init() error {
	var err error
	s.once.Do(func() {
		// 创建 GORM 适配器
		adapter, err := gormadapter.NewAdapterByDB(database.DB)
		if err != nil {
			fmt.Printf("创建 Casbin 适配器失败：%v\n", err)
			err = fmt.Errorf("创建 Casbin 适配器失败：%w", err)
			return
		}

		// 获取配置文件路径
		configPath := filepath.Join("config", "casbin_model.conf")

		// 创建 Enforcer (使用 NewEnforcerWithAdapter 避免版本兼容性问题)
		enforcer, err := casbin.NewEnforcer(configPath)
		if err != nil {
			fmt.Printf("创建 Casbin Enforcer 失败：%v\n", err)
			err = fmt.Errorf("创建 Casbin Enforcer 失败：%w", err)
			return
		}

		// 设置适配器
		enforcer.SetAdapter(adapter)

		// 加载策略
		if err := enforcer.LoadPolicy(); err != nil {
			fmt.Printf("加载 Casbin 策略失败：%v\n", err)
			err = fmt.Errorf("加载 Casbin 策略失败：%w", err)
			return
		}

		s.enforcer = enforcer
		fmt.Println("Casbin 初始化成功")
	})

	return err
}

// LoadPolicy 加载策略
func (s *casbinService) LoadPolicy() error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}
	return s.enforcer.LoadPolicy()
}

// SavePolicy 保存策略
func (s *casbinService) SavePolicy() error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}
	return s.enforcer.SavePolicy()
}

// AddPolicy 添加策略
func (s *casbinService) AddPolicy(sub, obj, act string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.AddPolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("添加策略失败：%w", err)
	}

	return nil
}

// RemovePolicy 移除策略
func (s *casbinService) RemovePolicy(sub, obj, act string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.RemovePolicy(sub, obj, act)
	if err != nil {
		return fmt.Errorf("移除策略失败：%w", err)
	}

	return nil
}

// AddGroupingPolicy 添加角色继承
func (s *casbinService) AddGroupingPolicy(user, role string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.AddGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("添加角色继承失败：%w", err)
	}

	return nil
}

// RemoveGroupingPolicy 移除角色继承
func (s *casbinService) RemoveGroupingPolicy(user, role string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.RemoveGroupingPolicy(user, role)
	if err != nil {
		return fmt.Errorf("移除角色继承失败：%w", err)
	}

	return nil
}

// Enforce 检查权限
func (s *casbinService) Enforce(sub, obj, act string) (bool, error) {
	if s.enforcer == nil {
		return false, fmt.Errorf("Casbin Enforcer 未初始化")
	}

	return s.enforcer.Enforce(sub, obj, act)
}

// GetImplicitPermissionsForUser 获取用户所有权限
func (s *casbinService) GetImplicitPermissionsForUser(user string) ([][]string, error) {
	if s.enforcer == nil {
		return nil, fmt.Errorf("Casbin Enforcer 未初始化")
	}

	permissions, err := s.enforcer.GetImplicitPermissionsForUser(user)
	if err != nil {
		return nil, err
	}
	return permissions, nil
}

// GetImplicitRolesForUser 获取用户所有角色
func (s *casbinService) GetImplicitRolesForUser(user string) ([]string, error) {
	if s.enforcer == nil {
		return nil, fmt.Errorf("Casbin Enforcer 未初始化")
	}

	roles, err := s.enforcer.GetImplicitRolesForUser(user)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AddPolicies 批量添加策略
func (s *casbinService) AddPolicies(rules [][]string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.AddPolicies(rules)
	if err != nil {
		return fmt.Errorf("批量添加策略失败：%w", err)
	}

	return nil
}

// RemovePolicies 批量移除策略
func (s *casbinService) RemovePolicies(rules [][]string) error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	_, err := s.enforcer.RemovePolicies(rules)
	if err != nil {
		return fmt.Errorf("批量移除策略失败：%w", err)
	}

	return nil
}

// GetPolicy 获取所有策略
func (s *casbinService) GetPolicy() ([][]string, error) {
	if s.enforcer == nil {
		return nil, fmt.Errorf("Casbin Enforcer 未初始化")
	}

	policy, err := s.enforcer.GetPolicy()
	if err != nil {
		return nil, err
	}
	return policy, nil
}

// ClearPolicy 清除所有策略
func (s *casbinService) ClearPolicy() error {
	if s.enforcer == nil {
		return fmt.Errorf("Casbin Enforcer 未初始化")
	}

	s.enforcer.ClearPolicy()
	return nil
}
