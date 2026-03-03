package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
)

// PaymentService 支付服务接口
type PaymentService interface {
	CreatePayment(orderID uuid.UUID, paymentMethod string, paymentType string, clientIP string) (*model.PaymentRecord, map[string]interface{}, error)
	HandleWeChatCallback(data []byte) (map[string]interface{}, error)
	HandleAlipayCallback(data map[string]interface{}) (map[string]interface{}, error)
	GetPaymentByID(id uuid.UUID) (*model.PaymentRecord, error)
	GetPaymentByOrderID(orderID uuid.UUID) (*model.PaymentRecord, error)
	GetPaymentByTransactionID(transactionID string) (*model.PaymentRecord, error)
	UpdatePaymentStatus(id uuid.UUID, status string, transactionID string) error
	RefundPayment(paymentID uuid.UUID, amount float64) error
	GetPaymentStatistics(startDate, endDate time.Time) (map[string]interface{}, error)
}

// paymentService 支付服务实现
type paymentService struct {
	config               *config.Config
	wechatNativeService  *WechatNativeService
	alipayPagePayService *AlipayPagePayService
}

// NewPaymentService 创建支付服务实例
func NewPaymentService(config *config.Config) PaymentService {
	return &paymentService{
		config:               config,
		wechatNativeService:  NewWechatNativeService(config),
		alipayPagePayService: NewAlipayPagePayService(config),
	}
}

// CreatePayment 创建支付记录
func (s *paymentService) CreatePayment(orderID uuid.UUID, paymentMethod string, paymentType string, clientIP string) (*model.PaymentRecord, map[string]interface{}, error) {
	orderService := NewOrderService()
	order, err := orderService.GetOrderByID(orderID)
	if err != nil {
		return nil, nil, err
	}

	if order.PaymentStatus != "pending" || order.OrderStatus != "created" {
		return nil, nil, errors.New("order is not in pending state")
	}

	if time.Now().After(order.ExpiredAt) {
		orderService.UpdateOrderStatus(orderID, "cancelled", "expired")
		return nil, nil, errors.New("order has expired")
	}

	payment := &model.PaymentRecord{
		OrderID:       orderID,
		PaymentAmount: order.TotalAmount,
		PaymentMethod: paymentMethod,
		PaymentStatus: "pending",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := database.DB.Create(payment).Error; err != nil {
		return nil, nil, err
	}

	paymentParams, err := s.generatePaymentParams(payment, order, clientIP, paymentType)
	if err != nil {
		return nil, nil, err
	}

	return payment, paymentParams, nil
}

// generatePaymentParams 生成支付参数
func (s *paymentService) generatePaymentParams(payment *model.PaymentRecord, order *model.Order, clientIP string, paymentType string) (map[string]interface{}, error) {
	switch payment.PaymentMethod {
	case "wechat":
		return s.generateWeChatPaymentParams(payment, order, clientIP, paymentType)
	case "alipay":
		return s.generateAlipayPaymentParams(payment, order, paymentType)
	default:
		return nil, errors.New("unsupported payment method")
	}
}

// generateWeChatPaymentParams 生成微信支付参数
func (s *paymentService) generateWeChatPaymentParams(payment *model.PaymentRecord, order *model.Order, clientIP string, paymentType string) (map[string]interface{}, error) {
	switch paymentType {
	case "native":
		result, err := s.wechatNativeService.CreateNativePayOrder(order, payment.ID.String(), clientIP, order.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to create wechat native order: %w", err)
		}

		qrCodeURL := s.wechatNativeService.GenerateQRCodeImageURL(result.CodeURL, 256)

		return map[string]interface{}{
			"payment_type": "native",
			"code_url":     result.CodeURL,
			"qr_code_url":  qrCodeURL,
			"prepay_id":    result.PrepayID,
			"trade_type":   result.TradeType,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	case "jsapi":
		nonceStr := fmt.Sprintf("nonce_%d", time.Now().UnixNano())
		params := map[string]interface{}{
			"appid":            s.config.Wechat.AppID,
			"mch_id":           s.config.Wechat.MchID,
			"nonce_str":        nonceStr,
			"body":             fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName),
			"out_trade_no":     payment.ID.String(),
			"total_fee":        int(order.TotalAmount * 100),
			"spbill_create_ip": clientIP,
			"notify_url":       s.config.Wechat.NotifyURL,
			"trade_type":       "JSAPI",
		}

		sign, err := s.generateWeChatSignFromMap(params)
		if err != nil {
			return nil, fmt.Errorf("failed to generate wechat sign: %w", err)
		}
		params["sign"] = sign

		prepayID := fmt.Sprintf("wx%d", time.Now().Unix())

		jsapiParams := map[string]interface{}{
			"appId":     s.config.Wechat.AppID,
			"timeStamp": fmt.Sprintf("%d", time.Now().Unix()),
			"nonceStr":  nonceStr,
			"package":   fmt.Sprintf("prepay_id=%s", prepayID),
			"signType":  "MD5",
			"paySign":   sign,
		}

		return map[string]interface{}{
			"payment_type": "jsapi",
			"params":       jsapiParams,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	default:
		return nil, errors.New("unsupported wechat payment type")
	}
}

// generateWeChatSignFromMap 从map生成微信签名
func (s *paymentService) generateWeChatSignFromMap(params map[string]interface{}) (string, error) {
	stringParams := make(map[string]string)
	for k, v := range params {
		stringParams[k] = fmt.Sprintf("%v", v)
	}
	return s.wechatNativeService.GenerateSign(stringParams), nil
}

// generateWeChatSign 生成微信支付签名
func (s *paymentService) generateWeChatSign(params map[string]interface{}) (string, error) {
	return s.generateWeChatSignFromMap(params)
}

// verifyWeChatSign 验证微信支付签名
func (s *paymentService) verifyWeChatSign(params map[string]interface{}) error {
	// TODO: 实现真实的微信支付签名验证逻辑
	// 1. 从参数中提取sign
	// 2. 移除sign参数
	// 3. 参数排序
	// 4. 拼接字符串 + key
	// 5. MD5哈希并比较

	// 临时跳过验证，实际项目中需要实现
	return nil
}

// generateAlipayPaymentParams 生成支付宝支付参数
func (s *paymentService) generateAlipayPaymentParams(payment *model.PaymentRecord, order *model.Order, paymentType string) (map[string]interface{}, error) {
	switch paymentType {
	case "page":
		paymentURL, err := s.alipayPagePayService.CreateTradePagePay(order, order.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("failed to create alipay page pay: %w", err)
		}

		qrCodeURL := s.alipayPagePayService.GenerateQRCodeImageURL(paymentURL, 256)

		return map[string]interface{}{
			"payment_type": "page",
			"payment_url":  paymentURL,
			"qr_code_url":  qrCodeURL,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	default:
		return nil, errors.New("unsupported alipay payment type")
	}
}

// generateAlipaySign 生成支付宝签名
func (s *paymentService) generateAlipaySign(params map[string]interface{}) (string, error) {
	// TODO: 实现真实的RSA2签名逻辑
	// 这里需要根据支付宝的签名规则实现
	// 1. 参数排序
	// 2. 拼接字符串
	// 3. RSA2签名
	// 4. Base64编码

	// 临时返回模拟签名，实际项目中需要替换
	return "mock_alipay_signature", nil
}

// verifyAlipaySign 验证支付宝签名
func (s *paymentService) verifyAlipaySign(params map[string]interface{}) error {
	// TODO: 实现真实的支付宝签名验证逻辑
	// 1. 从参数中提取sign
	// 2. 移除sign参数
	// 3. 参数排序
	// 4. 拼接字符串
	// 5. 使用支付宝公钥验证签名

	// 临时跳过验证，实际项目中需要实现
	return nil
}

// HandleWeChatCallback 处理微信支付回调
func (s *paymentService) HandleWeChatCallback(data []byte) (map[string]interface{}, error) {
	// 解析微信回调数据（实际项目中需要解析XML格式）
	var callbackData map[string]interface{}
	if err := json.Unmarshal(data, &callbackData); err != nil {
		return nil, err
	}

	// 验证签名
	if err := s.verifyWeChatSign(callbackData); err != nil {
		return nil, fmt.Errorf("wechat signature verification failed: %w", err)
	}

	// 获取支付结果
	resultCode, _ := callbackData["result_code"].(string)
	outTradeNo, _ := callbackData["out_trade_no"].(string)
	transactionID, _ := callbackData["transaction_id"].(string)

	// 转换支付ID
	paymentID, err := uuid.Parse(outTradeNo)
	if err != nil {
		return nil, err
	}

	// 获取支付记录
	payment, err := s.GetPaymentByID(paymentID)
	if err != nil {
		return nil, err
	}

	// 更新支付状态
	if resultCode == "SUCCESS" {
		// 支付成功
		err = s.UpdatePaymentStatus(paymentID, "success", transactionID)
		if err != nil {
			return nil, err
		}

		// 更新订单状态
		orderService := NewOrderService()
		err = orderService.UpdateOrderStatus(payment.OrderID, "completed", "paid")
		if err != nil {
			return nil, err
		}
	} else {
		// 支付失败
		err = s.UpdatePaymentStatus(paymentID, "failed", transactionID)
		if err != nil {
			return nil, err
		}

		// 更新订单状态
		orderService := NewOrderService()
		err = orderService.UpdateOrderStatus(payment.OrderID, "failed", "failed")
		if err != nil {
			return nil, err
		}
	}

	// 返回微信要求的响应格式
	return map[string]interface{}{
		"return_code": "SUCCESS",
		"return_msg":  "OK",
	}, nil
}

// HandleAlipayCallback 处理支付宝支付回调
func (s *paymentService) HandleAlipayCallback(data map[string]interface{}) (map[string]interface{}, error) {
	// 验证签名
	if err := s.verifyAlipaySign(data); err != nil {
		return nil, fmt.Errorf("alipay signature verification failed: %w", err)
	}

	// 获取支付结果
	tradeStatus, _ := data["trade_status"].(string)
	outTradeNo, _ := data["out_trade_no"].(string)
	tradeNo, _ := data["trade_no"].(string)

	// 转换支付ID
	paymentID, err := uuid.Parse(outTradeNo)
	if err != nil {
		return nil, err
	}

	// 获取支付记录
	payment, err := s.GetPaymentByID(paymentID)
	if err != nil {
		return nil, err
	}

	// 更新支付状态
	if tradeStatus == "TRADE_SUCCESS" || tradeStatus == "TRADE_FINISHED" {
		// 支付成功
		err = s.UpdatePaymentStatus(paymentID, "success", tradeNo)
		if err != nil {
			return nil, err
		}

		// 更新订单状态
		orderService := NewOrderService()
		err = orderService.UpdateOrderStatus(payment.OrderID, "completed", "paid")
		if err != nil {
			return nil, err
		}
	} else {
		// 支付失败
		err = s.UpdatePaymentStatus(paymentID, "failed", tradeNo)
		if err != nil {
			return nil, err
		}

		// 更新订单状态
		orderService := NewOrderService()
		err = orderService.UpdateOrderStatus(payment.OrderID, "failed", "failed")
		if err != nil {
			return nil, err
		}
	}

	// 返回支付宝要求的响应格式
	return map[string]interface{}{
		"code": "10000",
		"msg":  "Success",
	}, nil
}

// GetPaymentByID 根据ID获取支付记录
func (s *paymentService) GetPaymentByID(id uuid.UUID) (*model.PaymentRecord, error) {
	var payment model.PaymentRecord
	if err := database.DB.Preload("Order").First(&payment, id).Error; err != nil {
		return nil, err
	}
	return &payment, nil
}

// GetPaymentByOrderID 根据订单ID获取支付记录
func (s *paymentService) GetPaymentByOrderID(orderID uuid.UUID) (*model.PaymentRecord, error) {
	var payment model.PaymentRecord
	if err := database.DB.Preload("Order").Where("order_id = ?", orderID).First(&payment).Error; err != nil {
		return nil, err
	}
	return &payment, nil
}

// GetPaymentByTransactionID 根据交易ID获取支付记录
func (s *paymentService) GetPaymentByTransactionID(transactionID string) (*model.PaymentRecord, error) {
	var payment model.PaymentRecord
	if err := database.DB.Preload("Order").Where("transaction_id = ?", transactionID).First(&payment).Error; err != nil {
		return nil, err
	}
	return &payment, nil
}

// UpdatePaymentStatus 更新支付状态
func (s *paymentService) UpdatePaymentStatus(id uuid.UUID, status string, transactionID string) error {
	// 更新支付记录
	updates := map[string]interface{}{
		"payment_status": status,
		"transaction_id": transactionID,
		"payment_time":   time.Now(),
		"updated_at":     time.Now(),
	}

	return database.DB.Model(&model.PaymentRecord{}).Where("id = ?", id).Updates(updates).Error
}

// RefundPayment 退款
func (s *paymentService) RefundPayment(paymentID uuid.UUID, amount float64) error {
	// 获取支付记录
	payment, err := s.GetPaymentByID(paymentID)
	if err != nil {
		return err
	}

	// 检查支付状态
	if payment.PaymentStatus != "success" {
		return errors.New("payment is not in success state")
	}

	// 检查退款金额
	if amount <= 0 || amount > payment.PaymentAmount {
		return errors.New("invalid refund amount")
	}

	// 调用支付平台的退款API
	// 实际项目中需要调用微信或支付宝的退款API

	// 更新支付记录状态
	// 实际项目中需要记录退款信息

	// 更新订单状态
	orderService := NewOrderService()
	err = orderService.UpdateOrderStatus(payment.OrderID, "refunded", "refunded")
	if err != nil {
		return err
	}

	return nil
}

// GetPaymentStatistics 获取支付统计信息
func (s *paymentService) GetPaymentStatistics(startDate, endDate time.Time) (map[string]interface{}, error) {
	var totalPayments, successPayments, failedPayments int64
	var totalAmount float64

	// 总支付笔数
	if err := database.DB.Model(&model.PaymentRecord{}).Where("created_at BETWEEN ? AND ?", startDate, endDate).Count(&totalPayments).Error; err != nil {
		return nil, err
	}

	// 成功支付笔数
	if err := database.DB.Model(&model.PaymentRecord{}).Where("created_at BETWEEN ? AND ? AND payment_status = ?", startDate, endDate, "success").Count(&successPayments).Error; err != nil {
		return nil, err
	}

	// 失败支付笔数
	if err := database.DB.Model(&model.PaymentRecord{}).Where("created_at BETWEEN ? AND ? AND payment_status = ?", startDate, endDate, "failed").Count(&failedPayments).Error; err != nil {
		return nil, err
	}

	// 总支付金额
	if err := database.DB.Model(&model.PaymentRecord{}).Where("created_at BETWEEN ? AND ? AND payment_status = ?", startDate, endDate, "success").Select("COALESCE(SUM(payment_amount), 0)").Scan(&totalAmount).Error; err != nil {
		return nil, err
	}

	statistics := map[string]interface{}{
		"total_payments":   totalPayments,
		"success_payments": successPayments,
		"failed_payments":  failedPayments,
		"total_amount":     totalAmount,
		"success_rate":     float64(successPayments) / float64(totalPayments) * 100,
		"start_date":       startDate,
		"end_date":         endDate,
	}

	return statistics, nil
}
