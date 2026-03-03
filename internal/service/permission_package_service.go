package service

import (
	"errors"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

type PermissionPackageService interface {
	CreatePackage(name, code, description string, permissionIDs []uuid.UUID) (*model.PermissionPackage, error)
	GetPackageByID(id uuid.UUID) (*model.PermissionPackage, error)
	GetPackages(query string, page, pageSize int) ([]model.PermissionPackage, int64, error)
	UpdatePackage(id uuid.UUID, updates map[string]interface{}) error
	DeletePackage(id uuid.UUID) error
	AddPermissionsToPackage(packageID uuid.UUID, permissionIDs []uuid.UUID) error
	RemovePermissionsFromPackage(packageID uuid.UUID, permissionIDs []uuid.UUID) error
	GetPackagePermissions(packageID uuid.UUID) ([]model.Permission, error)
	AssignPackageToRole(roleID, packageID uuid.UUID) error
	RemovePackageFromRole(roleID, packageID uuid.UUID) error
	GetRolePackages(roleID uuid.UUID) ([]model.PermissionPackage, error)
	ClonePackage(id uuid.UUID, newCode string) (*model.PermissionPackage, error)
}

type permissionPackageService struct{}

func NewPermissionPackageService() PermissionPackageService {
	return &permissionPackageService{}
}

func (s *permissionPackageService) CreatePackage(name, code, description string, permissionIDs []uuid.UUID) (*model.PermissionPackage, error) {
	var existing model.PermissionPackage
	err := database.DB.Where("code = ?", code).First(&existing).Error
	if err == nil {
		return nil, errors.New("权限包代码已存在")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	pkg := &model.PermissionPackage{
		Name:        name,
		Code:        code,
		Description: description,
	}

	if err := database.DB.Create(pkg).Error; err != nil {
		return nil, err
	}

	if len(permissionIDs) > 0 {
		if err := s.AddPermissionsToPackage(pkg.ID, permissionIDs); err != nil {
			database.DB.Delete(pkg)
			return nil, err
		}
	}

	return s.GetPackageByID(pkg.ID)
}

func (s *permissionPackageService) GetPackageByID(id uuid.UUID) (*model.PermissionPackage, error) {
	var pkg model.PermissionPackage
	err := database.DB.Preload("Permissions").First(&pkg, id).Error
	if err != nil {
		return nil, err
	}
	return &pkg, nil
}

func (s *permissionPackageService) GetPackages(query string, page, pageSize int) ([]model.PermissionPackage, int64, error) {
	var packages []model.PermissionPackage
	var total int64

	db := database.DB.Model(&model.PermissionPackage{})

	if query != "" {
		db = db.Where("name LIKE ? OR code LIKE ? OR description LIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%")
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := db.Offset(offset).Limit(pageSize).Preload("Permissions").Find(&packages).Error; err != nil {
		return nil, 0, err
	}

	return packages, total, nil
}

func (s *permissionPackageService) UpdatePackage(id uuid.UUID, updates map[string]interface{}) error {
	return database.DB.Model(&model.PermissionPackage{}).Where("id = ?", id).Updates(updates).Error
}

func (s *permissionPackageService) DeletePackage(id uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var pkg model.PermissionPackage
		if err := tx.First(&pkg, id).Error; err != nil {
			return err
		}

		if err := tx.Where("package_id = ?", id).Delete(&model.PermissionPackagePermission{}).Error; err != nil {
			return err
		}

		return tx.Delete(&pkg).Error
	})
}

func (s *permissionPackageService) AddPermissionsToPackage(packageID uuid.UUID, permissionIDs []uuid.UUID) error {
	if len(permissionIDs) == 0 {
		return nil
	}

	exists, err := s.packageExists(packageID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("权限包不存在")
	}

	for _, permID := range permissionIDs {
		exists, err := s.permissionExists(permID)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		if err := database.DB.Exec(`
			INSERT INTO permission_package_permissions (package_id, permission_id, created_at)
			VALUES (?, ?, NOW())
			ON CONFLICT (package_id, permission_id) DO NOTHING
		`, packageID, permID).Error; err != nil {
			return err
		}
	}

	return nil
}

func (s *permissionPackageService) RemovePermissionsFromPackage(packageID uuid.UUID, permissionIDs []uuid.UUID) error {
	if len(permissionIDs) == 0 {
		return nil
	}

	return database.DB.Where("package_id = ? AND permission_id IN ?", packageID, permissionIDs).
		Delete(&model.PermissionPackagePermission{}).Error
}

func (s *permissionPackageService) GetPackagePermissions(packageID uuid.UUID) ([]model.Permission, error) {
	var permissions []model.Permission
	err := database.DB.
		Joins("JOIN permission_package_permissions ON permissions.id = permission_package_permissions.permission_id").
		Where("permission_package_permissions.package_id = ?", packageID).
		Find(&permissions).Error
	return permissions, err
}

func (s *permissionPackageService) AssignPackageToRole(roleID, packageID uuid.UUID) error {
	exists, err := s.roleExists(roleID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("角色不存在")
	}

	exists, err = s.packageExists(packageID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("权限包不存在")
	}

	return database.DB.Exec(`
		INSERT INTO role_permission_packages (role_id, package_id, created_at)
		VALUES (?, ?, NOW())
		ON CONFLICT (role_id, package_id) DO NOTHING
	`, roleID, packageID).Error
}

func (s *permissionPackageService) RemovePackageFromRole(roleID, packageID uuid.UUID) error {
	return database.DB.Exec(`
		DELETE FROM role_permission_packages WHERE role_id = ? AND package_id = ?
	`, roleID, packageID).Error
}

func (s *permissionPackageService) GetRolePackages(roleID uuid.UUID) ([]model.PermissionPackage, error) {
	var packages []model.PermissionPackage
	err := database.DB.
		Joins("JOIN role_permission_packages ON permission_packages.id = role_permission_packages.package_id").
		Where("role_permission_packages.role_id = ?", roleID).
		Preload("Permissions").
		Find(&packages).Error
	return packages, err
}

func (s *permissionPackageService) ClonePackage(id uuid.UUID, newCode string) (*model.PermissionPackage, error) {
	original, err := s.GetPackageByID(id)
	if err != nil {
		return nil, err
	}

	exists, err := s.packageCodeExists(newCode)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("权限包代码已存在")
	}

	newPkg := &model.PermissionPackage{
		Name:        original.Name + " (副本)",
		Code:        newCode,
		Description: original.Description,
	}

	if err := database.DB.Create(newPkg).Error; err != nil {
		return nil, err
	}

	var permIDs []uuid.UUID
	for _, perm := range original.Permissions {
		permIDs = append(permIDs, perm.ID)
	}

	if len(permIDs) > 0 {
		if err := s.AddPermissionsToPackage(newPkg.ID, permIDs); err != nil {
			database.DB.Delete(newPkg)
			return nil, err
		}
	}

	return s.GetPackageByID(newPkg.ID)
}

func (s *permissionPackageService) packageExists(id uuid.UUID) (bool, error) {
	var count int64
	err := database.DB.Model(&model.PermissionPackage{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

func (s *permissionPackageService) packageCodeExists(code string) (bool, error) {
	var count int64
	err := database.DB.Model(&model.PermissionPackage{}).Where("code = ?", code).Count(&count).Error
	return count > 0, err
}

func (s *permissionPackageService) permissionExists(id uuid.UUID) (bool, error) {
	var count int64
	err := database.DB.Model(&model.Permission{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

func (s *permissionPackageService) roleExists(id uuid.UUID) (bool, error) {
	var count int64
	err := database.DB.Model(&model.Role{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}
