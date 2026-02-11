package handler

import (
	"io/ioutil"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/service"
	"github.com/paper-format-checker/backend/internal/utils"
)

// PaymentHandler 支付处理器
type PaymentHandler struct {
	paymentService service.PaymentService
}

// NewPaymentHandler 创建支付处理器实例
func NewPaymentHandler(config *config.Config) *PaymentHandler {
	return &PaymentHandler{
		paymentService: service.NewPaymentService(config),
	}
}

// CreatePayment 创建支付记录
func (h *PaymentHandler) CreatePayment(c *gin.Context) {
	// 解析请求数据
	var req struct {
		OrderID       uuid.UUID `json:"order_id" binding:"required"`
		PaymentMethod string    `json:"payment_method" binding:"required,oneof=wechat alipay"`
		PaymentType   string    `json:"payment_type"` // wechat: native, jsapi; alipay: page
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 设置默认支付类型
	if req.PaymentType == "" {
		if req.PaymentMethod == "wechat" {
			req.PaymentType = "native"
		} else {
			req.PaymentType = "page"
		}
	}

	// 验证支付类型
	validPaymentTypes := map[string][]string{
		"wechat": {"native", "jsapi"},
		"alipay": {"page"},
	}
	if !contains(validPaymentTypes[req.PaymentMethod], req.PaymentType) {
		utils.BadRequest(c, "invalid payment type")
		return
	}

	clientIP := c.ClientIP()

	payment, paymentParams, err := h.paymentService.CreatePayment(req.OrderID, req.PaymentMethod, req.PaymentType, clientIP)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	utils.Created(c, gin.H{
		"payment":        payment,
		"payment_params": paymentParams,
	})
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GenerateWechatPayment 生成微信支付参数 - 已合并到CreatePayment方法中
func (h *PaymentHandler) GenerateWechatPayment(c *gin.Context) {
	utils.BadRequest(c, "该方法已废弃，请使用CreatePayment")
}

// GenerateAlipayPayment 生成支付宝支付参数 - 已合并到CreatePayment方法中
func (h *PaymentHandler) GenerateAlipayPayment(c *gin.Context) {
	utils.BadRequest(c, "该方法已废弃，请使用CreatePayment")
}

// HandleWechatCallback 处理微信支付回调
func (h *PaymentHandler) HandleWechatCallback(c *gin.Context) {
	// 读取回调数据
	data, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	// 处理微信支付回调
	result, err := h.paymentService.HandleWeChatCallback(data)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	utils.Success(c, result)
}

// HandleAlipayCallback 处理支付宝支付回调
func (h *PaymentHandler) HandleAlipayCallback(c *gin.Context) {
	// 解析支付宝回调参数
	c.Request.ParseForm()
	params := make(map[string]interface{})
	for k, v := range c.Request.Form {
		params[k] = v[0]
	}

	// 处理支付宝支付回调
	result, err := h.paymentService.HandleAlipayCallback(params)
	if err != nil {
		utils.BadRequest(c, err.Error())
		return
	}

	utils.Success(c, result)
}

// GetPaymentByID 根据ID获取支付记录
func (h *PaymentHandler) GetPaymentByID(c *gin.Context) {
	// 解析支付ID
	paymentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid payment id")
		return
	}

	// 获取支付记录
	payment, err := h.paymentService.GetPaymentByID(paymentID)
	if err != nil {
		utils.NotFound(c, err.Error())
		return
	}

	utils.Success(c, payment)
}

// GetPayQrCode 获取支付二维码
func (h *PaymentHandler) GetPayQrCode(c *gin.Context) {
	// 解析订单ID
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequest(c, "invalid order id")
		return
	}

	// 获取支付类型
	payType := c.DefaultQuery("pay_type", "alipay")
	if payType != "wechat" && payType != "alipay" {
		utils.BadRequest(c, "invalid pay_type")
		return
	}

	// 获取二维码图片
	qrCode, err := h.paymentService.GetPayQrCode(orderID, payType)
	if err != nil {
		utils.InternalServerError(c, err.Error())
		return
	}

	// 返回图片
	c.Header("Content-Type", "image/png")
	c.Header("Content-Disposition", "inline; filename=qrcode.png")
	c.Data(200, "image/png", qrCode)
}
