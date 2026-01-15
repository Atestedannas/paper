package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

// MemberService 会员服务接口
type MemberService interface {
	// 会员等级管理
	GetAllMemberLevels() ([]model.MemberLevel, error)
	GetMemberLevelByID(id uuid.UUID) (*model.MemberLevel, error)
	GetMemberLevelByName(levelName string) (*model.MemberLevel, error)
	CreateMemberLevel(level *model.MemberLevel) error
	UpdateMemberLevel(level *model.MemberLevel) error
	DeleteMemberLevel(id uuid.UUID) error

	// 会员信息管理
	GetMemberByUserID(userID uuid.UUID) (*model.Member, error)
	CreateMember(userID, memberLevelID uuid.UUID) (*model.Member, error)
	UpdateMember(member *model.Member) error
	RenewMember(userID, memberLevelID uuid.UUID) (*model.Member, error)
	CheckMemberStatus(userID uuid.UUID) (bool, error)
	GetMemberRemainingChecks(userID uuid.UUID) (int, error)
	IncrementCheckCount(userID uuid.UUID) error
}

// memberService 会员服务实现
type memberService struct{}

// NewMemberService 创建会员服务实例
func NewMemberService() MemberService {
	return &memberService{}
}

// GetAllMemberLevels 获取所有会员等级
func (s *memberService) GetAllMemberLevels() ([]model.MemberLevel, error) {
	var levels []model.MemberLevel
	err := database.DB.Where("is_active = ?", true).Order("sort_order ASC").Find(&levels).Error
	return levels, err
}

// GetMemberLevelByID 根据ID获取会员等级
func (s *memberService) GetMemberLevelByID(id uuid.UUID) (*model.MemberLevel, error) {
	var level model.MemberLevel
	if err := database.DB.First(&level, id).Error; err != nil {
		return nil, errors.New("member level not found")
	}
	return &level, nil
}

// GetMemberLevelByName 根据名称获取会员等级
func (s *memberService) GetMemberLevelByName(levelName string) (*model.MemberLevel, error) {
	var level model.MemberLevel
	if err := database.DB.Where("level_name = ?", levelName).First(&level).Error; err != nil {
		return nil, errors.New("member level not found")
	}
	return &level, nil
}

// CreateMemberLevel 创建会员等级
func (s *memberService) CreateMemberLevel(level *model.MemberLevel) error {
	level.CreatedAt = time.Now()
	level.UpdatedAt = time.Now()
	return database.DB.Create(level).Error
}

// UpdateMemberLevel 更新会员等级
func (s *memberService) UpdateMemberLevel(level *model.MemberLevel) error {
	level.UpdatedAt = time.Now()
	return database.DB.Save(level).Error
}

// DeleteMemberLevel 删除会员等级
func (s *memberService) DeleteMemberLevel(id uuid.UUID) error {
	return database.DB.Delete(&model.MemberLevel{}, id).Error
}

// GetMemberByUserID 根据用户ID获取会员信息
func (s *memberService) GetMemberByUserID(userID uuid.UUID) (*model.Member, error) {
	var member model.Member
	if err := database.DB.Preload("MemberLevel").Where("user_id = ?", userID).First(&member).Error; err != nil {
		return nil, errors.New("member not found")
	}
	return &member, nil
}

// CreateMember 创建会员信息
func (s *memberService) CreateMember(userID, memberLevelID uuid.UUID) (*model.Member, error) {
	// 获取会员等级
	level, err := s.GetMemberLevelByID(memberLevelID)
	if err != nil {
		return nil, err
	}

	// 计算有效期
	startDate := time.Now()
	endDate := startDate.AddDate(0, 0, level.DurationDays)

	// 创建会员记录
	member := &model.Member{
		UserID:        userID,
		MemberLevelID: memberLevelID,
		StartDate:     startDate,
		EndDate:       endDate,
		Status:        "active",
		TotalChecks:   0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 保存到数据库
	if err := database.DB.Create(member).Error; err != nil {
		return nil, err
	}

	// 加载会员等级信息
	if err := database.DB.Preload("MemberLevel").First(member, member.ID).Error; err != nil {
		return nil, err
	}

	return member, nil
}

// UpdateMember 更新会员信息
func (s *memberService) UpdateMember(member *model.Member) error {
	member.UpdatedAt = time.Now()
	return database.DB.Save(member).Error
}

// RenewMember 续费会员
func (s *memberService) RenewMember(userID, memberLevelID uuid.UUID) (*model.Member, error) {
	// 获取现有会员信息
	member, err := s.GetMemberByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 获取会员等级
	level, err := s.GetMemberLevelByID(memberLevelID)
	if err != nil {
		return nil, err
	}

	// 计算新的有效期
	var newEndDate time.Time
	now := time.Now()

	if member.EndDate.After(now) {
		// 如果会员未过期，从当前结束日期延长
		newEndDate = member.EndDate.AddDate(0, 0, level.DurationDays)
	} else {
		// 如果会员已过期，从当前时间开始计算
		newEndDate = now.AddDate(0, 0, level.DurationDays)
	}

	// 更新会员信息
	member.MemberLevelID = memberLevelID
	member.EndDate = newEndDate
	member.Status = "active"
	member.UpdatedAt = time.Now()

	if err := database.DB.Save(member).Error; err != nil {
		return nil, err
	}

	// 重新加载会员等级信息
	if err := database.DB.Preload("MemberLevel").First(member, member.ID).Error; err != nil {
		return nil, err
	}

	return member, nil
}

// CheckMemberStatus 检查会员状态
func (s *memberService) CheckMemberStatus(userID uuid.UUID) (bool, error) {
	member, err := s.GetMemberByUserID(userID)
	if err != nil {
		return false, err
	}

	now := time.Now()
	// 检查会员是否有效
	if member.Status == "active" && member.EndDate.After(now) {
		return true, nil
	}

	// 如果会员过期，更新状态
	if member.EndDate.Before(now) {
		member.Status = "expired"
		if err := database.DB.Save(member).Error; err != nil {
			return false, err
		}
	}

	return false, nil
}

// GetMemberRemainingChecks 获取会员剩余检查次数
func (s *memberService) GetMemberRemainingChecks(userID uuid.UUID) (int, error) {
	// 1. 先检查是否是会员
	member, err := s.GetMemberByUserID(userID)
	isMember := err == nil

	// 2. 检查会员状态
	isActive := false
	if isMember {
		isActive, _ = s.CheckMemberStatus(userID)
	}

	// 3. 如果是有效会员，返回会员剩余次数
	if isActive {
		remaining := member.MemberLevel.MaxChecks - member.TotalChecks
		if remaining < 0 {
			remaining = 0
		}
		return remaining, nil
	}

	// 4. 如果不是会员或会员已过期，返回用户免费试用剩余次数
	// 获取用户信息
	userService := NewUserService()
	user, err := userService.GetUserByID(userID)
	if err != nil {
		return 0, err
	}

	return user.FreeChecks, nil
}

// IncrementCheckCount 增加检查次数
func (s *memberService) IncrementCheckCount(userID uuid.UUID) error {
	// 1. 尝试作为会员扣减
	member, err := s.GetMemberByUserID(userID)
	isMember := err == nil
	isActive := false
	if isMember {
		isActive, _ = s.CheckMemberStatus(userID)
	}

	if isActive {
		// 检查是否超过最大检查次数
		if member.TotalChecks >= member.MemberLevel.MaxChecks {
			return errors.New("check count limit exceeded")
		}

		// 增加检查次数
		member.TotalChecks++
		member.UpdatedAt = time.Now()
		return database.DB.Save(member).Error
	}

	// 2. 如果不是会员，扣减免费试用次数
	userService := NewUserService()
	user, err := userService.GetUserByID(userID)
	if err != nil {
		return err
	}

	if user.FreeChecks > 0 {
		user.FreeChecks--
		return database.DB.Save(user).Error
	}

	return errors.New("no remaining checks available")
}
