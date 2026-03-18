package service

import (
	"bytes"
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/skip2/go-qrcode"
)

type AlipayTradePagePayParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	NotifyURL  string `json:"notify_url"`
	ReturnURL  string `json:"return_url"`
	BizContent string `json:"biz_content"`
	Sign       string `json:"sign"`
}

type AlipayTradePagePayBizContent struct {
	OutTradeNo     string `json:"out_trade_no"`
	ProductCode    string `json:"product_code"`
	TotalAmount    string `json:"total_amount"`
	Subject        string `json:"subject"`
	Body           string `json:"body,omitempty"`
	TimeoutExpress string `json:"timeout_express,omitempty"`
}

type AlipayTradePagePayResponse struct {
	AlipayTradePagePayResponse struct {
		TradeNo    string `json:"trade_no"`
		OutTradeNo string `json:"out_trade_no"`
	} `json:"alipay_trade_page_pay_response"`
	Sign string `json:"sign"`
}

type AlipayTradePrecreateParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	NotifyURL  string `json:"notify_url"`
	BizContent string `json:"biz_content"`
	Sign       string `json:"sign"`
}

type AlipayTradePrecreateBizContent struct {
	OutTradeNo     string `json:"out_trade_no"`
	TotalAmount    string `json:"total_amount"`
	Subject        string `json:"subject"`
	Body           string `json:"body,omitempty"`
	TimeoutExpress string `json:"timeout_express,omitempty"`
}

type AlipayTradePrecreateResponse struct {
	AlipayTradePrecreateResponse struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		OutTradeNo string `json:"out_trade_no"`
		QrCode     string `json:"qr_code"`
	} `json:"alipay_trade_precreate_response"`
	Sign string `json:"sign"`
}

type AlipayTradeQueryResponse struct {
	AlipayTradeQueryResponse struct {
		TradeNo        string `json:"trade_no"`
		OutTradeNo     string `json:"out_trade_no"`
		TradeStatus    string `json:"trade_status"`
		TotalAmount    string `json:"total_amount"`
		BuyerPayAmount string `json:"buyer_pay_amount"`
		InvoiceAmount  string `json:"invoice_amount"`
		GmtPayment     string `json:"gmt_payment"`
	} `json:"alipay_trade_query_response"`
	Sign string `json:"sign"`
}

type AlipayTradeRefundResponse struct {
	AlipayTradeRefundResponse struct {
		TradeNo      string `json:"trade_no"`
		OutTradeNo   string `json:"out_trade_no"`
		RefundAmount string `json:"refund_amount"`
	} `json:"alipay_trade_refund_response"`
	Sign string `json:"sign"`
}

type AlipayTradeRefundParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	BizContent string `json:"biz_content"`
	Sign       string `json:"sign"`
}

type AlipayTradeRefundBizContent struct {
	OutTradeNo   string `json:"out_trade_no"`
	RefundAmount string `json:"refund_amount"`
	RefundReason string `json:"refund_reason,omitempty"`
	OutRefundNo  string `json:"out_refund_no"`
}

type AlipayNotifyResponse struct {
	NotifyTime     string `json:"notify_time"`
	NotifyType     string `json:"notify_type"`
	NotifyID       string `json:"notify_id"`
	AppID          string `json:"app_id"`
	Charset        string `json:"charset"`
	Version        string `json:"version"`
	SignType       string `json:"sign_type"`
	Sign           string `json:"sign"`
	OutTradeNo     string `json:"out_trade_no"`
	TradeNo        string `json:"trade_no"`
	TradeStatus    string `json:"trade_status"`
	TotalAmount    string `json:"total_amount"`
	BuyerPayAmount string `json:"buyer_pay_amount"`
	InvoiceAmount  string `json:"invoice_amount"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	GmtPayment     string `json:"gmt_payment"`
}

type AlipayPagePayService struct {
	config     *config.Config
	httpClient *http.Client
}

func NewAlipayPagePayService(cfg *config.Config) *AlipayPagePayService {
	return &AlipayPagePayService{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *AlipayPagePayService) GetGatewayURL() string {
	if s.config.Alipay.SandboxEnabled {
		return s.config.Alipay.SandboxGatewayURL
	}
	return s.config.Alipay.GatewayURL
}

func (s *AlipayPagePayService) readPrivateKeyFromFile(filePath string) ([]byte, error) {
	// 尝试不同的路径格式
	pathsToTry := []string{
		filePath,
		filepath.Join("..", filePath),
		filepath.Join("conf", "cert", filepath.Base(filePath)),
	}

	for _, path := range pathsToTry {
		data, err := ioutil.ReadFile(path)
		if err == nil {
			log.Printf("[readPrivateKeyFromFile] Successfully read private key from: %s", path)
			return data, nil
		}
	}

	// 如果都失败，返回空
	log.Printf("[readPrivateKeyFromFile] Failed to read private key from any path, filePath: %s", filePath)
	return nil, fmt.Errorf("failed to read private key from file: %s", filePath)
}

func (s *AlipayPagePayService) readPublicKeyFromFile(filePath string) ([]byte, error) {
	// 尝试不同的路径格式
	pathsToTry := []string{
		filePath,
		filepath.Join("..", filePath),
		filepath.Join("conf", "cert", filepath.Base(filePath)),
	}

	for _, path := range pathsToTry {
		data, err := ioutil.ReadFile(path)
		if err == nil {
			log.Printf("[readPublicKeyFromFile] Successfully read public key from: %s", path)
			return data, nil
		}
	}

	// 如果都失败，返回空
	log.Printf("[readPublicKeyFromFile] Failed to read public key from any path, filePath: %s", filePath)
	return nil, fmt.Errorf("failed to read public key from file: %s", filePath)
}

func (s *AlipayPagePayService) GenerateNonceStr() string {
	rand.Seed(time.Now().UnixNano())
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// effectiveSignType 获取实际生效的签名类型（沙箱优先，默认 RSA2）
func (s *AlipayPagePayService) effectiveSignType() string {
	if s.config.Alipay.SandboxEnabled && s.config.Alipay.SandboxSignType != "" {
		st := strings.ToUpper(strings.TrimSpace(s.config.Alipay.SandboxSignType))
		if st == "RSA" {
			return "RSA"
		}
		return "RSA2"
	}
	st := strings.ToUpper(strings.TrimSpace(s.config.Alipay.SignType))
	if st == "RSA" {
		return "RSA"
	}
	return "RSA2"
}

// looksLikeFilePath 判断字符串是否看起来像文件路径而非 base64 私钥。
// Base64 私钥通常长度 > 200，而合法文件路径一般很短且带路径分隔符前缀或文件扩展名。
func looksLikeFilePath(s string) bool {
	if len(s) > 200 {
		return false // 太长，肯定不是路径
	}
	if strings.Contains(s, "-----BEGIN") {
		return false // PEM 格式，直接解析
	}
	// Unix/Windows 绝对路径
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	// Windows 绝对路径（C:\... 或 C:/...）
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	// 相对路径且带常见密钥扩展名
	if strings.HasSuffix(s, ".pem") || strings.HasSuffix(s, ".key") || strings.HasSuffix(s, ".txt") {
		return true
	}
	return false
}

// parsePrivateKey 解析 RSA 私钥（支持裸 base64、PKCS1 PEM、PKCS8 PEM）
func parsePrivateKey(keyStr string) (*rsa.PrivateKey, error) {
	keyStr = strings.TrimSpace(keyStr)
	// 如果不含 PEM 头，自动添加（先尝试 PKCS8，再尝试 PKCS1）
	if !strings.Contains(keyStr, "-----BEGIN") {
		// 尝试 PKCS8
		p8 := "-----BEGIN PRIVATE KEY-----\n" + keyStr + "\n-----END PRIVATE KEY-----"
		if blk, _ := pem.Decode([]byte(p8)); blk != nil {
			if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
				if rk, ok := k.(*rsa.PrivateKey); ok {
					return rk, nil
				}
			}
		}
		// 尝试 PKCS1
		p1 := "-----BEGIN RSA PRIVATE KEY-----\n" + keyStr + "\n-----END RSA PRIVATE KEY-----"
		if blk, _ := pem.Decode([]byte(p1)); blk != nil {
			if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
				return k, nil
			}
		}
		return nil, fmt.Errorf("无法解析私钥：既不是 PKCS8 也不是 PKCS1 格式")
	}
	blk, _ := pem.Decode([]byte(keyStr))
	if blk == nil {
		return nil, fmt.Errorf("PEM 解码失败")
	}
	// PKCS8
	if k, err := x509.ParsePKCS8PrivateKey(blk.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
	}
	// PKCS1
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("私钥格式无法识别（尝试了 PKCS8 和 PKCS1）")
}

func (s *AlipayPagePayService) GenerateSign(params map[string]string) (string, error) {
	var keys []string
	for k := range params {
		if k != "sign" && params[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var signStr bytes.Buffer
	for i, k := range keys {
		if i > 0 {
			signStr.WriteString("&")
		}
		signStr.WriteString(k)
		signStr.WriteString("=")
		signStr.WriteString(params[k])
	}

	signType := s.effectiveSignType()
	log.Printf("[GenerateSign] AppID: %s, SignType: %s", s.effectiveAppID(), signType)

	privateKeyData := strings.TrimSpace(s.effectivePrivateKey())
	// 仅当看起来是文件系统路径时才读文件
	// Base64 私钥很长（>100字符）且以字母/数字开头，不会是文件路径
	if looksLikeFilePath(privateKeyData) {
		var readErr error
		privateKeyData, readErr = s.readPrivateKeyFileContent(privateKeyData)
		if readErr != nil {
			return "", fmt.Errorf("读取私钥文件失败: %w", readErr)
		}
	}

	key, err := parsePrivateKey(privateKeyData)
	if err != nil {
		return "", fmt.Errorf("解析私钥失败: %w", err)
	}

	var signature []byte
	if signType == "RSA" {
		// RSA = SHA1WithRSA（支付宝老版本）
		h := sha1.New()
		h.Write(signStr.Bytes())
		signature, err = rsa.SignPKCS1v15(crand.Reader, key, crypto.SHA1, h.Sum(nil))
	} else {
		// RSA2 = SHA256WithRSA（推荐）
		h := sha256.New()
		h.Write(signStr.Bytes())
		signature, err = rsa.SignPKCS1v15(crand.Reader, key, crypto.SHA256, h.Sum(nil))
	}
	if err != nil {
		return "", fmt.Errorf("签名失败: %w", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

func (s *AlipayPagePayService) readPrivateKeyFileContent(filePath string) (string, error) {
	// 尝试不同的路径格式
	pathsToTry := []string{
		filePath,
		filepath.Join("..", filePath),
		filepath.Join("conf", "cert", filepath.Base(filePath)),
	}

	for _, path := range pathsToTry {
		data, err := ioutil.ReadFile(path)
		if err == nil {
			log.Printf("[readPrivateKeyFileContent] Successfully read private key from: %s", path)
			return string(data), nil
		}
	}

	return "", fmt.Errorf("failed to read private key from any path, filePath: %s", filePath)
}

func (s *AlipayPagePayService) readPublicKeyFileContent(filePath string) (string, error) {
	// 尝试不同的路径格式
	pathsToTry := []string{
		filePath,
		filepath.Join("..", filePath),
		filepath.Join("conf", "cert", filepath.Base(filePath)),
	}

	for _, path := range pathsToTry {
		data, err := ioutil.ReadFile(path)
		if err == nil {
			log.Printf("[readPublicKeyFileContent] Successfully read public key from: %s", path)
			return string(data), nil
		}
	}

	return "", fmt.Errorf("failed to read public key from any path, filePath: %s", filePath)
}

func (s *AlipayPagePayService) VerifySign(params map[string]string, sign string) error {
	// 检查公钥是否是文件路径
	publicKeyData := s.config.Alipay.AlipayPublicKey

	// 如果公钥看起来像文件路径（包含/或\且不是PEM格式），则从文件读取
	if (strings.Contains(publicKeyData, "/") || strings.Contains(publicKeyData, "\\")) &&
		!strings.Contains(publicKeyData, "-----BEGIN") {
		log.Printf("[VerifySign] Public key is a file path, reading from file: %s", publicKeyData)
		data, err := s.readPublicKeyFileContent(publicKeyData)
		if err != nil {
			return fmt.Errorf("failed to read public key file: %w", err)
		}
		publicKeyData = data
	}

	block, _ := pem.Decode([]byte(publicKeyData))
	if block == nil {
		return fmt.Errorf("failed to decode public key")
	}

	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	delete(params, "sign")
	var keys []string
	for k := range params {
		if params[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var signStr bytes.Buffer
	for i, k := range keys {
		if i > 0 {
			signStr.WriteString("&")
		}
		signStr.WriteString(k)
		signStr.WriteString("=")
		signStr.WriteString(params[k])
	}

	decodedSign, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return fmt.Errorf("failed to decode sign: %w", err)
	}

	h := sha256.New()
	h.Write(signStr.Bytes())
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, h.Sum(nil), decodedSign)
}

func (s *AlipayPagePayService) CreateTradePagePay(order *model.Order, paymentAmount float64) (string, error) {
	log.Printf("[CreateTradePagePay] 开始生成支付宝支付 - AppID: %s, Amount: %.2f", s.effectiveAppID(), paymentAmount)
	log.Printf("[CreateTradePagePay] NotifyURL: %s", s.effectiveNotifyURL())
	log.Printf("[CreateTradePagePay] ReturnURL: %s", s.effectiveReturnURL())
	log.Printf("[CreateTradePagePay] GatewayURL: %s", s.GetGatewayURL())

	// 配置不完整或私钥无效时降级为演示 URL
	if !s.isAlipayConfigComplete() {
		demo := s.demoAlipayQrContent(order, paymentAmount)
		log.Printf("[CreateTradePagePay] 支付宝配置不完整/私钥无效，返回演示 URL: %s", demo)
		return demo, nil
	}

	subject := "论文格式检查服务"
	body := "论文格式检查服务"
	if order.MemberLevel != nil {
		subject = fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName)
		body = fmt.Sprintf("购买%s会员，有效期%d天", order.MemberLevel.LevelName, order.MemberLevel.DurationDays)
		if order.MemberLevel.Description != "" {
			body = order.MemberLevel.Description
		}
	}

	// 计算最小金额
	amount := paymentAmount
	if amount <= 0 {
		amount = 0.01
	}

	bizContent := AlipayTradePagePayBizContent{
		OutTradeNo:     order.OrderNo,
		ProductCode:    "FAST_INSTANT_TRADE_PAY",
		TotalAmount:    fmt.Sprintf("%.2f", amount),
		Subject:        subject,
		Body:           body,
		TimeoutExpress: "30m",
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.effectiveAppID(),
		"method":      "alipay.trade.page.pay",
		"charset":     "utf-8",
		"sign_type":   s.effectiveSignType(),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  s.effectiveNotifyURL(),
		"return_url":  s.effectiveReturnURL(),
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.GenerateSign(params)
	if err != nil {
		return "", fmt.Errorf("failed to generate sign: %w", err)
	}
	params["sign"] = sign
	log.Printf("[CreateTradePagePay] Sign generated successfully, sign length: %d", len(sign))
	log.Printf("[CreateTradePagePay] All params: %v", params)

	var queryParts []string
	for k, v := range params {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}

	paymentURL := fmt.Sprintf("%s?%s", s.GetGatewayURL(), strings.Join(queryParts, "&"))
	log.Printf("[CreateTradePagePay] Final payment URL length: %d", len(paymentURL))
	log.Printf("[CreateTradePagePay] Payment URL (first 500 chars): %s", paymentURL[:minInt(500, len(paymentURL))])

	return paymentURL, nil
}

// effectiveAppID 返回当前生效的 AppID（按沙箱开关）
func (s *AlipayPagePayService) effectiveAppID() string {
	if s.config.Alipay.SandboxEnabled && s.config.Alipay.SandboxAppID != "" {
		return s.config.Alipay.SandboxAppID
	}
	return s.config.Alipay.AppID
}

// effectivePrivateKey 返回当前生效的私钥（按沙箱开关）
func (s *AlipayPagePayService) effectivePrivateKey() string {
	if s.config.Alipay.SandboxEnabled && s.config.Alipay.SandboxAppPrivateKey != "" {
		return s.config.Alipay.SandboxAppPrivateKey
	}
	return s.config.Alipay.AppPrivateKey
}

// effectiveNotifyURL 返回当前生效的异步通知地址
func (s *AlipayPagePayService) effectiveNotifyURL() string {
	if s.config.Alipay.SandboxEnabled && s.config.Alipay.SandboxNotifyURL != "" {
		return s.config.Alipay.SandboxNotifyURL
	}
	return s.config.Alipay.NotifyURL
}

// effectiveReturnURL 返回当前生效的同步跳转地址
func (s *AlipayPagePayService) effectiveReturnURL() string {
	if s.config.Alipay.SandboxEnabled && s.config.Alipay.SandboxReturnURL != "" {
		return s.config.Alipay.SandboxReturnURL
	}
	return s.config.Alipay.ReturnURL
}

// isAlipayConfigComplete 检查支付宝配置是否完整且私钥可用
func (s *AlipayPagePayService) isAlipayConfigComplete() bool {
	appID := s.effectiveAppID()
	rawKey := strings.TrimSpace(s.effectivePrivateKey())
	if appID == "" || rawKey == "" {
		return false
	}
	// 过滤占位符
	if strings.Contains(rawKey, "在此填入") || strings.Contains(rawKey, "TODO") || len(rawKey) < 64 {
		return false
	}
	// 如果是文件路径，尝试读取后再解析
	keyStr := rawKey
	if looksLikeFilePath(rawKey) {
		data, err := s.readPrivateKeyFileContent(rawKey)
		if err != nil {
			log.Printf("[isAlipayConfigComplete] 私钥文件读取失败: %v", err)
			return false
		}
		keyStr = data
	}
	if _, err := parsePrivateKey(keyStr); err != nil {
		log.Printf("[isAlipayConfigComplete] 私钥解析失败: %v", err)
		return false
	}
	return true
}

// demoAlipayQrContent 返回演示用的二维码内容（不能真实付款，仅作 UI 展示）
func (s *AlipayPagePayService) demoAlipayQrContent(order *model.Order, amount float64) string {
	return fmt.Sprintf("https://qr.alipay.com/demo?order=%s&amount=%.2f&t=%d",
		order.OrderNo, amount, time.Now().Unix())
}

func (s *AlipayPagePayService) CreateTradePrecreate(order *model.Order, paymentAmount float64) (string, error) {
	log.Printf("[CreateTradePrecreate] 开始生成支付宝扫码支付 - AppID: %s, Amount: %.2f", s.effectiveAppID(), paymentAmount)
	log.Printf("[CreateTradePrecreate] NotifyURL: %s", s.effectiveNotifyURL())
	log.Printf("[CreateTradePrecreate] GatewayURL: %s", s.GetGatewayURL())

	// 配置不完整或私钥无效时降级为演示二维码
	if !s.isAlipayConfigComplete() {
		demo := s.demoAlipayQrContent(order, paymentAmount)
		log.Printf("[CreateTradePrecreate] 支付宝配置不完整/私钥无效，返回演示二维码: %s", demo)
		return demo, nil
	}

	subject := "论文格式检查服务"
	body := "论文格式检查服务"
	if order.MemberLevel != nil {
		subject = fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName)
		body = fmt.Sprintf("购买%s会员，有效期%d天", order.MemberLevel.LevelName, order.MemberLevel.DurationDays)
		if order.MemberLevel.Description != "" {
			body = order.MemberLevel.Description
		}
	}

	// 计算最小金额
	amount := paymentAmount
	if amount <= 0 {
		amount = 0.01
	}

	bizContent := AlipayTradePrecreateBizContent{
		OutTradeNo:     order.OrderNo,
		TotalAmount:    fmt.Sprintf("%.2f", amount),
		Subject:        subject,
		Body:           body,
		TimeoutExpress: "30m",
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.effectiveAppID(),
		"method":      "alipay.trade.precreate",
		"charset":     "utf-8",
		"sign_type":   s.effectiveSignType(),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  s.effectiveNotifyURL(),
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.GenerateSign(params)
	if err != nil {
		return "", fmt.Errorf("failed to generate sign: %w", err)
	}
	params["sign"] = sign
	log.Printf("[CreateTradePrecreate] Sign generated successfully, sign length: %d", len(sign))

	var queryParts []string
	for k, v := range params {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}

	requestURL := fmt.Sprintf("%s?%s", s.GetGatewayURL(), strings.Join(queryParts, "&"))
	log.Printf("[CreateTradePrecreate] Request URL (first 500 chars): %s", requestURL[:minInt(500, len(requestURL))])

	// 发送请求到支付宝
	resp, err := s.httpClient.Get(requestURL)
	if err != nil {
		return "", fmt.Errorf("failed to request alipay precreate api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("[CreateTradePrecreate] Alipay response: %s", string(bodyBytes))

	result := &AlipayTradePrecreateResponse{}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.AlipayTradePrecreateResponse.Code != "10000" {
		return "", fmt.Errorf("alipay precreate failed: code=%s, msg=%s",
			result.AlipayTradePrecreateResponse.Code,
			result.AlipayTradePrecreateResponse.Msg)
	}

	log.Printf("[CreateTradePrecreate] 成功获取二维码: %s", result.AlipayTradePrecreateResponse.QrCode)
	return result.AlipayTradePrecreateResponse.QrCode, nil
}

func (s *AlipayPagePayService) QueryTrade(orderNo string) (*AlipayTradeQueryResponse, error) {
	if !s.isAlipayConfigComplete() {
		return nil, fmt.Errorf("alipay payment config is incomplete")
	}

	bizContent := map[string]string{
		"out_trade_no": orderNo,
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.effectiveAppID(),
		"method":      "alipay.trade.query",
		"charset":     "utf-8",
		"sign_type":   s.effectiveSignType(),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.GenerateSign(params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate sign: %w", err)
	}
	params["sign"] = sign

	queryURL := s.buildQueryURL(params)

	resp, err := s.httpClient.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to request alipay query api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &AlipayTradeQueryResponse{}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

func (s *AlipayPagePayService) RefundTrade(orderNo string, refundAmount float64) (*AlipayTradeRefundResponse, error) {
	if !s.isAlipayConfigComplete() {
		return nil, fmt.Errorf("alipay payment config is incomplete")
	}

	refundNo := fmt.Sprintf("refund_%s_%d", orderNo, time.Now().UnixNano())

	bizContent := AlipayTradeRefundBizContent{
		OutTradeNo:   orderNo,
		RefundAmount: fmt.Sprintf("%.2f", refundAmount),
		RefundReason: "用户退款",
		OutRefundNo:  refundNo,
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.effectiveAppID(),
		"method":      "alipay.trade.refund",
		"charset":     "utf-8",
		"sign_type":   s.effectiveSignType(),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.GenerateSign(params)
	if err != nil {
		return nil, fmt.Errorf("failed to generate sign: %w", err)
	}
	params["sign"] = sign

	queryURL := s.buildQueryURL(params)

	resp, err := s.httpClient.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to request alipay refund api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &AlipayTradeRefundResponse{}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result, nil
}

func (s *AlipayPagePayService) HandleNotify(params map[string]string) (*AlipayNotifyResponse, error) {
	sign, ok := params["sign"]
	if !ok {
		return nil, fmt.Errorf("sign not found in params")
	}

	if err := s.VerifySign(params, sign); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return &AlipayNotifyResponse{
		NotifyTime:     params["notify_time"],
		NotifyType:     params["notify_type"],
		NotifyID:       params["notify_id"],
		AppID:          params["app_id"],
		Charset:        params["charset"],
		Version:        params["version"],
		SignType:       params["sign_type"],
		Sign:           sign,
		OutTradeNo:     params["out_trade_no"],
		TradeNo:        params["trade_no"],
		TradeStatus:    params["trade_status"],
		TotalAmount:    params["total_amount"],
		BuyerPayAmount: params["buyer_pay_amount"],
		InvoiceAmount:  params["invoice_amount"],
		Subject:        params["subject"],
		Body:           params["body"],
		GmtPayment:     params["gmt_payment"],
	}, nil
}

func (s *AlipayPagePayService) buildQueryURL(params map[string]string) string {
	var queryParts []string
	for k, v := range params {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}
	return fmt.Sprintf("%s?%s", s.GetGatewayURL(), strings.Join(queryParts, "&"))
}

func (s *AlipayPagePayService) GenerateQRCodeImageURL(paymentURL string, width int) string {
	if width == 0 {
		width = 256
	}
	encodedURL := url.QueryEscape(paymentURL)
	return fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=%dx%d&data=%s", width, width, encodedURL)
}

func (s *AlipayPagePayService) GenerateQRCodeImage(data string, width int) []byte {
	qrCode, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return nil
	}
	if width == 0 {
		width = 256
	}
	img := qrCode.Image(width)
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func (s *AlipayPagePayService) CloseTrade(orderNo string) error {
	if !s.isAlipayConfigComplete() {
		return fmt.Errorf("alipay payment config is incomplete")
	}

	bizContent := map[string]string{
		"out_trade_no": orderNo,
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.effectiveAppID(),
		"method":      "alipay.trade.close",
		"charset":     "utf-8",
		"sign_type":   s.effectiveSignType(),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContentJSON),
	}

	sign, err := s.GenerateSign(params)
	if err != nil {
		return fmt.Errorf("failed to generate sign: %w", err)
	}
	params["sign"] = sign

	queryURL := s.buildQueryURL(params)

	resp, err := s.httpClient.Get(queryURL)
	if err != nil {
		return fmt.Errorf("failed to request alipay close api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		AlipayTradeCloseResponse struct {
			TradeNo    string `json:"trade_no"`
			OutTradeNo string `json:"out_trade_no"`
		} `json:"alipay_trade_close_response"`
	}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}
