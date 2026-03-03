package service

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
)

type AlipaySandboxTradePagePayParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	NotifyURL  string `json:"notify_url"`
	ReturnURL  string `json:"return_url"`
	BizContent string `json:"biz_content"`
}

type AlipaySandboxBizContent struct {
	OutTradeNo     string `json:"out_trade_no"`
	ProductCode    string `json:"product_code"`
	TotalAmount    string `json:"total_amount"`
	Subject        string `json:"subject"`
	Body           string `json:"body,omitempty"`
	TimeoutExpress string `json:"timeout_express,omitempty"`
}

type AlipaySandboxTradeQueryParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	BizContent string `json:"biz_content"`
}

type AlipaySandboxTradeRefundParams struct {
	AppID      string `json:"app_id"`
	Method     string `json:"method"`
	Charset    string `json:"charset"`
	SignType   string `json:"sign_type"`
	Timestamp  string `json:"timestamp"`
	Version    string `json:"version"`
	BizContent string `json:"biz_content"`
}

type AlipaySandboxResponse struct {
	AlipayTradePagePayResponse struct {
		OutTradeNo string `json:"out_trade_no"`
		TradeNo    string `json:"trade_no"`
	} `json:"alipay_trade_page_pay_response"`
	Sign string `json:"sign"`
}

type AlipaySandboxQueryResponse struct {
	AlipayTradeQueryResponse struct {
		OutTradeNo     string `json:"out_trade_no"`
		TradeNo        string `json:"trade_no"`
		TradeStatus    string `json:"trade_status"`
		TotalAmount    string `json:"total_amount"`
		BuyerPayAmount string `json:"buyer_pay_amount"`
	} `json:"alipay_trade_query_response"`
	Sign string `json:"sign"`
}

type AlipaySandboxRefundResponse struct {
	AlipayTradeRefundResponse struct {
		OutTradeNo   string `json:"out_trade_no"`
		TradeNo      string `json:"trade_no"`
		RefundAmount string `json:"refund_amount"`
	} `json:"alipay_trade_refund_response"`
	Sign string `json:"sign"`
}

type AlipaySandboxNotifyParams struct {
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

type AlipaySandboxService struct {
	config     *config.Config
	httpClient *http.Client
}

func NewAlipaySandboxService(cfg *config.Config) *AlipaySandboxService {
	return &AlipaySandboxService{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *AlipaySandboxService) IsSandboxEnabled() bool {
	return s.config.Alipay.SandboxEnabled
}

func (s *AlipaySandboxService) GetGatewayURL() string {
	if s.config.Alipay.SandboxEnabled {
		return s.config.Alipay.SandboxGatewayURL
	}
	return s.config.Alipay.GatewayURL
}

func (s *AlipaySandboxService) GenerateNonceStr() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *AlipaySandboxService) GenerateSign(params map[string]string) (string, error) {
	var keys []string
	for k := range params {
		if k != "sign" && params[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var signStr strings.Builder
	for i, k := range keys {
		if i > 0 {
			signStr.WriteString("&")
		}
		signStr.WriteString(k)
		signStr.WriteString("=")
		signStr.WriteString(params[k])
	}

	privateKey := s.config.Alipay.AppPrivateKey
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(signStr.String()))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", fmt.Errorf("failed to sign: %w", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

func (s *AlipaySandboxService) VerifySign(params map[string]string, sign string) error {
	publicKey := s.config.Alipay.AlipayPublicKey
	block, _ := pem.Decode([]byte(publicKey))
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

	var signStr strings.Builder
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
	h.Write([]byte(signStr.String()))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, h.Sum(nil), decodedSign)
}

func (s *AlipaySandboxService) CreateTradePagePay(order *model.Order) (string, error) {
	if !s.IsSandboxEnabled() {
		return "", fmt.Errorf("alipay sandbox is not enabled")
	}

	bizContent := AlipaySandboxBizContent{
		OutTradeNo:     order.OrderNo,
		ProductCode:    "FAST_INSTANT_TRADE_PAY",
		TotalAmount:    fmt.Sprintf("%.2f", order.TotalAmount),
		Subject:        fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName),
		Body:           fmt.Sprintf("购买%s会员，有效期%d天", order.MemberLevel.LevelName, order.MemberLevel.DurationDays),
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

	queryParams := url.Values{}
	for k, v := range params {
		queryParams.Add(k, v)
	}

	return fmt.Sprintf("%s?%s", s.GetGatewayURL(), queryParams.Encode()), nil
}

func (s *AlipaySandboxService) QueryTrade(outTradeNo string) (*AlipaySandboxQueryResponse, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("alipay sandbox is not enabled")
	}

	bizContent := map[string]string{
		"out_trade_no": outTradeNo,
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

	queryURL := fmt.Sprintf("%s?%s", s.GetGatewayURL(), s.buildQueryString(params))

	resp, err := s.httpClient.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to request alipay sandbox query api: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result AlipaySandboxQueryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

func (s *AlipaySandboxService) RefundTrade(outTradeNo string, refundAmount float64) (*AlipaySandboxRefundResponse, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("alipay sandbox is not enabled")
	}

	bizContent := map[string]string{
		"out_trade_no":  outTradeNo,
		"refund_amount": fmt.Sprintf("%.2f", refundAmount),
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

	queryURL := fmt.Sprintf("%s?%s", s.GetGatewayURL(), s.buildQueryString(params))

	resp, err := s.httpClient.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to request alipay sandbox refund api: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result AlipaySandboxRefundResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

func (s *AlipaySandboxService) HandleNotify(params map[string]string) (*AlipaySandboxNotifyParams, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("alipay sandbox is not enabled")
	}

	sign, ok := params["sign"]
	if !ok {
		return nil, fmt.Errorf("sign not found in params")
	}

	if err := s.VerifySign(params, sign); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return &AlipaySandboxNotifyParams{
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

func (s *AlipaySandboxService) BuildWapPayURL(order *model.Order) (string, error) {
	if !s.IsSandboxEnabled() {
		return "", fmt.Errorf("alipay sandbox is not enabled")
	}

	bizContent := AlipaySandboxBizContent{
		OutTradeNo:     order.OrderNo,
		ProductCode:    "QUICK_WAP_WAY",
		TotalAmount:    fmt.Sprintf("%.2f", order.TotalAmount),
		Subject:        fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName),
		Body:           fmt.Sprintf("购买%s会员，有效期%d天", order.MemberLevel.LevelName, order.MemberLevel.DurationDays),
		TimeoutExpress: "30m",
	}

	bizContentJSON, err := json.Marshal(bizContent)
	if err != nil {
		return "", fmt.Errorf("failed to marshal biz_content: %w", err)
	}

	params := map[string]string{
		"app_id":      s.config.Alipay.AppID,
		"method":      "alipay.trade.wap.pay",
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

	return fmt.Sprintf("%s?%s", s.GetGatewayURL(), s.buildQueryString(params)), nil
}

func (s *AlipaySandboxService) buildQueryString(params map[string]string) string {
	var queryParts []string
	for k, v := range params {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}
	return strings.Join(queryParts, "&")
}

func (s *AlipaySandboxService) GetSandboxTestAccount() map[string]string {
	return map[string]string{
		"app_id":       s.config.Alipay.AppID,
		"sandbox_url":  s.GetGatewayURL(),
		"test_account": "请登录支付宝开放平台沙箱环境获取测试账号",
		"guide":        "https://opendocs.alipay.com/open/200/105311",
	}
}

var _ = crypto.SHA256
var _ = x509.MarshalPKCS1PrivateKey
var _ = pem.Encode
