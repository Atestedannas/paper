package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// UserService 用户服务接口
type UserService interface {
	Register(username, email, password string) (*model.User, error)
	Login(account, password string) (*model.User, error)
	GetUserByID(id uuid.UUID) (*model.User, error)
	GetUserByEmail(email string) (*model.User, error)
	GetUserByWechatOpenID(openID string) (*model.User, error)
	GetUserByAlipayOpenID(openID string) (*model.User, error)
	CreateOrUpdateWechatUser(openID, nickname, unionID, avatar string, gender int) (*model.User, error)
	CreateOrUpdateAlipayUser(userID, openID, nickname, avatar, gender string) (*model.User, error)
	UpdateUser(user *model.User) error
	ChangePassword(userID uuid.UUID, oldPassword, newPassword string) error
	// 管理员相关功能
	GetAllUsers(page, pageSize int) ([]model.User, int64, error)
	UpdateUserRole(userID uuid.UUID, role string) error
	DeleteUser(userID uuid.UUID) error
	UpdateUserStatus(userID uuid.UUID, status string) error
	UpdateUserFreeChecks(userID uuid.UUID, checks int) error
}

// userService 用户服务实现
type userService struct{}

// NewUserService 创建用户服务实例
func NewUserService() UserService {
	return &userService{}
}

// Register 用户注册
func (s *userService) Register(username, email, password string) (*model.User, error) {
	// 检查数据库连接
	if database.DB == nil {
		return nil, errors.New("service unavailable")
	}

	// 检查用户名是否已存在
	var existingUser model.User
	err := database.DB.Where("username = ?", username).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("username already exists")
	}

	// 检查邮箱是否已存在
	err = database.DB.Where("email = ?", email).First(&existingUser).Error
	if err == nil {
		return nil, errors.New("email already exists")
	}

	// 生成密码哈希 - 提高加密成本因子到12
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}

	// 创建新用户
	user := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hashedPassword),
		Status:       "active",
		FreeChecks:   2, // 新用户默认送2次免费额度
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// 保存到数据库
	if err := database.DB.Create(user).Error; err != nil {
		return nil, errors.New("service unavailable")
	}

	return user, nil
}

// Login 用户登录
func (s *userService) Login(account, password string) (*model.User, error) {
	var user model.User
	var err error

	// 判断是邮箱还是用户名登录
	if strings.Contains(account, "@") {
		// 邮箱登录
		err = database.DB.Where("email = ?", account).First(&user).Error
	} else {
		// 用户名登录
		err = database.DB.Where("username = ?", account).First(&user).Error
	}

	if err != nil {
		fmt.Printf("Login failed: User not found or DB error for account '%s': %v\n", account, err)
		return nil, errors.New("invalid account or password")
	}
	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		fmt.Printf("Login failed: Password mismatch for account '%s'\n", account)
		return nil, errors.New("invalid account or password")
	}

	// 检查用户状态
	if user.Status != "active" {
		return nil, errors.New("user account is not active")
	}

	return &user, nil
}

// GetUserByID 根据ID获取用户
func (s *userService) GetUserByID(id uuid.UUID) (*model.User, error) {
	var user model.User
	if err := database.DB.First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail 根据邮箱获取用户
func (s *userService) GetUserByEmail(email string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByWechatOpenID 根据微信OpenID获取用户
func (s *userService) GetUserByWechatOpenID(openID string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("wechat_open_id = ?", openID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByAlipayOpenID 根据支付宝OpenID获取用户
func (s *userService) GetUserByAlipayOpenID(openID string) (*model.User, error) {
	var user model.User
	if err := database.DB.Where("alipay_open_id = ?", openID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateOrUpdateWechatUser 创建或更新微信用户
func (s *userService) CreateOrUpdateWechatUser(openID, nickname, unionID, avatar string, gender int) (*model.User, error) {
	// 检查用户是否已存在
	var user model.User
	err := database.DB.Where("wechat_open_id = ?", openID).First(&user).Error

	if err != nil {
		// 用户不存在，创建新用户
		user = model.User{
			Username:     fmt.Sprintf("wechat_%s", openID[:10]),
			Email:        fmt.Sprintf("wechat_%s@example.com", openID),
			FullName:     nickname,
			Avatar:       avatar,
			WechatOpenID: &openID,
			Status:       "active",
			FreeChecks:   2, // 微信新用户也送2次
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		// 保存到数据库
		if err := database.DB.Create(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to create wechat user: %w", err)
		}
	} else {
		// 用户已存在，更新用户信息
		user.FullName = nickname
		user.Avatar = avatar
		user.UpdatedAt = time.Now()

		// 保存到数据库
		if err := database.DB.Save(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to update wechat user: %w", err)
		}
	}

	return &user, nil
}

// CreateOrUpdateAlipayUser 创建或更新支付宝用户
func (s *userService) CreateOrUpdateAlipayUser(userID, openID, nickname, avatar, gender string) (*model.User, error) {
	// 检查用户是否已存在
	var user model.User
	err := database.DB.Where("alipay_open_id = ?", openID).First(&user).Error

	if err != nil {
		// 用户不存在，创建新用户
		user = model.User{
			Username:     fmt.Sprintf("alipay_%s", userID[:10]),
			Email:        fmt.Sprintf("alipay_%s@example.com", userID),
			FullName:     nickname,
			Avatar:       avatar,
			AlipayOpenID: &openID,
			Status:       "active",
			FreeChecks:   2, // 支付宝新用户也送2次
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		// 保存到数据库
		if err := database.DB.Create(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to create alipay user: %w", err)
		}
	} else {
		// 用户已存在，更新用户信息
		user.FullName = nickname
		user.Avatar = avatar
		user.UpdatedAt = time.Now()

		// 保存到数据库
		if err := database.DB.Save(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to update alipay user: %w", err)
		}
	}

	return &user, nil
}

// UpdateUser 鏇存柊鐢ㄦ埛淇℃伅
func (s *userService) UpdateUser(user *model.User) error {
	user.UpdatedAt = time.Now()
	return database.DB.Save(user).Error
}

// ChangePassword 修改密码
func (s *userService) ChangePassword(userID uuid.UUID, oldPassword, newPassword string) error {
	// 获取用户信息
	user, err := s.GetUserByID(userID)
	if err != nil {
		return err
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("invalid old password")
	}

	// 生成新密码哈希 - 提高加密成本因子到12
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return err
	}

	// 更新密码
	user.PasswordHash = string(hashedPassword)
	user.UpdatedAt = time.Now()

	return database.DB.Save(user).Error
}

// GetAllUsers 获取所有用户，支持分页
func (s *userService) GetAllUsers(page, pageSize int) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 获取总记录数
	if err := database.DB.Model(&model.User{}).Where("status != ?", "deleted").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := database.DB.Where("status != ?", "deleted").Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// UpdateUserRole 更新用户角色
func (s *userService) UpdateUserRole(userID uuid.UUID, role string) error {
	// 验证角色值
	validRoles := []string{"user", "admin"}
	valid := false
	for _, r := range validRoles {
		if r == role {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid role value")
	}

	// 更新角色
	return database.DB.Model(&model.User{}).Where("id = ?", userID).Update("role", role).Error
}

// DeleteUser 删除用户
func (s *userService) DeleteUser(userID uuid.UUID) error {
	// 软删除用户，将状态改为deleted
	return database.DB.Model(&model.User{}).Where("id = ?", userID).Update("status", "deleted").Error
}

// UpdateUserStatus 更新用户状态
func (s *userService) UpdateUserStatus(userID uuid.UUID, status string) error {
	// 验证状态值
	validStatuses := []string{"active", "inactive", "deleted"}
	valid := false
	for _, s := range validStatuses {
		if s == status {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid status value")
	}

	// 更新状态
	return database.DB.Model(&model.User{}).Where("id = ?", userID).Update("status", status).Error
}

// UpdateUserFreeChecks 更新用户免费检查次数
func (s *userService) UpdateUserFreeChecks(userID uuid.UUID, checks int) error {
	if checks < 0 {
		return errors.New("invalid checks count")
	}
	return database.DB.Model(&model.User{}).Where("id = ?", userID).Update("free_checks", checks).Error
}
