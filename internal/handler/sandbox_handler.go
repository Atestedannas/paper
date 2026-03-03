package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/database"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

type SandboxHandler struct {
	wechatSandboxService *service.WechatSandboxService
	alipaySandboxService *service.AlipaySandboxService
	orderService         service.OrderService
}

func NewSandboxHandler(cfg *config.Config) *SandboxHandler {
	return &SandboxHandler{
		wechatSandboxService: service.NewWechatSandboxService(cfg),
		alipaySandboxService: service.NewAlipaySandboxService(cfg),
		orderService:         service.NewOrderService(),
	}
}

type SandboxTestRequest struct {
	OrderID       uuid.UUID `json:"order_id" binding:"required"`
	PaymentMethod string    `json:"payment_method" binding:"required,oneof=wechat alipay"`
	ClientIP      string    `json:"client_ip"`
}

type SandboxTestResponse struct {
	SandboxEnabled bool                   `json:"sandbox_enabled"`
	PaymentMethod  string                 `json:"payment_method"`
	PaymentParams  map[string]interface{} `json:"payment_params"`
	PaymentURL     string                 `json:"payment_url,omitempty"`
	OrderInfo      map[string]interface{} `json:"order_info"`
	Instructions   string                 `json:"instructions"`
}

func (h *SandboxHandler) CreateSandboxPayment(c *gin.Context) {
	var req SandboxTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	clientIP := req.ClientIP
	if clientIP == "" {
		clientIP = c.ClientIP()
	}

	order, err := h.orderService.GetOrderByID(req.OrderID)
	if err != nil {
		utils.NotFound(c, "order not found")
		return
	}

	var result SandboxTestResponse
	var paymentURL string

	switch req.PaymentMethod {
	case "wechat":
		if !h.wechatSandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "wechat sandbox is not enabled")
			return
		}

		orderResult, err := h.wechatSandboxService.CreateSandboxOrder(order, clientIP)
		if err != nil {
			utils.InternalServerError(c, "failed to create wechat sandbox order: "+err.Error())
			return
		}

		jsapiParams, err := h.wechatSandboxService.GenerateJSAPIParams(orderResult.PrepayID)
		if err != nil {
			utils.InternalServerError(c, "failed to generate jsapi params: "+err.Error())
			return
		}

		qrCode, _ := h.wechatSandboxService.GeneratePaymentQRCode(orderResult.PrepayID)

		result = SandboxTestResponse{
			SandboxEnabled: true,
			PaymentMethod:  "wechat",
			PaymentParams:  jsapiParams,
			PaymentURL:     qrCode,
			OrderInfo:      h.getOrderInfoMap(order),
			Instructions:   h.getWechatSandboxInstructions(orderResult.PrepayID),
		}

	case "alipay":
		if !h.alipaySandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "alipay sandbox is not enabled")
			return
		}

		paymentURL, err = h.alipaySandboxService.CreateTradePagePay(order)
		if err != nil {
			utils.InternalServerError(c, "failed to create alipay sandbox order: "+err.Error())
			return
		}

		result = SandboxTestResponse{
			SandboxEnabled: true,
			PaymentMethod:  "alipay",
			PaymentParams:  map[string]interface{}{"payment_url": paymentURL},
			PaymentURL:     paymentURL,
			OrderInfo:      h.getOrderInfoMap(order),
			Instructions:   h.getAlipaySandboxInstructions(),
		}

	default:
		utils.BadRequest(c, "unsupported payment method")
		return
	}

	utils.Success(c, result)
}

func (h *SandboxHandler) QuerySandboxPayment(c *gin.Context) {
	var req struct {
		OrderNo       string `json:"order_no" binding:"required"`
		PaymentMethod string `json:"payment_method" binding:"required,oneof=wechat alipay"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	switch req.PaymentMethod {
	case "wechat":
		if !h.wechatSandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "wechat sandbox is not enabled")
			return
		}

		result, err := h.wechatSandboxService.QueryOrder(req.OrderNo)
		if err != nil {
			utils.InternalServerError(c, "failed to query wechat order: "+err.Error())
			return
		}

		utils.Success(c, gin.H{
			"payment_method":    "wechat",
			"order_no":          req.OrderNo,
			"trade_status":      result.TradeState,
			"trade_status_desc": result.TradeStateDesc,
			"transaction_id":    result.TransactionID,
			"total_fee":         result.TotalFee,
		})

	case "alipay":
		if !h.alipaySandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "alipay sandbox is not enabled")
			return
		}

		result, err := h.alipaySandboxService.QueryTrade(req.OrderNo)
		if err != nil {
			utils.InternalServerError(c, "failed to query alipay order: "+err.Error())
			return
		}

		utils.Success(c, gin.H{
			"payment_method": "alipay",
			"order_no":       req.OrderNo,
			"trade_status":   result.AlipayTradeQueryResponse.TradeStatus,
			"trade_no":       result.AlipayTradeQueryResponse.TradeNo,
			"total_amount":   result.AlipayTradeQueryResponse.TotalAmount,
			"buyer_p_amount": result.AlipayTradeQueryResponse.BuyerPayAmount,
		})

	default:
		utils.BadRequest(c, "unsupported payment method")
		return
	}
}

func (h *SandboxHandler) SimulateWechatPayment(c *gin.Context) {
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	if !h.wechatSandboxService.IsSandboxEnabled() {
		utils.BadRequest(c, "wechat sandbox is not enabled")
		return
	}

	order, err := h.orderService.GetOrderByOrderNo(req.OrderNo)
	if err != nil {
		utils.NotFound(c, "order not found")
		return
	}

	simulateXML := h.generateWechatSimulateXML(order)

	utils.Success(c, gin.H{
		"payment_method":  "wechat",
		"order_no":        req.OrderNo,
		"simulate_result": "success",
		"simulate_xml":    simulateXML,
		"instructions":    "在微信支付测试工具中导入此XML数据以模拟支付回调",
	})
}

func (h *SandboxHandler) SimulateAlipayPayment(c *gin.Context) {
	var req struct {
		OrderNo string `json:"order_no" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	if !h.alipaySandboxService.IsSandboxEnabled() {
		utils.BadRequest(c, "alipay sandbox is not enabled")
		return
	}

	order, err := h.orderService.GetOrderByOrderNo(req.OrderNo)
	if err != nil {
		utils.NotFound(c, "order not found")
		return
	}

	notifyParams := h.generateAlipaySimulateNotify(order)

	utils.Success(c, gin.H{
		"payment_method":  "alipay",
		"order_no":        req.OrderNo,
		"simulate_result": "success",
		"notify_params":   notifyParams,
		"instructions":    "在支付宝沙箱测试工具中使用此参数模拟支付回调",
	})
}

func (h *SandboxHandler) GetSandboxStatus(c *gin.Context) {
	utils.Success(c, gin.H{
		"wechat_sandbox_enabled": h.wechatSandboxService.IsSandboxEnabled(),
		"alipay_sandbox_enabled": h.alipaySandboxService.IsSandboxEnabled(),
		"wechat_sandbox_url":     h.wechatSandboxService.GetSandboxAPIURL(),
		"alipay_sandbox_url":     h.alipaySandboxService.GetGatewayURL(),
	})
}

func (h *SandboxHandler) GetSandboxConfig(c *gin.Context) {
	wechatConfig := map[string]interface{}{
		"enabled":                 h.wechatSandboxService.IsSandboxEnabled(),
		"app_id":                  "", // 不返回敏感信息
		"sandbox_sign_key_status": "configured",
	}

	alipayConfig := map[string]interface{}{
		"enabled":            h.alipaySandboxService.IsSandboxEnabled(),
		"sandbox_guide":      "https://opendocs.alipay.com/open/200/105311",
		"test_account_guide": "请登录支付宝开放平台沙箱环境获取买家账号和密码",
	}

	utils.Success(c, gin.H{
		"wechat": wechatConfig,
		"alipay": alipayConfig,
		"note": map[string]string{
			"wechat": "微信沙箱测试需要在微信开放平台申请沙箱资质",
			"alipay": "支付宝沙箱测试需要在支付宝开放平台创建沙箱应用",
		},
	})
}

func (h *SandboxHandler) CreateSandboxRefund(c *gin.Context) {
	var req struct {
		OrderID       uuid.UUID `json:"order_id" binding:"required"`
		PaymentMethod string    `json:"payment_method" binding:"required,oneof=wechat alipay"`
		RefundAmount  float64   `json:"refund_amount" binding:"required,gt=0"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	switch req.PaymentMethod {
	case "wechat":
		if !h.wechatSandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "wechat sandbox is not enabled")
			return
		}

		payment, err := h.getPaymentByOrderID(req.OrderID)
		if err != nil {
			utils.NotFound(c, "payment not found")
			return
		}

		result, err := h.wechatSandboxService.CreateRefundOrder(payment, req.RefundAmount)
		if err != nil {
			utils.InternalServerError(c, "failed to create refund: "+err.Error())
			return
		}

		utils.Success(c, gin.H{
			"payment_method": "wechat",
			"refund_result":  "success",
			"refund_id":      result.RefundID,
			"out_refund_no":  result.OutRefundNo,
		})

	case "alipay":
		if !h.alipaySandboxService.IsSandboxEnabled() {
			utils.BadRequest(c, "alipay sandbox is not enabled")
			return
		}

		order, err := h.orderService.GetOrderByID(req.OrderID)
		if err != nil {
			utils.NotFound(c, "order not found")
			return
		}

		result, err := h.alipaySandboxService.RefundTrade(order.OrderNo, req.RefundAmount)
		if err != nil {
			utils.InternalServerError(c, "failed to create refund: "+err.Error())
			return
		}

		utils.Success(c, gin.H{
			"payment_method": "alipay",
			"refund_result":  "success",
			"trade_no":       result.AlipayTradeRefundResponse.TradeNo,
			"refund_amount":  result.AlipayTradeRefundResponse.RefundAmount,
		})

	default:
		utils.BadRequest(c, "unsupported payment method")
		return
	}
}

func (h *SandboxHandler) GetSandboxTestGuide(c *gin.Context) {
	guide := map[string]interface{}{
		"wechat": map[string]interface{}{
			"title": "微信支付沙箱环境测试指南",
			"steps": []map[string]interface{}{
				{
					"step":        1,
					"action":      "申请微信沙箱资质",
					"description": "登录微信开放平台，在账户中心申请沙箱测试资质",
					"url":         "https://open.weixin.qq.com/",
				},
				{
					"step":        2,
					"action":      "配置沙箱参数",
					"description": "在.env文件中配置WECHAT_SANDBOX_ENABLED=true及相关密钥",
				},
				{
					"step":        3,
					"action":      "获取沙箱签名密钥",
					"description": "调用沙箱API获取sandbox_sign_key",
				},
				{
					"step":        4,
					"action":      "测试支付流程",
					"description": "使用沙箱测试工具模拟支付流程",
				},
			},
			"test_tools": []string{
				"微信支付沙箱工具",
				"微信开发者工具",
			},
		},
		"alipay": map[string]interface{}{
			"title": "支付宝沙箱环境测试指南",
			"steps": []map[string]interface{}{
				{
					"step":        1,
					"action":      "创建支付宝沙箱应用",
					"description": "登录支付宝开放平台，创建沙箱应用",
					"url":         "https://open.alipay.com/",
				},
				{
					"step":        2,
					"action":      "配置沙箱参数",
					"description": "在.env文件中配置ALIPAY_SANDBOX_ENABLED=true",
				},
				{
					"step":        3,
					"action":      "获取沙箱买家账号",
					"description": "在沙箱环境管理页面获取测试账号和密码",
				},
				{
					"step":        4,
					"action":      "测试支付流程",
					"description": "使用沙箱账号登录支付宝进行支付测试",
				},
			},
			"test_account": map[string]string{
				"note":      "请登录支付宝开放平台沙箱环境获取买家账号",
				"guide_url": "https://opendocs.alipay.com/open/200/105311",
			},
		},
	}

	utils.Success(c, guide)
}

func (h *SandboxHandler) getOrderInfoMap(order *model.Order) map[string]interface{} {
	return map[string]interface{}{
		"order_id":       order.ID,
		"order_no":       order.OrderNo,
		"total_amount":   order.TotalAmount,
		"member_level":   order.MemberLevel.LevelName,
		"payment_method": order.PaymentMethod,
		"payment_status": order.PaymentStatus,
	}
}

func (h *SandboxHandler) getWechatSandboxInstructions(prepayID string) string {
	return `
微信沙箱支付测试说明：

1. 获取支付参数后，可在微信开发者工具中进行测试
2. 扫描二维码或在微信中打开支付链接
3. 使用沙箱测试工具模拟支付成功回调
4. 测试完成后，订单状态将自动更新

注意：沙箱环境仅用于开发测试，不能用于正式生产环境。
`
}

func (h *SandboxHandler) getAlipaySandboxInstructions() string {
	return `
支付宝沙箱支付测试说明：

1. 获取支付链接后，在浏览器中打开
2. 使用沙箱买家账号登录（账号和密码在支付宝开放平台沙箱环境获取）
3. 完成支付流程
4. 支付完成后，系统将自动接收异步通知并更新订单状态

注意：沙箱环境仅用于开发测试，不能用于正式生产环境。
`
}

func (h *SandboxHandler) generateWechatSimulateXML(order *model.Order) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<xml>
  <appid><![CDATA[` + order.UserID.String() + `]]></appid>
  <mch_id><![CDATA[` + order.ID.String() + `]]></mch_id>
  <nonce_str><![CDATA[test_nonce_str]]></nonce_str>
  <result_code><![CDATA[SUCCESS]]></result_code>
  <return_code><![CDATA[SUCCESS]]></return_code>
  <return_msg><![CDATA[OK]]></return_msg>
  <sign><![CDATA[test_sign]]></sign>
  <out_trade_no><![CDATA[` + order.OrderNo + `]]></out_trade_no>
  <trade_state><![CDATA[SUCCESS]]></trade_state>
  <trade_state_desc><![CDATA[支付成功]]></trade_state_desc>
  <transaction_id><![CDATA[test_transaction_` + order.OrderNo + `]]></transaction_id>
  <total_fee><![CDATA[` + fmt.Sprintf("%.0f", order.TotalAmount*100) + `]]></total_fee>
</xml>`
}

func (h *SandboxHandler) generateAlipaySimulateNotify(order *model.Order) map[string]string {
	return map[string]string{
		"notify_type":      "trade_status_sync",
		"notify_id":        "test_notify_" + order.OrderNo,
		"notify_time":      "2024-01-01 12:00:00",
		"out_trade_no":     order.OrderNo,
		"trade_no":         "test_trade_" + order.OrderNo,
		"trade_status":     "TRADE_SUCCESS",
		"total_amount":     fmt.Sprintf("%.2f", order.TotalAmount),
		"buyer_pay_amount": fmt.Sprintf("%.2f", order.TotalAmount),
	}
}

func (h *SandboxHandler) getPaymentByOrderID(orderID uuid.UUID) (*model.PaymentRecord, error) {
	var payment model.PaymentRecord
	if err := database.DB.Where("order_id = ?", orderID).First(&payment).Error; err != nil {
		return nil, err
	}
	return &payment, nil
}
