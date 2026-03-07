package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PermissionPackage 权限包（权限组）模型
type PermissionPackage struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name        string    `gorm:"size:100;not null" json:"name"`             // 权限包名称
	Code        string    `gorm:"size:100;uniqueIndex;not null" json:"code"` // 权限包代码
	Description string    `gorm:"size:200" json:"description"`               // 权限包描述

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Permissions []Permission `gorm:"many2many:permission_package_permissions;" json:"permissions,omitempty"`
}

// TableName 指定表名
func (PermissionPackage) TableName() string {
	return "permission_packages"
}

// PermissionPackagePermission 权限包-权限关联表
type PermissionPackagePermission struct {
	PackageID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	PermissionID uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// TableName 指定表名
func (PermissionPackagePermission) TableName() string {
	return "permission_package_permissions"
}

// DataPermissionRule 数据权限规则模型
type DataPermissionRule struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name         string    `gorm:"size:100;not null" json:"name"`         // 规则名称
	ResourceType string    `gorm:"size:50;not null" json:"resource_type"` // 资源类型：paper/order/user/university
	RuleType     string    `gorm:"size:20;not null" json:"rule_type"`     // 规则类型：self/department/all/custom
	ColumnFilter string    `gorm:"type:jsonb" json:"column_filter"`       // 列级权限JSON配置
	FilterSQL    string    `gorm:"size:500" json:"filter_sql"`            // 自定义过滤SQL（安全转义后）
	Description  string    `gorm:"size:200" json:"description"`           // 规则描述

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	Role   *Role     `gorm:"foreignKey:RoleID" json:"role,omitempty"`
	RoleID uuid.UUID `gorm:"type:uuid;index" json:"role_id"`
}

// TableName 指定表名
func (DataPermissionRule) TableName() string {
	return "data_permission_rules"
}

// DataScope 数据范围类型常量
const (
	DataScopeSelf       = "self"       // 仅本人数据
	DataScopeDepartment = "department" // 部门数据
	DataScopeAll        = "all"        // 全部数据
	DataScopeCustom     = "custom"     // 自定义规则
)

// DataPermissionType 数据权限规则类型常量
const (
	DataRuleTypeSelf       = "self"
	DataRuleTypeDepartment = "department"
	DataRuleTypeAll        = "all"
	DataRuleTypeCustom     = "custom"
)

// ColumnPermission 列级权限配置
type ColumnPermission struct {
	Column   string `json:"column"`    // 列名
	Readable bool   `json:"readable"`  // 是否可读
	Writable bool   `json:"writable"`  // 是否可写
	Hidden   bool   `json:"hidden"`    // 是否隐藏
	Masked   bool   `json:"masked"`    // 是否脱敏显示
	MaskType string `json:"mask_type"` // 脱敏类型：phone/email/id_card
}

// GetColumnFilterJSON 解析列级权限配置JSON
func (d *DataPermissionRule) GetColumnFilterJSON() ([]ColumnPermission, error) {
	if d.ColumnFilter == "" {
		return []ColumnPermission{}, nil
	}
	var columns []ColumnPermission
	err := json.Unmarshal([]byte(d.ColumnFilter), &columns)
	return columns, err
}

// SetColumnFilterJSON 设置列级权限配置JSON
func (d *DataPermissionRule) SetColumnFilterJSON(columns []ColumnPermission) error {
	data, err := json.Marshal(columns)
	if err != nil {
		return err
	}
	d.ColumnFilter = string(data)
	return nil
}

// DataPermission 用户数据权限配置（增强版用户角色关联）
type DataPermission struct {
	ID               uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID           uuid.UUID  `gorm:"type:uuid;not null;index:idx_user_role" json:"user_id"`
	RoleID           uuid.UUID  `gorm:"type:uuid;not null;index:idx_user_role" json:"role_id"`
	DataScope        string     `gorm:"size:20;default:'self'" json:"data_scope"` // 数据范围：self/department/all
	DataRuleID       *uuid.UUID `gorm:"type:uuid;index" json:"data_rule_id"`      // 自定义数据权限规则ID
	CustomFilter     string     `gorm:"type:jsonb" json:"custom_filter"`          // 自定义过滤条件JSON
	FieldPermissions string     `gorm:"type:jsonb" json:"field_permissions"`      // 字段级权限配置JSON

	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// 关联关系
	User *User               `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Role *Role               `gorm:"foreignKey:RoleID" json:"role,omitempty"`
	Rule *DataPermissionRule `gorm:"foreignKey:DataRuleID" json:"rule,omitempty"`
}

// TableName 指定表名
func (DataPermission) TableName() string {
	return "data_permissions"
}

// GetCustomFilter 解析自定义过滤条件
func (d *DataPermission) GetCustomFilter() (map[string]interface{}, error) {
	if d.CustomFilter == "" {
		return map[string]interface{}{}, nil
	}
	var filter map[string]interface{}
	err := json.Unmarshal([]byte(d.CustomFilter), &filter)
	return filter, err
}

// SetCustomFilter 设置自定义过滤条件
func (d *DataPermission) SetCustomFilter(filter map[string]interface{}) error {
	data, err := json.Marshal(filter)
	if err != nil {
		return err
	}
	d.CustomFilter = string(data)
	return nil
}

// GetFieldPermissions 解析字段权限配置
func (d *DataPermission) GetFieldPermissions() ([]ColumnPermission, error) {
	if d.FieldPermissions == "" {
		return []ColumnPermission{}, nil
	}
	var perms []ColumnPermission
	err := json.Unmarshal([]byte(d.FieldPermissions), &perms)
	return perms, err
}

// SetFieldPermissions 设置字段权限配置
func (d *DataPermission) SetFieldPermissions(perms []ColumnPermission) error {
	data, err := json.Marshal(perms)
	if err != nil {
		return err
	}
	d.FieldPermissions = string(data)
	return nil
}

const (
	ACLResourceTypeOrder  = "order"
	ACLResourceTypePaper  = "paper"
	ACLResourceTypeUser   = "user"
	ACLResourceTypeReport = "report"
)

const (
	ACLAccessLevelRead  = "read"
	ACLAccessLevelWrite = "write"
	ACLAccessLevelAdmin = "admin"
)

type ResourceACL struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ResourceType string    `gorm:"size:50;not null;index:idx_resource_type_id" json:"resource_type"`
	ResourceID   uuid.UUID `gorm:"type:uuid;not null;index:idx_resource_type_id" json:"resource_id"`
	OwnerID      uuid.UUID `gorm:"type:uuid;not null;index" json:"owner_id"`
	CreatorID    uuid.UUID `gorm:"type:uuid" json:"creator_id"`
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

func (ResourceACL) TableName() string {
	return "resource_acls"
}

type ACLUser struct {
	ACLID       uuid.UUID `gorm:"type:uuid;primaryKey" json:"acl_id"`
	UserID      uuid.UUID `gorm:"type:uuid;primaryKey;index" json:"user_id"`
	AccessLevel string    `gorm:"size:20;default:'read'" json:"access_level"`
	CreatedAt   time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

func (ACLUser) TableName() string {
	return "resource_acl_users"
}

type ACLWithUsers struct {
	ResourceACL
	Users []ACLUser `gorm:"foreignKey:ACLID" json:"users"`
}

type ACLUserInput struct {
	UserID      uuid.UUID `json:"user_id"`
	AccessLevel string    `json:"access_level"`
}
