package service

import (
	"bytes"
	"crypto/subtle"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"gorm.io/gorm"
)

// PaymentService 支付服务接口
type PaymentService interface {
	CreatePayment(orderID uuid.UUID, paymentMethod string, paymentType string, clientIP string) (*model.PaymentRecord, map[string]interface{}, error)
	HandleWeChatCallback(data []byte) (map[string]interface{}, error)
	HandleAlipayCallback(data map[string]interface{}) error
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

// CreatePayment 创建支付记录（幂等：同订单存在 pending 记录则复用）
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

	// 幂等：同订单存在 pending 支付记录则复用；切换渠道前先关闭旧渠道订单。
	var existing model.PaymentRecord
	if err := database.DB.
		Where("order_id = ? AND payment_status = 'pending'", orderID).
		First(&existing).Error; err == nil {
		if existing.PaymentMethod != paymentMethod {
			if err := s.closePendingProviderOrder(existing.PaymentMethod, order.OrderNo); err != nil {
				return nil, nil, fmt.Errorf("close previous payment order: %w", err)
			}
			if err := database.DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&existing).Updates(map[string]interface{}{
					"payment_method": paymentMethod,
					"updated_at":     time.Now(),
				}).Error; err != nil {
					return err
				}
				return tx.Model(order).Update("payment_method", paymentMethod).Error
			}); err != nil {
				return nil, nil, err
			}
			existing.PaymentMethod = paymentMethod
		}
		paymentParams, err := s.generatePaymentParams(&existing, order, clientIP, paymentMethod, paymentType)
		if err != nil {
			return nil, nil, err
		}
		return &existing, paymentParams, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}

	// 新建支付记录，用 UUID 前缀占位，避免空字符串重复违反唯一约束
	newID := uuid.New()
	payment := &model.PaymentRecord{
		ID:            newID,
		OrderID:       orderID,
		TransactionID: "PENDING_" + newID.String(),
		PaymentAmount: order.TotalAmount,
		PaymentMethod: paymentMethod,
		PaymentStatus: "pending",
		ExtraData:     "{}",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := database.DB.Create(payment).Error; err != nil {
		return nil, nil, err
	}

	paymentParams, err := s.generatePaymentParams(payment, order, clientIP, paymentMethod, paymentType)
	if err != nil {
		return nil, nil, err
	}

	return payment, paymentParams, nil
}

func (s *paymentService) closePendingProviderOrder(paymentMethod, orderNo string) error {
	switch paymentMethod {
	case "wechat":
		return s.wechatNativeService.CloseOrder(orderNo)
	case "alipay":
		return s.alipayPagePayService.CloseTrade(orderNo)
	default:
		return fmt.Errorf("unsupported payment method %q", paymentMethod)
	}
}

// generatePaymentParams 使用本次请求的 paymentMethod（避免复用 pending 记录时仍走旧的微信/支付宝分支）
func (s *paymentService) generatePaymentParams(payment *model.PaymentRecord, order *model.Order, clientIP string, paymentMethod string, paymentType string) (map[string]interface{}, error) {
	switch paymentMethod {
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
			"total_fee":        int(math.Round(order.TotalAmount * 100)),
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
	if strings.TrimSpace(s.config.Wechat.ApiKey) == "" {
		return errors.New("wechat API key is not configured")
	}
	stringParams := make(map[string]string, len(params))
	for key, value := range params {
		stringParams[key] = fmt.Sprint(value)
	}
	provided := strings.ToUpper(strings.TrimSpace(stringParams["sign"]))
	if provided == "" {
		return errors.New("missing sign")
	}
	expected := s.wechatNativeService.GenerateSign(stringParams)
	if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		return errors.New("invalid sign")
	}
	return nil
}

// generateAlipayPaymentParams 生成支付宝支付参数
func (s *paymentService) generateAlipayPaymentParams(payment *model.PaymentRecord, order *model.Order, paymentType string) (map[string]interface{}, error) {
	switch paymentType {
	case "precreate":
		qrContent, err := s.alipayPagePayService.CreateTradePrecreate(order, order.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("支付宝当面付创建失败: %w", err)
		}
		qrCodeURL := s.alipayPagePayService.GenerateQRCodeImageURL(qrContent, 256)
		return map[string]interface{}{
			"payment_type": "precreate",
			"qr_content":   qrContent,
			"qr_code_url":  qrCodeURL,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	case "wap":
		paymentURL, err := s.alipayPagePayService.CreateTradeWapPay(order, order.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("支付宝手机网站支付创建失败: %w", err)
		}
		qrCodeURL := s.alipayPagePayService.GenerateQRCodeImageURL(paymentURL, 256)
		return map[string]interface{}{
			"payment_type": "wap",
			"payment_url":  paymentURL,
			"qr_content":   paymentURL,
			"qr_code_url":  qrCodeURL,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	case "page":
		paymentURL, err := s.alipayPagePayService.CreateTradePagePay(order, order.TotalAmount)
		if err != nil {
			return nil, fmt.Errorf("支付宝页面支付创建失败: %w", err)
		}
		return map[string]interface{}{
			"payment_type": "page",
			"payment_url":  paymentURL,
			"order_no":     order.OrderNo,
			"total_amount": order.TotalAmount,
		}, nil
	case "auto":
		result, err := s.generateAlipayPaymentParams(payment, order, "precreate")
		if err != nil {
			log.Printf("[AlipayAuto] precreate 失败: %v，降级到 page 模式", err)
			return s.generateAlipayPaymentParams(payment, order, "page")
		}
		return result, nil
	default:
		return nil, errors.New("unsupported alipay payment type")
	}
}

// verifyAlipaySign 验证支付宝异步通知签名（需配置开放平台「支付宝公钥」，非应用公钥）
func (s *paymentService) verifyAlipaySign(params map[string]interface{}) error {
	strParams := make(map[string]string)
	for k, v := range params {
		switch val := v.(type) {
		case string:
			strParams[k] = val
		case []string:
			if len(val) > 0 {
				strParams[k] = val[0]
			}
		default:
			strParams[k] = fmt.Sprintf("%v", v)
		}
	}
	sign := strParams["sign"]
	if sign == "" {
		return fmt.Errorf("missing sign")
	}
	return s.alipayPagePayService.VerifySign(strParams, sign)
}

// HandleWeChatCallback 处理微信支付回调
func (s *paymentService) HandleWeChatCallback(data []byte) (map[string]interface{}, error) {
	callbackData, err := parseWechatCallbackXML(data)
	if err != nil {
		return nil, err
	}
	if err := s.verifyWeChatSign(callbackData); err != nil {
		return nil, fmt.Errorf("wechat signature verification failed: %w", err)
	}
	if callbackData["return_code"] != "SUCCESS" || callbackData["result_code"] != "SUCCESS" {
		return nil, fmt.Errorf("wechat callback reported failure: %v", callbackData["return_msg"])
	}
	if callbackData["appid"] != s.config.Wechat.AppID || callbackData["mch_id"] != s.config.Wechat.MchID {
		return nil, errors.New("wechat merchant identity mismatch")
	}
	payment, err := s.resolvePaymentByOutTradeNo(fmt.Sprint(callbackData["out_trade_no"]))
	if err != nil {
		return nil, err
	}
	totalFee, err := strconv.ParseInt(fmt.Sprint(callbackData["total_fee"]), 10, 64)
	if err != nil || totalFee != int64(math.Round(payment.PaymentAmount*100)) {
		return nil, errors.New("wechat payment amount mismatch")
	}
	if err := s.UpdatePaymentStatus(payment.ID, "success", fmt.Sprint(callbackData["transaction_id"])); err != nil {
		return nil, err
	}
	if err := NewOrderService().UpdateOrderStatus(payment.OrderID, "completed", "paid"); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"return_code": "SUCCESS",
		"return_msg":  "OK",
	}, nil
}

func parseWechatCallbackXML(data []byte) (map[string]interface{}, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	values := make(map[string]interface{})
	var field string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid wechat callback XML: %w", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, errors.New("XML directives are not allowed")
		case xml.StartElement:
			if value.Name.Local != "xml" {
				field = value.Name.Local
			}
		case xml.CharData:
			if field != "" {
				values[field] = strings.TrimSpace(string(value))
			}
		case xml.EndElement:
			if value.Name.Local == field {
				field = ""
			}
		}
	}
	if len(values) == 0 {
		return nil, errors.New("empty wechat callback")
	}
	return values, nil
}

// resolvePaymentByOutTradeNo out_trade_no 为订单号（ORD…）或支付记录 UUID
func (s *paymentService) resolvePaymentByOutTradeNo(outTradeNo string) (*model.PaymentRecord, error) {
	if outTradeNo == "" {
		return nil, fmt.Errorf("empty out_trade_no")
	}
	if paymentID, err := uuid.Parse(outTradeNo); err == nil {
		return s.GetPaymentByID(paymentID)
	}
	orderSvc := NewOrderService()
	order, err := orderSvc.GetOrderByOrderNo(outTradeNo)
	if err != nil {
		return nil, err
	}
	var payment model.PaymentRecord
	if err := database.DB.Preload("Order").Where("order_id = ? AND payment_status = ?", order.ID, "pending").
		Order("created_at DESC").First(&payment).Error; err != nil {
		if err := database.DB.Preload("Order").Where("order_id = ?", order.ID).Order("created_at DESC").First(&payment).Error; err != nil {
			return nil, err
		}
	}
	return &payment, nil
}

// HandleAlipayCallback 处理支付宝支付回调（返回 error，由 Handler 写 success/fail 明文）
func (s *paymentService) HandleAlipayCallback(data map[string]interface{}) error {
	log.Printf("[AlipayCallback] 收到支付宝回调: trade_status=%v, out_trade_no=%v, trade_no=%v",
		data["trade_status"], data["out_trade_no"], data["trade_no"])

	if err := s.verifyAlipaySign(data); err != nil {
		log.Printf("[AlipayCallback] 验签失败: %v", err)
		return fmt.Errorf("alipay signature verification failed: %w", err)
	}
	log.Printf("[AlipayCallback] 验签成功")

	tradeStatus, _ := data["trade_status"].(string)
	outTradeNo, _ := data["out_trade_no"].(string)
	tradeNo, _ := data["trade_no"].(string)

	payment, err := s.resolvePaymentByOutTradeNo(outTradeNo)
	if err != nil {
		log.Printf("[AlipayCallback] 根据订单号 %s 查找支付记录失败: %v", outTradeNo, err)
		return err
	}
	if fmt.Sprint(data["app_id"]) != s.alipayPagePayService.effectiveAppID() {
		return errors.New("alipay app identity mismatch")
	}
	totalAmount, err := strconv.ParseFloat(fmt.Sprint(data["total_amount"]), 64)
	if err != nil || math.Abs(totalAmount-payment.PaymentAmount) > 0.001 {
		return errors.New("alipay payment amount mismatch")
	}
	paymentID := payment.ID
	log.Printf("[AlipayCallback] 找到支付记录: paymentID=%s, orderID=%s", paymentID, payment.OrderID)

	if tradeStatus == "TRADE_SUCCESS" || tradeStatus == "TRADE_FINISHED" {
		if err := s.UpdatePaymentStatus(paymentID, "success", tradeNo); err != nil {
			log.Printf("[AlipayCallback] 更新支付状态失败: %v", err)
			return err
		}
		orderService := NewOrderService()
		if err := orderService.UpdateOrderStatus(payment.OrderID, "completed", "paid"); err != nil {
			log.Printf("[AlipayCallback] 更新订单状态失败: %v", err)
			return err
		}
		log.Printf("[AlipayCallback] 支付成功，订单 %s 已更新为 paid", payment.OrderID)
		return nil
	}

	log.Printf("[AlipayCallback] 支付未成功，trade_status=%s", tradeStatus)
	if err := s.UpdatePaymentStatus(paymentID, "failed", tradeNo); err != nil {
		return err
	}
	orderService := NewOrderService()
	if err := orderService.UpdateOrderStatus(payment.OrderID, "failed", "failed"); err != nil {
		return err
	}
	return nil
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
	var current model.PaymentRecord
	if err := database.DB.First(&current, "id = ?", id).Error; err != nil {
		return err
	}
	if current.PaymentStatus == status {
		return nil
	}
	allowed := (current.PaymentStatus == "pending" && (status == "success" || status == "failed")) ||
		(current.PaymentStatus == "failed" && status == "success") ||
		(current.PaymentStatus == "success" && status == "refunded")
	if !allowed {
		return fmt.Errorf("invalid payment status transition: %s -> %s", current.PaymentStatus, status)
	}
	updates := map[string]interface{}{
		"payment_status": status,
		"transaction_id": transactionID,
		"payment_time":   time.Now(),
		"updated_at":     time.Now(),
	}

	result := database.DB.Model(&model.PaymentRecord{}).
		Where("id = ? AND payment_status = ?", id, current.PaymentStatus).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("payment status changed concurrently")
	}
	return nil
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

	switch payment.PaymentMethod {
	case "wechat":
		result, err := s.wechatNativeService.RefundOrder(&payment.Order, amount, payment.TransactionID)
		if err != nil {
			return err
		}
		if result.ResultCode != "SUCCESS" {
			return fmt.Errorf("wechat refund failed: %s", result.ReturnMsg)
		}
	case "alipay":
		result, err := s.alipayPagePayService.RefundTrade(payment.Order.OrderNo, amount)
		if err != nil {
			return err
		}
		if result.AlipayTradeRefundResponse.RefundAmount == "" {
			return errors.New("alipay refund was not confirmed")
		}
	default:
		return errors.New("unsupported payment method")
	}

	if err := s.UpdatePaymentStatus(payment.ID, "refunded", payment.TransactionID); err != nil {
		return err
	}
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
