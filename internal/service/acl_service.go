package service

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type ACLService interface {
	GrantAccess(resourceType string, resourceID uuid.UUID, ownerID uuid.UUID, authorizedUsers []ACLUserInput, creatorID uuid.UUID) error
	RevokeAccess(resourceType string, resourceID uuid.UUID, userID uuid.UUID) error
	CanAccess(userID uuid.UUID, resourceType string, resourceID uuid.UUID, requiredLevel string) (bool, error)
	GetAccessibleResources(userID uuid.UUID, resourceType string) ([]uuid.UUID, error)
	GetResourceACL(resourceType string, resourceID uuid.UUID) (*model.ResourceACL, error)
	GetACLWithUsers(resourceType string, resourceID uuid.UUID) (*model.ACLWithUsers, error)
	GetUserACLs(userID uuid.UUID, resourceType string) ([]model.ACLWithUsers, error)
	DeleteResourceACL(resourceType string, resourceID uuid.UUID) error
}

type ACLUserInput struct {
	UserID      uuid.UUID `json:"user_id"`
	AccessLevel string    `json:"access_level"`
}

type aclService struct{}

func NewACLService() ACLService {
	return &aclService{}
}

func (s *aclService) GrantAccess(resourceType string, resourceID uuid.UUID, ownerID uuid.UUID, authorizedUsers []ACLUserInput, creatorID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var acl model.ResourceACL
		err := tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				acl = model.ResourceACL{
					ID:           uuid.New(),
					ResourceType: resourceType,
					ResourceID:   resourceID,
					OwnerID:      ownerID,
					CreatorID:    creatorID,
				}
				if err := tx.Create(&acl).Error; err != nil {
					return fmt.Errorf("创建ACL失败: %w", err)
				}
			} else {
				return fmt.Errorf("查询ACL失败: %w", err)
			}
		}

		for _, user := range authorizedUsers {
			var existingUser model.ACLUser
			err := tx.Where("acl_id = ? AND user_id = ?", acl.ID, user.UserID).First(&existingUser).Error

			if errors.Is(err, gorm.ErrRecordNotFound) {
				aclUser := model.ACLUser{
					ACLID:       acl.ID,
					UserID:      user.UserID,
					AccessLevel: user.AccessLevel,
				}
				if err := tx.Create(&aclUser).Error; err != nil {
					return fmt.Errorf("添加授权用户失败: %w", err)
				}
			} else if err != nil {
				return fmt.Errorf("查询授权用户失败: %w", err)
			} else {
				if err := tx.Model(&existingUser).Update("access_level", user.AccessLevel).Error; err != nil {
					return fmt.Errorf("更新授权用户失败: %w", err)
				}
			}
		}

		return nil
	})
}

func (s *aclService) RevokeAccess(resourceType string, resourceID uuid.UUID, userID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var acl model.ResourceACL
		err := tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error
		if err != nil {
			return fmt.Errorf("ACL不存在: %w", err)
		}

		result := tx.Where("acl_id = ? AND user_id = ?", acl.ID, userID).Delete(&model.ACLUser{})
		if result.Error != nil {
			return fmt.Errorf("撤销授权失败: %w", result.Error)
		}

		var count int64
		tx.Model(&model.ACLUser{}).Where("acl_id = ?", acl.ID).Count(&count)
		if count == 0 {
			tx.Delete(&acl)
		}

		return nil
	})
}

func (s *aclService) CanAccess(userID uuid.UUID, resourceType string, resourceID uuid.UUID, requiredLevel string) (bool, error) {
	var acl model.ResourceACL
	err := database.DB.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查询ACL失败: %w", err)
	}

	if acl.OwnerID == userID {
		return true, nil
	}

	var aclUser model.ACLUser
	err = database.DB.Where("acl_id = ? AND user_id = ?", acl.ID, userID).First(&aclUser).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查询授权用户失败: %w", err)
	}

	return hasAccessLevel(aclUser.AccessLevel, requiredLevel), nil
}

func (s *aclService) GetAccessibleResources(userID uuid.UUID, resourceType string) ([]uuid.UUID, error) {
	var resourceIDs []uuid.UUID

	subQuery := database.DB.Model(&model.ACLUser{}).
		Select("resource_acls.resource_id").
		Joins("JOIN resource_acls ON resource_acls.id = resource_acl_users.acl_id").
		Where("resource_acl_users.user_id = ? AND resource_acls.resource_type = ?", userID, resourceType)

	query := database.DB.Model(&model.ResourceACL{}).
		Select("resource_id").
		Where("resource_type = ? AND owner_id = ?", resourceType, userID)

	rows, err := query.Unscoped().Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		resourceIDs = append(resourceIDs, id)
	}

	rows2, err := subQuery.Unscoped().Rows()
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var id uuid.UUID
		if err := rows2.Scan(&id); err != nil {
			return nil, err
		}
		exists := false
		for _, existing := range resourceIDs {
			if existing == id {
				exists = true
				break
			}
		}
		if !exists {
			resourceIDs = append(resourceIDs, id)
		}
	}

	return resourceIDs, nil
}

func (s *aclService) GetResourceACL(resourceType string, resourceID uuid.UUID) (*model.ResourceACL, error) {
	var acl model.ResourceACL
	err := database.DB.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询ACL失败: %w", err)
	}
	return &acl, nil
}

func (s *aclService) GetACLWithUsers(resourceType string, resourceID uuid.UUID) (*model.ACLWithUsers, error) {
	var acl model.ResourceACL
	err := database.DB.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询ACL失败: %w", err)
	}

	var users []model.ACLUser
	database.DB.Where("acl_id = ?", acl.ID).Find(&users)

	return &model.ACLWithUsers{
		ResourceACL: acl,
		Users:       users,
	}, nil
}

func (s *aclService) GetUserACLs(userID uuid.UUID, resourceType string) ([]model.ACLWithUsers, error) {
	var acls []model.ACLWithUsers

	subQuery := database.DB.Model(&model.ACLUser{}).
		Select("resource_acls.id").
		Joins("JOIN resource_acls ON resource_acls.id = resource_acl_users.acl_id").
		Where("resource_acl_users.user_id = ? AND resource_acls.resource_type = ?", userID, resourceType)

	var resources []model.ResourceACL
	err := database.DB.Where("resource_type = ? AND (owner_id = ? OR id IN (?))", resourceType, userID, subQuery).
		Find(&resources).Error
	if err != nil {
		return nil, fmt.Errorf("查询ACL列表失败: %w", err)
	}

	for _, r := range resources {
		var users []model.ACLUser
		database.DB.Where("acl_id = ?", r.ID).Find(&users)

		acls = append(acls, model.ACLWithUsers{
			ResourceACL: r,
			Users:       users,
		})
	}

	return acls, nil
}

func (s *aclService) DeleteResourceACL(resourceType string, resourceID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var acl model.ResourceACL
		err := tx.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&acl).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return fmt.Errorf("查询ACL失败: %w", err)
		}

		if err := tx.Where("acl_id = ?", acl.ID).Delete(&model.ACLUser{}).Error; err != nil {
			return fmt.Errorf("删除授权用户失败: %w", err)
		}

		if err := tx.Delete(&acl).Error; err != nil {
			return fmt.Errorf("删除ACL失败: %w", err)
		}

		return nil
	})
}

func hasAccessLevel(currentLevel, requiredLevel string) bool {
	levels := map[string]int{
		model.ACLAccessLevelRead:  1,
		model.ACLAccessLevelWrite: 2,
		model.ACLAccessLevelAdmin: 3,
	}

	current, ok1 := levels[currentLevel]
	required, ok2 := levels[requiredLevel]

	if !ok1 || !ok2 {
		return false
	}

	return current >= required
}

func (s *aclService) FilterByACL(userID uuid.UUID, resourceType string, query *gorm.DB, resourceIDColumn string) *gorm.DB {
	accessibleIDs, err := s.GetAccessibleResources(userID, resourceType)
	if err != nil {
		return query
	}

	if len(accessibleIDs) == 0 {
		return query.Where("1 = 0")
	}

	return query.Where(resourceIDColumn+" IN ?", accessibleIDs)
}
