package service

import (
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
)

type WechatSandboxPaymentParams struct {
	AppID          string `xml:"appid"`
	MchID          string `xml:"mch_id"`
	NonceStr       string `xml:"nonce_str"`
	Sign           string `xml:"sign"`
	Body           string `xml:"body"`
	OutTradeNo     string `xml:"out_trade_no"`
	TotalFee       int    `xml:"total_fee"`
	SpbillCreateIP string `xml:"spbill_create_ip"`
	NotifyURL      string `xml:"notify_url"`
	TradeType      string `xml:"trade_type"`
}

type WechatSandboxOrderResult struct {
	ReturnCode string `xml:"return_code"`
	ReturnMsg  string `xml:"return_msg"`
	AppID      string `xml:"appid"`
	MchID      string `xml:"mch_id"`
	NonceStr   string `xml:"nonce_str"`
	Sign       string `xml:"sign"`
	ResultCode string `xml:"result_code"`
	PrepayID   string `xml:"prepay_id"`
	TradeType  string `xml:"trade_type"`
}

type WechatSandboxCallback struct {
	XMLName    xml.Name `xml:"xml"`
	ReturnCode string   `xml:"return_code"`
	ReturnMsg  string   `xml:"return_msg"`
	AppID      string   `xml:"appid"`
	MchID      string   `xml:"mch_id"`
	NonceStr   string   `xml:"nonce_str"`
	Sign       string   `xml:"sign"`
	ResultCode string   `xml:"result_code"`
	OutTradeNo string   `xml:"out_trade_no"`
	TradeState string   `xml:"trade_state"`
	TotalFee   int      `xml:"total_fee"`
}

type WechatSandboxService struct {
	config     *config.Config
	httpClient *http.Client
}

func NewWechatSandboxService(cfg *config.Config) *WechatSandboxService {
	return &WechatSandboxService{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *WechatSandboxService) IsSandboxEnabled() bool {
	return s.config.WechatSandbox.Enabled
}

func (s *WechatSandboxService) GetSandboxAPIURL() string {
	return "https://api.mch.weixin.qq.com/sandbox/pay/unifiedorder"
}

func (s *WechatSandboxService) GetSandboxSignKeyURL() string {
	return "https://api.mch.weixin.qq.com/sandbox/pay/getsignkey"
}

func (s *WechatSandboxService) GenerateNonceStr() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *WechatSandboxService) GenerateSign(params map[string]string, apiKey string) string {
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
	signStr.WriteString("&key=")
	signStr.WriteString(apiKey)

	hash := md5.Sum([]byte(signStr.String()))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func (s *WechatSandboxService) CreateSandboxOrder(order *model.Order, clientIP string) (*WechatSandboxOrderResult, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("wechat sandbox is not enabled")
	}

	params := map[string]string{
		"appid":            s.config.Wechat.AppID,
		"mch_id":           s.config.Wechat.MchID,
		"nonce_str":        s.GenerateNonceStr(),
		"body":             fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName),
		"out_trade_no":     order.OrderNo,
		"total_fee":        fmt.Sprintf("%.0f", order.TotalAmount*100),
		"spbill_create_ip": clientIP,
		"notify_url":       s.config.Wechat.NotifyURL,
		"trade_type":       "JSAPI",
	}

	signKey := s.config.WechatSandbox.SandboxSignKey
	if signKey == "" {
		signKey = s.config.Wechat.ApiKey
	}
	params["sign"] = s.GenerateSign(params, signKey)

	xmlData, err := s.mapToXML(params)
	if err != nil {
		return nil, fmt.Errorf("failed to convert params to xml: %w", err)
	}

	resp, err := s.httpClient.Post(s.GetSandboxAPIURL(), "text/xml", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat sandbox api: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result WechatSandboxOrderResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat sandbox api error: %s", result.ReturnMsg)
	}

	return &result, nil
}

func (s *WechatSandboxService) GenerateJSAPIParams(prepayID string) (map[string]interface{}, error) {
	nonceStr := s.GenerateNonceStr()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	params := map[string]string{
		"appId":     s.config.Wechat.AppID,
		"timeStamp": timestamp,
		"nonceStr":  nonceStr,
		"package":   fmt.Sprintf("prepay_id=%s", prepayID),
		"signType":  "MD5",
	}

	signKey := s.config.WechatSandbox.SandboxSignKey
	if signKey == "" {
		signKey = s.config.Wechat.ApiKey
	}

	keys := []string{"appId", "timeStamp", "nonceStr", "package", "signType"}
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
	signStr.WriteString("&key=")
	signStr.WriteString(signKey)

	hash := md5.Sum([]byte(signStr.String()))
	paySign := strings.ToUpper(hex.EncodeToString(hash[:]))

	return map[string]interface{}{
		"appId":     s.config.Wechat.AppID,
		"timeStamp": timestamp,
		"nonceStr":  nonceStr,
		"package":   fmt.Sprintf("prepay_id=%s", prepayID),
		"signType":  "MD5",
		"paySign":   paySign,
	}, nil
}

func (s *WechatSandboxService) VerifySandboxSignKey(apiKey3, apiKey3Secret string) (string, error) {
	params := map[string]string{
		"mch_id":    s.config.Wechat.MchID,
		"nonce_str": s.GenerateNonceStr(),
	}

	sign := s.GenerateSign(params, apiKey3Secret)
	params["sign"] = sign

	xmlData, err := s.mapToXML(params)
	if err != nil {
		return "", fmt.Errorf("failed to convert params to xml: %w", err)
	}

	resp, err := s.httpClient.Post(s.GetSandboxSignKeyURL(), "text/xml", strings.NewReader(xmlData))
	if err != nil {
		return "", fmt.Errorf("failed to request sign key: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		ReturnCode     string `xml:"return_code"`
		ReturnMsg      string `xml:"return_msg"`
		SandboxSignKey string `xml:"sandbox_signkey"`
	}

	if err := xml.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return "", fmt.Errorf("failed to get sandbox sign key: %s", result.ReturnMsg)
	}

	return result.SandboxSignKey, nil
}

func (s *WechatSandboxService) HandleCallback(data []byte) (*WechatSandboxCallback, error) {
	var callback WechatSandboxCallback
	if err := xml.Unmarshal(data, &callback); err != nil {
		return nil, fmt.Errorf("failed to unmarshal callback data: %w", err)
	}

	return &callback, nil
}

func (s *WechatSandboxService) VerifyCallbackSign(params map[string]string) bool {
	receivedSign, ok := params["sign"]
	if !ok {
		return false
	}

	signKey := s.config.WechatSandbox.SandboxSignKey
	if signKey == "" {
		signKey = s.config.Wechat.ApiKey
	}

	calculatedSign := s.GenerateSign(params, signKey)
	return receivedSign == calculatedSign
}

func (s *WechatSandboxService) mapToXML(params map[string]string) (string, error) {
	var xml strings.Builder
	xml.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?><xml>")
	for k, v := range params {
		xml.WriteString(fmt.Sprintf("<%s><![CDATA[%s]]></%s>", k, v, k))
	}
	xml.WriteString("</xml>")
	return xml.String(), nil
}

type WechatSandboxRefundParams struct {
	AppID       string `xml:"appid"`
	MchID       string `xml:"mch_id"`
	NonceStr    string `xml:"nonce_str"`
	Sign        string `xml:"sign"`
	OutTradeNo  string `xml:"out_trade_no"`
	OutRefundNo string `xml:"out_refund_no"`
	TotalFee    int    `xml:"total_fee"`
	RefundFee   int    `xml:"refund_fee"`
}

type WechatSandboxRefundResult struct {
	ReturnCode    string `xml:"return_code"`
	ReturnMsg     string `xml:"return_msg"`
	AppID         string `xml:"appid"`
	MchID         string `xml:"mch_id"`
	NonceStr      string `xml:"nonce_str"`
	Sign          string `xml:"sign"`
	ResultCode    string `xml:"result_code"`
	TransactionID string `xml:"transaction_id"`
	OutTradeNo    string `xml:"out_trade_no"`
	OutRefundNo   string `xml:"out_refund_no"`
	RefundID      string `xml:"refund_id"`
}

func (s *WechatSandboxService) CreateRefundOrder(payment *model.PaymentRecord, refundAmount float64) (*WechatSandboxRefundResult, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("wechat sandbox is not enabled")
	}

	params := map[string]string{
		"appid":         s.config.Wechat.AppID,
		"mch_id":        s.config.Wechat.MchID,
		"nonce_str":     s.GenerateNonceStr(),
		"out_trade_no":  payment.TransactionID,
		"out_refund_no": fmt.Sprintf("refund_%s", payment.ID.String()),
		"total_fee":     fmt.Sprintf("%.0f", payment.PaymentAmount*100),
		"refund_fee":    fmt.Sprintf("%.0f", refundAmount*100),
	}

	signKey := s.config.WechatSandbox.SandboxSignKey
	if signKey == "" {
		signKey = s.config.Wechat.ApiKey
	}
	params["sign"] = s.GenerateSign(params, signKey)

	xmlData, err := s.mapToXML(params)
	if err != nil {
		return nil, fmt.Errorf("failed to convert params to xml: %w", err)
	}

	resp, err := s.httpClient.Post("https://api.mch.weixin.qq.com/sandbox/pay/refund", "text/xml", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat sandbox refund api: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result WechatSandboxRefundResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat sandbox refund api error: %s", result.ReturnMsg)
	}

	return &result, nil
}

type WechatSandboxQueryResult struct {
	ReturnCode     string `xml:"return_code"`
	ReturnMsg      string `xml:"return_msg"`
	AppID          string `xml:"appid"`
	MchID          string `xml:"mch_id"`
	NonceStr       string `xml:"nonce_str"`
	Sign           string `xml:"sign"`
	ResultCode     string `xml:"result_code"`
	TradeState     string `xml:"trade_state"`
	TradeStateDesc string `xml:"trade_state_desc"`
	TransactionID  string `xml:"transaction_id"`
	OutTradeNo     string `xml:"out_trade_no"`
	TotalFee       int    `xml:"total_fee"`
}

func (s *WechatSandboxService) QueryOrder(outTradeNo string) (*WechatSandboxQueryResult, error) {
	if !s.IsSandboxEnabled() {
		return nil, fmt.Errorf("wechat sandbox is not enabled")
	}

	params := map[string]string{
		"appid":        s.config.Wechat.AppID,
		"mch_id":       s.config.Wechat.MchID,
		"nonce_str":    s.GenerateNonceStr(),
		"out_trade_no": outTradeNo,
	}

	signKey := s.config.WechatSandbox.SandboxSignKey
	if signKey == "" {
		signKey = s.config.Wechat.ApiKey
	}
	params["sign"] = s.GenerateSign(params, signKey)

	xmlData, err := s.mapToXML(params)
	if err != nil {
		return nil, fmt.Errorf("failed to convert params to xml: %w", err)
	}

	resp, err := s.httpClient.Post("https://api.mch.weixin.qq.com/sandbox/pay/orderquery", "text/xml", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat sandbox query api: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result WechatSandboxQueryResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat sandbox query api error: %s", result.ReturnMsg)
	}

	return &result, nil
}

func (s *WechatSandboxService) GeneratePaymentQRCode(prepayID string) (string, error) {
	qrURL := fmt.Sprintf("weixin://wxpay/bizpayurl?pr=%s", prepayID)
	return qrURL, nil
}

func init() {
	crypto.RegisterHash(crypto.MD5, md5.New)
}
