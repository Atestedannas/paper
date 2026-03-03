package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type DataPermissionService interface {
	GetUserDataScope(userID uuid.UUID) (string, error)
	SetUserDataScope(userID uuid.UUID, scope string) error
	GetUserDataRule(userID uuid.UUID) (*model.DataPermissionRule, error)
	GetUserDataFilter(userID uuid.UUID, resourceType string) (*DataFilter, error)
	FilterQuery(userID uuid.UUID, resourceType string, query *gorm.DB) *gorm.DB
	GetUserFieldPermissions(userID uuid.UUID, resourceType string) ([]model.ColumnPermission, error)
	ApplyFieldMasking(userID uuid.UUID, resourceType string, data map[string]interface{}) map[string]interface{}
	CreateDataRule(rule *model.DataPermissionRule) (*model.DataPermissionRule, error)
	UpdateDataRule(id uuid.UUID, updates map[string]interface{}) error
	DeleteDataRule(id uuid.UUID) error
	GetDataRules(resourceType string) ([]model.DataPermissionRule, error)
	GetDataRuleByID(id uuid.UUID) (*model.DataPermissionRule, error)
}

type dataPermissionService struct{}

type DataFilter struct {
	TableAlias  string
	WhereClause string
	JoinClauses []string
	Parameters  map[string]interface{}
}

func NewDataPermissionService() DataPermissionService {
	return &dataPermissionService{}
}

func (s *dataPermissionService) GetUserDataScope(userID uuid.UUID) (string, error) {
	var dataPermission model.DataPermission
	err := database.DB.Where("user_id = ?", userID).First(&dataPermission).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DataScopeSelf, nil
		}
		return "", err
	}
	return dataPermission.DataScope, nil
}

func (s *dataPermissionService) SetUserDataScope(userID uuid.UUID, scope string) error {
	validScopes := []string{model.DataScopeSelf, model.DataScopeDepartment, model.DataScopeAll, model.DataScopeCustom}
	isValid := false
	for _, valid := range validScopes {
		if scope == valid {
			isValid = true
			break
		}
	}
	if !isValid {
		return errors.New("无效的数据范围类型")
	}

	return database.DB.Transaction(func(tx *gorm.DB) error {
		var dataPermission model.DataPermission
		err := tx.Where("user_id = ?", userID).First(&dataPermission).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("请先为用户分配角色")
			}
			return err
		}

		dataPermission.DataScope = scope
		return tx.Save(&dataPermission).Error
	})
}

func (s *dataPermissionService) GetUserDataRule(userID uuid.UUID) (*model.DataPermissionRule, error) {
	var dataPermission model.DataPermission
	err := database.DB.Where("user_id = ?", userID).First(&dataPermission).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if dataPermission.DataRuleID == nil {
		return nil, nil
	}

	var rule model.DataPermissionRule
	if err := database.DB.First(&rule, *dataPermission.DataRuleID).Error; err != nil {
		return nil, err
	}

	return &rule, nil
}

func (s *dataPermissionService) GetUserDataFilter(userID uuid.UUID, resourceType string) (*DataFilter, error) {
	filter := &DataFilter{
		TableAlias: "",
		Parameters: make(map[string]interface{}),
	}

	dataPermission, err := s.getEffectiveDataPermission(userID)
	if err != nil {
		return nil, err
	}

	if dataPermission == nil {
		filter.WhereClause = "user_id = ?"
		filter.Parameters["user_id"] = userID
		return filter, nil
	}

	switch dataPermission.DataScope {
	case model.DataScopeSelf:
		filter.WhereClause = "user_id = ?"
		filter.Parameters["user_id"] = userID

	case model.DataScopeAll:
		filter.WhereClause = ""
		filter.Parameters = make(map[string]interface{})

	case model.DataScopeDepartment:
		filter.WhereClause = "department_id = ?"
		filter.Parameters["department_id"] = userID

	case model.DataScopeCustom:
		customFilter, err := dataPermission.GetCustomFilter()
		if err != nil {
			return nil, err
		}
		if customFilter != nil && len(customFilter) > 0 {
			filter.WhereClause = customFilter["where"].(string)
			for k, v := range customFilter["params"].(map[string]interface{}) {
				filter.Parameters[k] = v
			}
		}
	}

	return filter, nil
}

func (s *dataPermissionService) FilterQuery(userID uuid.UUID, resourceType string, query *gorm.DB) *gorm.DB {
	filter, err := s.GetUserDataFilter(userID, resourceType)
	if err != nil {
		return query
	}

	if filter.WhereClause != "" {
		query = query.Where(filter.WhereClause, filter.Parameters)
	}

	for _, join := range filter.JoinClauses {
		query = query.Joins(join)
	}

	return query
}

func (s *dataPermissionService) GetUserFieldPermissions(userID uuid.UUID, resourceType string) ([]model.ColumnPermission, error) {
	dataPermission, err := s.getEffectiveDataPermission(userID)
	if err != nil {
		return nil, err
	}

	if dataPermission == nil {
		return []model.ColumnPermission{}, nil
	}

	return dataPermission.GetFieldPermissions()
}

func (s *dataPermissionService) ApplyFieldMasking(userID uuid.UUID, resourceType string, data map[string]interface{}) map[string]interface{} {
	permissions, err := s.GetUserFieldPermissions(userID, resourceType)
	if err != nil {
		return data
	}

	for _, perm := range permissions {
		if perm.Hidden && perm.Masked {
			if value, exists := data[perm.Column]; exists && value != nil {
				data[perm.Column] = s.maskValue(value.(string), perm.MaskType)
			}
		}
	}

	return data
}

func (s *dataPermissionService) maskValue(value string, maskType string) string {
	if value == "" || len(value) < 4 {
		return "****"
	}

	switch maskType {
	case "phone":
		if len(value) >= 11 {
			return value[:3] + "****" + value[len(value)-4:]
		}
	case "email":
		parts := splitEmail(value)
		if len(parts) >= 2 {
			return parts[0][:1] + "***@" + parts[1]
		}
	case "id_card":
		if len(value) >= 18 {
			return value[:6] + "*********" + value[len(value)-4:]
		}
	}

	return "****"
}

func splitEmail(email string) []string {
	atIndex := -1
	for i, c := range email {
		if c == '@' {
			atIndex = i
			break
		}
	}
	if atIndex == -1 {
		return []string{email}
	}
	return []string{email[:atIndex], email[atIndex+1:]}
}

func (s *dataPermissionService) getEffectiveDataPermission(userID uuid.UUID) (*model.DataPermission, error) {
	var dataPermission model.DataPermission
	err := database.DB.Preload("Rule").Where("user_id = ?", userID).First(&dataPermission).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &dataPermission, nil
}

func (s *dataPermissionService) CreateDataRule(rule *model.DataPermissionRule) (*model.DataPermissionRule, error) {
	var existing model.DataPermissionRule
	err := database.DB.Where("code = ?", rule.Name).First(&existing).Error
	if err == nil {
		return nil, errors.New("规则名称已存在")
	}

	if err := database.DB.Create(rule).Error; err != nil {
		return nil, err
	}

	return rule, nil
}

func (s *dataPermissionService) UpdateDataRule(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.DataPermissionRule{}).Where("id = ?", id).Updates(updates).Error
}

func (s *dataPermissionService) DeleteDataRule(id uuid.UUID) error {
	return database.DB.Delete(&model.DataPermissionRule{}, id).Error
}

func (s *dataPermissionService) GetDataRules(resourceType string) ([]model.DataPermissionRule, error) {
	var rules []model.DataPermissionRule
	err := database.DB.Where("resource_type = ?", resourceType).Find(&rules).Error
	return rules, err
}

func (s *dataPermissionService) GetDataRuleByID(id uuid.UUID) (*model.DataPermissionRule, error) {
	var rule model.DataPermissionRule
	err := database.DB.First(&rule, id).Error
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

type DataFilterContext struct {
	UserID       uuid.UUID
	ResourceType string
	TableName    string
	TableAlias   string
}

func BuildDataFilterQuery(ctx *DataFilterContext) (string, []interface{}, error) {
	service := NewDataPermissionService()
	filter, err := service.GetUserDataFilter(ctx.UserID, ctx.ResourceType)
	if err != nil {
		return "", nil, err
	}

	if filter.WhereClause == "" {
		return "", []interface{}{}, nil
	}

	alias := ctx.TableAlias
	if alias != "" && alias != ctx.TableName {
		filter.WhereClause = replaceTableAliases(filter.WhereClause, ctx.TableName, alias)
	}

	params := make([]interface{}, 0, len(filter.Parameters))
	for _, v := range filter.Parameters {
		params = append(params, v)
	}

	return filter.WhereClause, params, nil
}

func replaceTableAliases(sql, tableName, alias string) string {
	placeholder := fmt.Sprintf("%s.", tableName)
	aliasPlaceholder := fmt.Sprintf("%s.", alias)
	return strings.ReplaceAll(sql, placeholder, aliasPlaceholder)
}
