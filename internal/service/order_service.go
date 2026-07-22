package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	return "ORD" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
}

func ExpirePendingOrders() (int64, error) {
	result := database.DB.Model(&model.Order{}).
		Where("expired_at < ? AND order_status = ? AND payment_status = ?", time.Now(), "created", "pending").
		Updates(map[string]interface{}{
			"order_status":   "cancelled",
			"payment_status": "expired",
			"updated_at":     time.Now(),
		})
	return result.RowsAffected, result.Error
}

func StartOrderExpirationTask(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := ExpirePendingOrders(); err != nil {
			log.Printf("expire pending orders: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// CreateOrder 创建订单
func (s *orderService) CreateOrder(userID, memberLevelID uuid.UUID, paymentMethod string) (*model.Order, error) {
	// 获取会员等级
	memberService := NewMemberService()
	level, err := memberService.GetMemberLevelByID(memberLevelID)
	if err != nil {
		return nil, err
	}
	var existing model.Order
	if err := database.DB.Preload("MemberLevel").Where(
		"user_id = ? AND member_level_id = ? AND order_status = ? AND payment_status = ? AND expired_at > ?",
		userID, memberLevelID, "created", "pending", time.Now(),
	).Order("created_at DESC").First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
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
	var parsedPaperID *uuid.UUID
	if value := strings.TrimSpace(paperID); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid paper id: %w", err)
		}
		parsedPaperID = &id
		var existing model.Order
		if err := database.DB.Where(
			"user_id = ? AND service_type = ? AND paper_id = ? AND order_status = ? AND payment_status = ? AND expired_at > ?",
			userID, serviceType, id, "created", "pending", time.Now(),
		).Order("created_at DESC").First(&existing).Error; err == nil {
			return &existing, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
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
		ServiceType:   serviceType,
		PaperID:       parsedPaperID,
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
		// ACL 表可能尚未创建，降级为仅查询本人订单
		log.Printf("[GetOrdersByUserID] ACL 查询失败，降级为仅用户订单: %v", err)
		accessibleOrderIDs = nil
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

	findQuery := database.DB.Preload("MemberLevel").Preload("PaymentRecord").
		Where("user_id = ?", userID)
	if len(accessibleOrderIDs) > 0 {
		findQuery = findQuery.Or("id IN ? AND user_id != ?", accessibleOrderIDs, userID)
	}
	if err := findQuery.Order("created_at DESC").
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
	if err := query.Order("created_at DESC, id DESC").
		Offset(offset).Limit(pageSize).
		Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// UpdateOrderStatus 更新订单状态
func (s *orderService) UpdateOrderStatus(orderID uuid.UUID, orderStatus, paymentStatus string) error {
	order, err := s.GetOrderByID(orderID)
	if err != nil {
		return err
	}
	sameState := order.OrderStatus == orderStatus && order.PaymentStatus == paymentStatus
	if sameState && !(orderStatus == "completed" && paymentStatus == "paid" && order.MemberLevelID != uuid.Nil && order.UsedAt == nil) {
		return nil
	}
	if !sameState && !validOrderTransition(order.OrderStatus, orderStatus, order.PaymentStatus, paymentStatus) {
		return fmt.Errorf("invalid order status transition: %s/%s -> %s/%s", order.OrderStatus, order.PaymentStatus, orderStatus, paymentStatus)
	}

	updates := map[string]interface{}{
		"order_status":   orderStatus,
		"payment_status": paymentStatus,
		"updated_at":     time.Now(),
	}

	if !sameState {
		result := database.DB.Model(&model.Order{}).
			Where("id = ? AND order_status = ? AND payment_status = ?", orderID, order.OrderStatus, order.PaymentStatus).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("order status changed concurrently")
		}
	}

	// 如果订单已支付，创建或更新会员信息
	if paymentStatus == "paid" && orderStatus == "completed" {
		// 获取订单信息
		// Paper service orders are one-time entitlements, not membership purchases.
		if order.MemberLevelID == uuid.Nil {
			return nil
		}
		claimedAt := time.Now()
		claim := database.DB.Model(&model.Order{}).Where("id = ? AND used_at IS NULL", orderID).Update("used_at", claimedAt)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return nil
		}

		// 创建或更新会员信息
		memberService := NewMemberService()
		_, err = memberService.GetMemberByUserID(order.UserID)
		if err != nil {
			// 会员不存在，创建新会员
			newMember, err := memberService.CreateMember(order.UserID, order.MemberLevelID)
			if err != nil {
				database.DB.Model(&model.Order{}).Where("id = ? AND used_at = ?", orderID, claimedAt).Update("used_at", nil)
				return fmt.Errorf("创建会员失败: %w", err)
			}
			log.Printf("新会员创建成功: 用户ID %s, 会员等级 %s", order.UserID, newMember.MemberLevel.LevelName)
		} else {
			// 会员已存在，续费会员
			renewedMember, err := memberService.RenewMember(order.UserID, order.MemberLevelID)
			if err != nil {
				database.DB.Model(&model.Order{}).Where("id = ? AND used_at = ?", orderID, claimedAt).Update("used_at", nil)
				return fmt.Errorf("续费会员失败: %w", err)
			}
			log.Printf("会员续费成功: 用户ID %s, 会员等级 %s, 新到期时间 %s", order.UserID, renewedMember.MemberLevel.LevelName, renewedMember.EndDate)
		}
	}

	return nil
}

func validOrderTransition(currentOrder, nextOrder, currentPayment, nextPayment string) bool {
	orderAllowed := map[string]map[string]bool{
		"created":   {"completed": true, "cancelled": true, "failed": true},
		"failed":    {"completed": true},
		"completed": {"refunded": true},
	}
	paymentAllowed := map[string]map[string]bool{
		"pending": {"paid": true, "cancelled": true, "expired": true, "failed": true},
		"failed":  {"paid": true},
		"paid":    {"refunded": true},
	}
	return orderAllowed[currentOrder][nextOrder] && paymentAllowed[currentPayment][nextPayment]
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
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var paymentIDs []uuid.UUID
		if err := tx.Model(&model.PaymentRecord{}).Where("order_id = ?", orderID).Pluck("id", &paymentIDs).Error; err != nil {
			return err
		}
		if len(paymentIDs) > 0 {
			if err := tx.Where("payment_id IN ?", paymentIDs).Delete(&model.PaymentResourceLink{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("order_id = ?", orderID).Delete(&model.PaymentRecord{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", orderID).Delete(&model.Order{}).Error
	})
}

// BatchUpdateOrderStatus 批量更新订单状态
func (s *orderService) BatchUpdateOrderStatus(orderIDs []uuid.UUID, orderStatus string) error {
	return database.DB.Model(&model.Order{}).Where("id IN ?", orderIDs).Update("order_status", orderStatus).Error
}
