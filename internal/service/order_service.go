package service

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// OrderService 订单服务接口
type OrderService interface {
	CreateOrder(userID, memberLevelID uuid.UUID, paymentMethod string) (*model.Order, error)
	CreatePaperCheckOrder(userID uuid.UUID, serviceType string, amount float64, paperID, templateID, paymentMethod string) (*model.Order, error)
	GetOrderByID(orderID uuid.UUID) (*model.Order, error)
	GetOrderByOrderNo(orderNo string) (*model.Order, error)
	GetOrdersByUserID(userID uuid.UUID, page, pageSize int) ([]model.Order, int64, error)
	GetAllOrders(page, pageSize int, statusFilter string) ([]model.Order, int64, error) // 管理员获取所有订单
	UpdateOrderStatus(orderID uuid.UUID, orderStatus, paymentStatus string) error
	GetOrderStatistics(userID uuid.UUID) (map[string]interface{}, error)
	GetOrderStatisticsForAdmin() (map[string]interface{}, error) // 管理员统计
	CancelOrder(orderID uuid.UUID) error
	GetExpiredOrders() ([]model.Order, error)
	DeleteOrder(orderID uuid.UUID) error
	BatchUpdateOrderStatus(orderIDs []uuid.UUID, orderStatus string) error
}

// orderService 订单服务实现
type orderService struct{}

// NewOrderService 创建订单服务实例
func NewOrderService() OrderService {
	return &orderService{}
}

// generateOrderNo 生成订单号
func generateOrderNo() string {
	now := time.Now()
	return fmt.Sprintf("ORD%s%06d", now.Format("20060102150405"), now.Nanosecond()%1000000)
}

// CreateOrder 创建订单
func (s *orderService) CreateOrder(userID, memberLevelID uuid.UUID, paymentMethod string) (*model.Order, error) {
	// 获取会员等级
	memberService := NewMemberService()
	level, err := memberService.GetMemberLevelByID(memberLevelID)
	if err != nil {
		return nil, err
	}

	// 生成订单号
	orderNo := generateOrderNo()

	// 计算订单过期时间
	startDate := time.Now()

	// 创建订单
	order := &model.Order{
		UserID:        userID,
		MemberLevelID: memberLevelID,
		OrderNo:       orderNo,
		TotalAmount:   level.Price,
		PaymentMethod: paymentMethod,
		PaymentStatus: "pending",
		OrderStatus:   "created",
		ExpiredAt:     startDate.Add(30 * time.Minute), // 订单30分钟后过期
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 保存到数据库
	if err := database.DB.Create(order).Error; err != nil {
		return nil, err
	}

	// 加载关联数据
	if err := database.DB.Preload("MemberLevel").First(order, order.ID).Error; err != nil {
		return nil, err
	}

	return order, nil
}

// CreatePaperCheckOrder 创建论文检查订单
func (s *orderService) CreatePaperCheckOrder(userID uuid.UUID, serviceType string, amount float64, paperID, templateID, paymentMethod string) (*model.Order, error) {
	// 生成订单号
	orderNo := generateOrderNo()

	// 计算订单过期时间
	startDate := time.Now()

	// 创建订单（论文检查订单不需要会员等级，设置一个默认的空 UUID）
	memberLevelID := uuid.Nil

	order := &model.Order{
		UserID:        userID,
		MemberLevelID: memberLevelID,
		OrderNo:       orderNo,
		TotalAmount:   amount,
		PaymentMethod: paymentMethod,
		PaymentStatus: "pending",
		OrderStatus:   "created",
		ExpiredAt:     startDate.Add(30 * time.Minute), // 订单30分钟后过期
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 保存到数据库
	if err := database.DB.Create(order).Error; err != nil {
		return nil, err
	}

	return order, nil
}

// GetOrderByID 根据ID获取订单
func (s *orderService) GetOrderByID(orderID uuid.UUID) (*model.Order, error) {
	var order model.Order
	if err := database.DB.Preload("MemberLevel").Preload("PaymentRecord").Preload("User").First(&order, orderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("order not found")
		}
		return nil, err
	}
	return &order, nil
}

// GetOrderByOrderNo 根据订单号获取订单
func (s *orderService) GetOrderByOrderNo(orderNo string) (*model.Order, error) {
	var order model.Order
	if err := database.DB.Preload("MemberLevel").Preload("PaymentRecord").Preload("User").Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("order not found")
		}
		return nil, err
	}
	return &order, nil
}

// GetOrdersByUserID 根据用户ID获取订单列表（包含 ACL 授权的订单）
func (s *orderService) GetOrdersByUserID(userID uuid.UUID, page, pageSize int) ([]model.Order, int64, error) {
	var orders []model.Order
	var total int64

	offset := (page - 1) * pageSize

	aclService := NewACLService()
	accessibleOrderIDs, err := aclService.GetAccessibleResources(userID, model.ACLResourceTypeOrder)
	if err != nil {
		return nil, 0, err
	}

	query := database.DB.Model(&model.Order{})

	if len(accessibleOrderIDs) > 0 {
		query = query.Where("user_id = ? OR id IN ?", userID, accessibleOrderIDs)
	} else {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := database.DB.Preload("MemberLevel").Preload("PaymentRecord").
		Where("user_id = ?", userID).
		Or("id IN ? AND user_id != ?", accessibleOrderIDs, userID).
		Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// GetAllOrders 管理员获取所有订单列表
func (s *orderService) GetAllOrders(page, pageSize int, statusFilter string) ([]model.Order, int64, error) {
	var orders []model.Order
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	query := database.DB.Model(&model.Order{}).Preload("User").Preload("MemberLevel").Preload("PaymentRecord")

	// 如果有状态过滤条件
	if statusFilter != "" {
		query = query.Where("order_status = ? OR payment_status = ?", statusFilter, statusFilter)
	}

	// 获取总记录数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// UpdateOrderStatus 更新订单状态
func (s *orderService) UpdateOrderStatus(orderID uuid.UUID, orderStatus, paymentStatus string) error {
	// 更新订单状态
	updates := map[string]interface{}{
		"order_status":   orderStatus,
		"payment_status": paymentStatus,
		"updated_at":     time.Now(),
	}

	if err := database.DB.Model(&model.Order{}).Where("id = ?", orderID).Updates(updates).Error; err != nil {
		return err
	}

	// 如果订单已支付，创建或更新会员信息
	if paymentStatus == "paid" && orderStatus == "completed" {
		// 获取订单信息
		order, err := s.GetOrderByID(orderID)
		if err != nil {
			return fmt.Errorf("获取订单信息失败: %w", err)
		}

		// 创建或更新会员信息
		memberService := NewMemberService()
		_, err = memberService.GetMemberByUserID(order.UserID)
		if err != nil {
			// 会员不存在，创建新会员
			newMember, err := memberService.CreateMember(order.UserID, order.MemberLevelID)
			if err != nil {
				return fmt.Errorf("创建会员失败: %w", err)
			}
			log.Printf("新会员创建成功: 用户ID %s, 会员等级 %s", order.UserID, newMember.MemberLevel.LevelName)
		} else {
			// 会员已存在，续费会员
			renewedMember, err := memberService.RenewMember(order.UserID, order.MemberLevelID)
			if err != nil {
				return fmt.Errorf("续费会员失败: %w", err)
			}
			log.Printf("会员续费成功: 用户ID %s, 会员等级 %s, 新到期时间 %s", order.UserID, renewedMember.MemberLevel.LevelName, renewedMember.EndDate)
		}
	}

	return nil
}

// GetOrderStatistics 获取订单统计信息
func (s *orderService) GetOrderStatistics(userID uuid.UUID) (map[string]interface{}, error) {
	var totalOrders, completedOrders, cancelledOrders int64
	var totalAmount float64

	// 总订单数
	if err := database.DB.Model(&model.Order{}).Where("user_id = ?", userID).Count(&totalOrders).Error; err != nil {
		return nil, err
	}

	// 已完成订单数
	if err := database.DB.Model(&model.Order{}).Where("user_id = ? AND order_status = ?", userID, "completed").Count(&completedOrders).Error; err != nil {
		return nil, err
	}

	// 已取消订单数
	if err := database.DB.Model(&model.Order{}).Where("user_id = ? AND order_status = ?", userID, "cancelled").Count(&cancelledOrders).Error; err != nil {
		return nil, err
	}

	// 总消费金额
	if err := database.DB.Model(&model.Order{}).Where("user_id = ? AND payment_status = ?", userID, "paid").Select("COALESCE(SUM(total_amount), 0)").Scan(&totalAmount).Error; err != nil {
		return nil, err
	}

	statistics := map[string]interface{}{
		"total_orders":     totalOrders,
		"completed_orders": completedOrders,
		"cancelled_orders": cancelledOrders,
		"total_amount":     totalAmount,
	}

	return statistics, nil
}

// GetOrderStatisticsForAdmin 管理员获取订单统计信息
func (s *orderService) GetOrderStatisticsForAdmin() (map[string]interface{}, error) {
	var totalOrders, completedOrders, pendingOrders, cancelledOrders int64
	var totalRevenue float64

	// 总订单数
	if err := database.DB.Model(&model.Order{}).Count(&totalOrders).Error; err != nil {
		return nil, err
	}

	// 已完成订单数
	if err := database.DB.Model(&model.Order{}).Where("order_status = ?", "completed").Count(&completedOrders).Error; err != nil {
		return nil, err
	}

	// 待支付订单数
	if err := database.DB.Model(&model.Order{}).Where("payment_status = ?", "pending").Count(&pendingOrders).Error; err != nil {
		return nil, err
	}

	// 已取消订单数
	if err := database.DB.Model(&model.Order{}).Where("order_status = ?", "cancelled").Count(&cancelledOrders).Error; err != nil {
		return nil, err
	}

	// 总收入
	if err := database.DB.Model(&model.Order{}).Where("payment_status = ?", "paid").Select("COALESCE(SUM(total_amount), 0)").Scan(&totalRevenue).Error; err != nil {
		return nil, err
	}

	statistics := map[string]interface{}{
		"total_orders":     totalOrders,
		"completed_orders": completedOrders,
		"pending_orders":   pendingOrders,
		"cancelled_orders": cancelledOrders,
		"total_revenue":    totalRevenue,
	}

	return statistics, nil
}

// CancelOrder 取消订单
func (s *orderService) CancelOrder(orderID uuid.UUID) error {
	// 更新订单状态
	return s.UpdateOrderStatus(orderID, "cancelled", "cancelled")
}

// GetExpiredOrders 获取过期订单
func (s *orderService) GetExpiredOrders() ([]model.Order, error) {
	var orders []model.Order
	now := time.Now()

	// 获取已过期但未处理的订单
	if err := database.DB.Where("expired_at < ? AND order_status = ? AND payment_status = ?", now, "created", "pending").Find(&orders).Error; err != nil {
		return nil, err
	}

	return orders, nil
}

// DeleteOrder 删除订单
func (s *orderService) DeleteOrder(orderID uuid.UUID) error {
	return database.DB.Delete(&model.Order{}, orderID).Error
}

// BatchUpdateOrderStatus 批量更新订单状态
func (s *orderService) BatchUpdateOrderStatus(orderIDs []uuid.UUID, orderStatus string) error {
	return database.DB.Model(&model.Order{}).Where("id IN ?", orderIDs).Update("order_status", orderStatus).Error
}
