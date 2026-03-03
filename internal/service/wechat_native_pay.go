package service

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"image/png"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/paper-format-checker/backend/internal/config"
	"github.com/paper-format-checker/backend/internal/model"
	"github.com/skip2/go-qrcode"
)

type WechatNativePayParams struct {
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
	ProductID      string `xml:"product_id"`
}

type WechatNativePayResult struct {
	ReturnCode string `xml:"return_code"`
	ReturnMsg  string `xml:"return_msg"`
	AppID      string `xml:"appid"`
	MchID      string `xml:"mch_id"`
	NonceStr   string `xml:"nonce_str"`
	Sign       string `xml:"sign"`
	ResultCode string `xml:"result_code"`
	PrepayID   string `xml:"prepay_id"`
	CodeURL    string `xml:"code_url"`
	TradeType  string `xml:"trade_type"`
}

type WechatNativeQueryResult struct {
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
	TradeType      string `xml:"trade_type"`
}

type WechatNativeRefundResult struct {
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
	RefundFee     int    `xml:"refund_fee"`
}

type WechatNativeRefundParams struct {
	AppID         string `xml:"appid"`
	MchID         string `xml:"mch_id"`
	NonceStr      string `xml:"nonce_str"`
	Sign          string `xml:"sign"`
	TransactionID string `xml:"transaction_id"`
	OutTradeNo    string `xml:"out_trade_no"`
	OutRefundNo   string `xml:"out_refund_no"`
	RefundFee     int    `xml:"refund_fee"`
	TotalFee      int    `xml:"total_fee"`
	RefundFeeType string `xml:"refund_fee_type"`
	OpUserID      string `xml:"op_user_id"`
}

type WechatNativeService struct {
	config     *config.Config
	httpClient *http.Client
}

func NewWechatNativeService(cfg *config.Config) *WechatNativeService {
	return &WechatNativeService{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *WechatNativeService) GetAPIURL() string {
	if s.config.WechatSandbox.Enabled {
		return "https://api.mch.weixin.qq.com/sandboxnew/pay/unifiedorder"
	}
	return "https://api.mch.weixin.qq.com/pay/unifiedorder"
}

func (s *WechatNativeService) GetQueryURL() string {
	if s.config.WechatSandbox.Enabled {
		return "https://api.mch.weixin.qq.com/sandboxnew/pay/orderquery"
	}
	return "https://api.mch.weixin.qq.com/pay/orderquery"
}

func (s *WechatNativeService) GetRefundURL() string {
	if s.config.WechatSandbox.Enabled {
		return "https://api.mch.weixin.qq.com/sandboxnew/pay/refund"
	}
	return "https://api.mch.weixin.qq.com/pay/refund"
}

func (s *WechatNativeService) GenerateNonceStr() string {
	rand.Seed(time.Now().UnixNano())
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func (s *WechatNativeService) GenerateSign(params map[string]string) string {
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
	signStr.WriteString("&key=")
	signStr.WriteString(s.config.Wechat.ApiKey)

	hash := md5.Sum(signStr.Bytes())
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func (s *WechatNativeService) mapToXML(params map[string]string) string {
	var xml bytes.Buffer
	xml.WriteString(`<?xml version="1.0" encoding="UTF-8"?><xml>`)
	for k, v := range params {
		xml.WriteString(fmt.Sprintf(`<%s><![CDATA[%s]]></%s>`, k, v, k))
	}
	xml.WriteString(`</xml>`)
	return xml.String()
}

func (s *WechatNativeService) XMLToMap(data []byte) (map[string]string, error) {
	var result map[string]string
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *WechatNativeService) CreateNativePayOrder(order *model.Order, productID string, clientIP string, paymentAmount float64) (*WechatNativePayResult, error) {
	if s.config.Wechat.AppID == "" || s.config.Wechat.MchID == "" || s.config.Wechat.ApiKey == "" {
		return nil, fmt.Errorf("wechat payment config is incomplete")
	}

	nonceStr := s.GenerateNonceStr()

	body := "论文格式检查服务"
	if order.MemberLevel != nil {
		body = fmt.Sprintf("购买%s会员", order.MemberLevel.LevelName)
		if order.MemberLevel.Description != "" {
			body = order.MemberLevel.Description
		}
	}

	params := map[string]string{
		"appid":            s.config.Wechat.AppID,
		"mch_id":           s.config.Wechat.MchID,
		"nonce_str":        nonceStr,
		"body":             body,
		"out_trade_no":     order.OrderNo,
		"total_fee":        fmt.Sprintf("%.0f", getMinAmount(paymentAmount)*100),
		"spbill_create_ip": getIPv4(clientIP),
		"notify_url":       s.config.Wechat.NotifyURL,
		"trade_type":       "NATIVE",
		"product_id":       productID,
	}

	sign := s.GenerateSign(params)
	params["sign"] = sign

	xmlData := s.mapToXML(params)

	resp, err := s.httpClient.Post(s.GetAPIURL(), "text/xml; charset=utf-8", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat unifiedorder api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &WechatNativePayResult{}
	if err := xml.Unmarshal(bodyBytes, result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat api error: %s", result.ReturnMsg)
	}

	if result.ResultCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat result error: %s", result.ReturnMsg)
	}

	return result, nil
}

func (s *WechatNativeService) GenerateQRCodeURL(codeURL string) string {
	qrURL := fmt.Sprintf("weixin://wxpay/bizpayurl?pr=%s", codeURL)
	return qrURL
}

func (s *WechatNativeService) GenerateQRCodeImageURL(codeURL string, width int) string {
	if width == 0 {
		width = 256
	}
	encodedURL := url.QueryEscape(codeURL)
	return fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=%dx%d&data=%s", width, width, encodedURL)
}

func (s *WechatNativeService) GenerateQRCodeImage(data string, width int) []byte {
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

func (s *WechatNativeService) QueryOrder(orderNo string) (*WechatNativeQueryResult, error) {
	if s.config.Wechat.AppID == "" || s.config.Wechat.MchID == "" || s.config.Wechat.ApiKey == "" {
		return nil, fmt.Errorf("wechat payment config is incomplete")
	}

	nonceStr := s.GenerateNonceStr()

	params := map[string]string{
		"appid":        s.config.Wechat.AppID,
		"mch_id":       s.config.Wechat.MchID,
		"nonce_str":    nonceStr,
		"out_trade_no": orderNo,
	}

	sign := s.GenerateSign(params)
	params["sign"] = sign

	xmlData := s.mapToXML(params)

	resp, err := s.httpClient.Post(s.GetQueryURL(), "text/xml; charset=utf-8", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat query api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &WechatNativeQueryResult{}
	if err := xml.Unmarshal(bodyBytes, result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat query api error: %s", result.ReturnMsg)
	}

	return result, nil
}

func (s *WechatNativeService) RefundOrder(order *model.Order, refundAmount float64, transactionID string) (*WechatNativeRefundResult, error) {
	if s.config.Wechat.AppID == "" || s.config.Wechat.MchID == "" || s.config.Wechat.ApiKey == "" {
		return nil, fmt.Errorf("wechat payment config is incomplete")
	}

	nonceStr := s.GenerateNonceStr()
	refundNo := fmt.Sprintf("refund_%s_%d", order.OrderNo, time.Now().UnixNano())

	params := map[string]string{
		"appid":           s.config.Wechat.AppID,
		"mch_id":          s.config.Wechat.MchID,
		"nonce_str":       nonceStr,
		"transaction_id":  transactionID,
		"out_trade_no":    order.OrderNo,
		"out_refund_no":   refundNo,
		"total_fee":       fmt.Sprintf("%.0f", order.TotalAmount*100),
		"refund_fee":      fmt.Sprintf("%.0f", refundAmount*100),
		"refund_fee_type": "CNY",
		"op_user_id":      s.config.Wechat.MchID,
	}

	sign := s.GenerateSign(params)
	params["sign"] = sign

	xmlData := s.mapToXML(params)

	resp, err := s.httpClient.Post(s.GetRefundURL(), "text/xml; charset=utf-8", strings.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to request wechat refund api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &WechatNativeRefundResult{}
	if err := xml.Unmarshal(bodyBytes, result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat refund api error: %s", result.ReturnMsg)
	}

	return result, nil
}

func (s *WechatNativeService) CloseOrder(orderNo string) error {
	if s.config.Wechat.AppID == "" || s.config.Wechat.MchID == "" || s.config.Wechat.ApiKey == "" {
		return fmt.Errorf("wechat payment config is incomplete")
	}

	nonceStr := s.GenerateNonceStr()

	params := map[string]string{
		"appid":        s.config.Wechat.AppID,
		"mch_id":       s.config.Wechat.MchID,
		"nonce_str":    nonceStr,
		"out_trade_no": orderNo,
	}

	sign := s.GenerateSign(params)
	params["sign"] = sign

	xmlData := s.mapToXML(params)

	resp, err := s.httpClient.Post("https://api.mch.weixin.qq.com/pay/closeorder", "text/xml; charset=utf-8", strings.NewReader(xmlData))
	if err != nil {
		return fmt.Errorf("failed to request wechat closeorder api: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	result := make(map[string]string)
	if err := xml.Unmarshal(bodyBytes, &result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result["return_code"] != "SUCCESS" {
		return fmt.Errorf("wechat closeorder api error: %s", result["return_msg"])
	}

	return nil
}

// getIPv4 将 IPv6 地址转换为 IPv4 格式（微信支付只支持 IPv4）
func getIPv4(ip string) string {
	if ip == "::1" || ip == "localhost" {
		return "127.0.0.1"
	}
	if ip == "::" || ip == "" {
		return "127.0.0.1"
	}
	if ip == "[::1]" {
		return "127.0.0.1"
	}
	if len(ip) > 15 {
		return "127.0.0.1"
	}
	if ip == "::ffff:127.0.0.1" {
		return "127.0.0.1"
	}
	return ip
}

// getMinAmount 获取最小金额（微信支付最小1分）
func getMinAmount(amount float64) float64 {
	if amount <= 0 {
		return 0.01 // 微信支付最小金额1分（0.01元）
	}
	return amount
}
