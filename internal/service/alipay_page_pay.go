package service

import (
	"bytes"
	"crypto"
	crand "crypto/rand"
	"crypto/rsa"
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

	// 检查私钥是否是文件路径
	privateKeyData := s.config.Alipay.AppPrivateKey
	log.Printf("[GenerateSign] PrivateKey config length: %d, starts with: %s", len(privateKeyData), privateKeyData[:minInt(50, len(privateKeyData))])
	log.Printf("[GenerateSign] AppID: %s, SignType: RSA2", s.config.Alipay.AppID)

	// 如果私钥看起来像文件路径（包含/或\且不是PEM格式），则从文件读取
	if (strings.Contains(privateKeyData, "/") || strings.Contains(privateKeyData, "\\")) &&
		!strings.Contains(privateKeyData, "-----BEGIN") {
		log.Printf("[GenerateSign] Private key is a file path, reading from file: %s", privateKeyData)
		var err error
		privateKeyData, err = s.readPrivateKeyFileContent(privateKeyData)
		if err != nil {
			return "", fmt.Errorf("failed to read private key file: %w", err)
		}
		log.Printf("[GenerateSign] Successfully read private key from file, key length: %d", len(privateKeyData))
	}

	block, _ := pem.Decode([]byte(privateKeyData))
	if block == nil {
		return "", fmt.Errorf("failed to decode private key")
	}

	// 尝试解析PKCS#1格式，如果失败则尝试PKCS#8格式
	var key *rsa.PrivateKey
	var err error

	key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// 如果PKCS#1解析失败，尝试PKCS#8格式
		log.Printf("[GenerateSign] PKCS#1 parse failed (err: %v), trying PKCS#8 format", err)
		pkcs8Data, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return "", fmt.Errorf("failed to parse private key: PKCS#1: %w, PKCS#8: %v", err, parseErr)
		}
		// 类型断言为RSA私钥
		key = pkcs8Data.(*rsa.PrivateKey)
		log.Printf("[GenerateSign] Successfully parsed PKCS#8 format RSA private key")
	}

	h := sha256.New()
	h.Write(signStr.Bytes())
	signature, err := rsa.SignPKCS1v15(crand.Reader, key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
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
	log.Printf("[CreateTradePagePay] 开始生成支付宝支付 - AppID: %s, Amount: %.2f", s.config.Alipay.AppID, paymentAmount)
	log.Printf("[CreateTradePagePay] NotifyURL: %s", s.config.Alipay.NotifyURL)
	log.Printf("[CreateTradePagePay] ReturnURL: %s", s.config.Alipay.ReturnURL)
	log.Printf("[CreateTradePagePay] GatewayURL: %s", s.GetGatewayURL())

	// 如果配置不完整，返回模拟的支付 URL
	if s.config.Alipay.AppID == "" || s.config.Alipay.AppPrivateKey == "" || s.config.Alipay.AlipayPublicKey == "" {
		// 生成更完整的模拟支付宝支付 URL，包含必要的参数
		// 使用支付宝沙箱测试 APPID
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		subject := "论文格式检查服务"

		// 使用 url.Values 构建查询参数，确保所有参数都被正确编码
		queryParams := url.Values{}
		queryParams.Add("method", "alipay.trade.page.pay")
		queryParams.Add("app_id", "2016101500698584")
		queryParams.Add("charset", "utf-8")
		queryParams.Add("sign_type", "RSA2")
		queryParams.Add("timestamp", timestamp)
		queryParams.Add("version", "1.0")
		queryParams.Add("total_amount", fmt.Sprintf("%.2f", paymentAmount))
		queryParams.Add("out_trade_no", order.OrderNo)
		queryParams.Add("subject", subject)

		mockPaymentURL := fmt.Sprintf("https://openapi.alipaydev.com/gateway.do?%s", queryParams.Encode())
		log.Printf("[CreateTradePagePay] 支付宝配置不完整，返回模拟支付 URL: %s", mockPaymentURL)
		return mockPaymentURL, nil
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
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.page.pay",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  s.config.Alipay.NotifyURL,
		"return_url":  s.config.Alipay.ReturnURL,
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

func (s *AlipayPagePayService) CreateTradePrecreate(order *model.Order, paymentAmount float64) (string, error) {
	log.Printf("[CreateTradePrecreate] 开始生成支付宝扫码支付 - AppID: %s, Amount: %.2f", s.config.Alipay.AppID, paymentAmount)
	log.Printf("[CreateTradePrecreate] NotifyURL: %s", s.config.Alipay.NotifyURL)
	log.Printf("[CreateTradePrecreate] GatewayURL: %s", s.GetGatewayURL())

	// 如果配置不完整，返回模拟的二维码内容
	if s.config.Alipay.AppID == "" || s.config.Alipay.AppPrivateKey == "" || s.config.Alipay.AlipayPublicKey == "" {
		mockQrCode := fmt.Sprintf("https://qr.alipay.com/bax0888xxxxxxxxxx")
		log.Printf("[CreateTradePrecreate] 支付宝配置不完整，返回模拟二维码: %s", mockQrCode)
		return mockQrCode, nil
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
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.precreate",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  s.config.Alipay.NotifyURL,
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
	if s.config.Alipay.AppID == "" || s.config.Alipay.AppPrivateKey == "" || s.config.Alipay.AlipayPublicKey == "" {
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
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.query",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
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
	if s.config.Alipay.AppID == "" || s.config.Alipay.AppPrivateKey == "" || s.config.Alipay.AlipayPublicKey == "" {
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
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.refund",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
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
	if s.config.Alipay.AppID == "" || s.config.Alipay.AppPrivateKey == "" || s.config.Alipay.AlipayPublicKey == "" {
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
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.close",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
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
