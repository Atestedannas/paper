package aiclassifier

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DeepSeekWebClient 通过 Web API（cookie 认证）调用 DeepSeek
// 参考 openclaw-zero-token 的 deepseek-web-client.ts 实现
type DeepSeekWebClient struct {
	cookie     string
	bearer     string
	baseURL    string
	httpClient *http.Client
}

// deepSeekHTTPTimeout 单次 HTTP 请求（含读 SSE 响应体）超时。长论文分段 JSON 流常超过 2 分钟。
// 环境变量 DEEPSEEK_CHAT_TIMEOUT_SEC：30–900，默认 360（秒）。与 DEEPSEEK_REFINER_TIMEOUT_SEC 不同，此处约束底层 http.Client。
func deepSeekHTTPTimeout() time.Duration {
	const defaultSec = 360
	s := strings.TrimSpace(os.Getenv("DEEPSEEK_CHAT_TIMEOUT_SEC"))
	if s == "" {
		return defaultSec * time.Second
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 30 {
		return defaultSec * time.Second
	}
	if n > 900 {
		n = 900
	}
	return time.Duration(n) * time.Second
}

// NewDeepSeekWebClient 创建客户端
func NewDeepSeekWebClient(cookie, bearer string) *DeepSeekWebClient {
	timeout := deepSeekHTTPTimeout()
	log.Printf("\n========================================\n"+
		"[DeepSeek]1 初始化 Web 客户端\n"+
		"  base_url    : %s\n"+
		"  http_timeout: %v (env DEEPSEEK_CHAT_TIMEOUT_SEC)\n"+
		"  cookie      : %s...\n"+
		"  bearer      : %s...\n"+
		"========================================",
		"https://chat.deepseek.com",
		timeout,
		truncCookie(cookie, 60),
		truncCookie(bearer, 20))

	return &DeepSeekWebClient{
		cookie:  cookie,
		bearer:  bearer,
		baseURL: "https://chat.deepseek.com",
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DisableCompression: true,
			},
		},
	}
}

func truncCookie(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// powChallenge PoW 挑战结构
type powChallenge struct {
	Algorithm  string `json:"algorithm"`
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
	Salt       string `json:"salt"`
	Signature  string `json:"signature"`
	ExpireAt   int64  `json:"expire_at,omitempty"`
}

type powChallengeResp struct {
	Data struct {
		BizData struct {
			Challenge *powChallenge `json:"challenge"`
		} `json:"biz_data"`
	} `json:"data"`
}

type chatSessionResp struct {
	Data struct {
		BizData struct {
			ID            string `json:"id"`
			ChatSessionID string `json:"chat_session_id"`
		} `json:"biz_data"`
	} `json:"data"`
}

// commonHeaders 公共请求头（与浏览器真实请求一致）
func (c *DeepSeekWebClient) commonHeaders() http.Header {
	h := http.Header{}
	h.Set("Cookie", c.cookie)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "en,zh-CN;q=0.9,zh;q=0.8")
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36")
	h.Set("Referer", "https://chat.deepseek.com/")
	h.Set("Origin", "https://chat.deepseek.com")
	h.Set("Sec-Fetch-Dest", "empty")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("x-client-platform", "web")
	h.Set("x-client-version", "1.7.0")
	h.Set("x-app-version", "20241129.1")
	h.Set("x-client-locale", "en_US")
	h.Set("x-client-timezone-offset", "28800")
	if c.bearer != "" {
		h.Set("Authorization", "Bearer "+c.bearer)
	}
	return h
}

// createPowChallenge 创建 PoW 挑战
func (c *DeepSeekWebClient) createPowChallenge(targetPath string) (*powChallenge, error) {
	url := c.baseURL + "/api/v0/chat/create_pow_challenge"
	body, _ := json.Marshal(map[string]string{"target_path": targetPath})

	log.Printf("\n----------------------------------------\n"+
		"[DeepSeek] >> POST %s\n"+
		"  target_path: %s\n"+
		"----------------------------------------",
		url, targetPath)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = c.commonHeaders()

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[DeepSeek] << PoW Challenge FAILED (%v)\n  error: %v", elapsed, err)
		return nil, fmt.Errorf("pow challenge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[DeepSeek] << PoW Challenge %d (%v)\n  body: %s",
		resp.StatusCode, elapsed, truncate(string(respBody), 500))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("pow challenge HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result powChallengeResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("pow decode error: %w", err)
	}
	if result.Data.BizData.Challenge == nil {
		return nil, fmt.Errorf("pow challenge missing in response")
	}

	ch := result.Data.BizData.Challenge
	log.Printf("[DeepSeek] PoW Challenge parsed:\n"+
		"  algorithm  : %s\n"+
		"  difficulty : %d\n"+
		"  salt       : %s\n"+
		"  challenge  : %s...",
		ch.Algorithm, ch.Difficulty, ch.Salt, truncate(ch.Challenge, 30))

	return ch, nil
}

// solvePow dispatches to the correct solver based on the algorithm.
// Returns interface{} because sha256 yields int while DeepSeekHashV1 yields float64.
func (c *DeepSeekWebClient) solvePow(ch *powChallenge) (interface{}, error) {
	switch ch.Algorithm {
	case "sha256":
		return c.solvePowSHA256(ch)
	case "DeepSeekHashV1":
		return c.solvePowDeepSeekHashV1(ch)
	default:
		return nil, fmt.Errorf("unsupported PoW algorithm: %s", ch.Algorithm)
	}
}

func (c *DeepSeekWebClient) solvePowSHA256(ch *powChallenge) (int, error) {
	targetDifficulty := ch.Difficulty
	if targetDifficulty > 1000 {
		targetDifficulty = int(math.Log2(float64(targetDifficulty)))
	}

	log.Printf("[DeepSeek] Solving PoW (sha256, difficulty=%d, target_bits=%d)...",
		ch.Difficulty, targetDifficulty)

	start := time.Now()
	for nonce := 0; nonce < 2_000_000; nonce++ {
		input := fmt.Sprintf("%s%s%d", ch.Salt, ch.Challenge, nonce)
		hash := sha256.Sum256([]byte(input))

		zeroBits := 0
		for _, b := range hash {
			if b == 0 {
				zeroBits += 8
			} else {
				zeroBits += leadingZeroBits(b)
				break
			}
			if zeroBits >= targetDifficulty {
				break
			}
		}
		if zeroBits >= targetDifficulty {
			elapsed := time.Since(start)
			log.Printf("[DeepSeek] PoW solved:\n"+
				"  nonce    : %d\n"+
				"  elapsed  : %v\n"+
				"  attempts : %d",
				nonce, elapsed, nonce+1)
			return nonce, nil
		}
	}
	return 0, fmt.Errorf("SHA256 PoW timeout after 2M iterations")
}

func (c *DeepSeekWebClient) solvePowDeepSeekHashV1(ch *powChallenge) (float64, error) {
	log.Printf("[DeepSeek] Solving PoW (DeepSeekHashV1, difficulty=%d)...", ch.Difficulty)
	return solveDeepSeekHashV1(ch.Challenge, ch.Salt, ch.Difficulty, ch.ExpireAt)
}

func leadingZeroBits(b byte) int {
	if b == 0 {
		return 8
	}
	n := 0
	if b&0xF0 == 0 {
		n += 4
		b <<= 4
	}
	if b&0xC0 == 0 {
		n += 2
		b <<= 2
	}
	if b&0x80 == 0 {
		n++
	}
	return n
}

// createChatSession 创建聊天会话
func (c *DeepSeekWebClient) createChatSession() (string, error) {
	url := c.baseURL + "/api/v0/chat_session/create"
	body, _ := json.Marshal(map[string]interface{}{})

	log.Printf("\n----------------------------------------\n"+
		"[DeepSeek] >> POST %s\n"+
		"----------------------------------------", url)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header = c.commonHeaders()

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[DeepSeek] << Session Create FAILED (%v)\n  error: %v", elapsed, err)
		return "", fmt.Errorf("create session failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("[DeepSeek] << Session Create %d (%v)\n  body: %s",
		resp.StatusCode, elapsed, truncate(string(respBody), 300))

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("create session HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result chatSessionResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	sid := result.Data.BizData.ID
	if sid == "" {
		sid = result.Data.BizData.ChatSessionID
	}

	log.Printf("[DeepSeek] Session ID: %s", sid)
	return sid, nil
}

// ChatCompletion 发送消息并获取完整回复（完整流程日志）
func (c *DeepSeekWebClient) ChatCompletion(prompt string) (string, error) {
	totalStart := time.Now()

	log.Printf("\n========================================\n"+
		"[DeepSeek] ChatCompletion START\n"+
		"  prompt_len: %d chars\n"+
		"  prompt    : %s\n"+
		"========================================",
		len([]rune(prompt)), truncate(prompt, 200))

	// Step 1: 创建会话
	sessionID, err := c.createChatSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	// Step 2: PoW
	targetPath := "/api/v0/chat/completion"
	challenge, err := c.createPowChallenge(targetPath)
	if err != nil {
		return "", fmt.Errorf("PoW challenge failed: %w", err)
	}

	answer, err := c.solvePow(challenge)
	if err != nil {
		return "", fmt.Errorf("PoW solve failed: %w", err)
	}

	// Step 3: 构造 PoW response（Base64 JSON）
	powPayload := map[string]interface{}{
		"algorithm":   challenge.Algorithm,
		"challenge":   challenge.Challenge,
		"difficulty":  challenge.Difficulty,
		"salt":        challenge.Salt,
		"signature":   challenge.Signature,
		"answer":      answer,
		"target_path": targetPath,
	}
	if challenge.ExpireAt > 0 {
		powPayload["expire_at"] = challenge.ExpireAt
	}
	powJSON, _ := json.Marshal(powPayload)
	powBase64 := base64.StdEncoding.EncodeToString(powJSON)

	// Step 4: 发送 completion 请求
	reqBody, _ := json.Marshal(map[string]interface{}{
		"chat_session_id":   sessionID,
		"parent_message_id": nil,
		"prompt":            prompt,
		"ref_file_ids":      []string{},
		"thinking_enabled":  false,
		"search_enabled":    false,
	})

	completionURL := c.baseURL + targetPath
	log.Printf("\n----------------------------------------\n"+
		"[DeepSeek] >> POST %s\n"+
		"  session_id  : %s\n"+
		"  pow_base64  : %s...\n"+
		"  body_len    : %d bytes\n"+
		"----------------------------------------",
		completionURL, sessionID, truncate(powBase64, 40), len(reqBody))

	req, err := http.NewRequest("POST", completionURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header = c.commonHeaders()
	req.Header.Set("x-ds-pow-response", powBase64)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[DeepSeek] << Completion FAILED\n  error: %v", err)
		return "", fmt.Errorf("completion request failed: %w", err)
	}
	defer resp.Body.Close()

	contentEncoding := resp.Header.Get("Content-Encoding")
	log.Printf("[DeepSeek] << Completion %d (connect %v)\n"+
		"  content-type    : %s\n"+
		"  content-encoding: %s\n"+
		"  transfer-encoding: %s",
		resp.StatusCode, time.Since(start),
		resp.Header.Get("Content-Type"),
		contentEncoding,
		resp.Header.Get("Transfer-Encoding"))

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		log.Printf("[DeepSeek] << ERROR BODY:\n%s", string(b))
		return "", fmt.Errorf("completion HTTP %d: %s", resp.StatusCode, string(b))
	}

	// Step 5: 解析 SSE 流
	content, err := c.parseSSEResponse(resp.Body, contentEncoding)
	totalElapsed := time.Since(totalStart)

	if err != nil {
		log.Printf("\n========================================\n"+
			"[DeepSeek] ChatCompletion END（失败，可能为 SSE 读超时）\n"+
			"  total_time    : %v\n"+
			"  partial_chars : %d\n"+
			"  error         : %v\n"+
			"  hint          : 可调大 DEEPSEEK_CHAT_TIMEOUT_SEC（当前 http 超时 %v）\n"+
			"  head          : %s\n"+
			"========================================",
			totalElapsed, len([]rune(content)), err, c.httpClient.Timeout, truncate(content, 400))
		return content, err
	}

	log.Printf("\n========================================\n"+
		"[DeepSeek] ChatCompletion DONE\n"+
		"  total_time   : %v\n"+
		"  response_len : %d chars\n"+
		"  response     : %s\n"+
		"========================================",
		totalElapsed, len([]rune(content)), truncate(content, 300))

	return content, nil
}

// parseSSEResponse 解析 DeepSeek Web SSE 流
// DeepSeek 网页版实际使用 JSON Patch 格式流式传输内容:
//
//	data: {"p":"response/fragments/-1/content","o":"APPEND","v":"文本片段"}
//
// 每个 APPEND 操作追加一小段文本，拼接起来就是完整的 AI 回复。
func (c *DeepSeekWebClient) parseSSEResponse(body io.Reader, contentEncoding string) (string, error) {
	var reader io.Reader = body

	if strings.Contains(contentEncoding, "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return "", fmt.Errorf("gzip decompress failed: %w", err)
		}
		defer gz.Close()
		reader = gz
		log.Printf("[DeepSeek] SSE: decompressing gzip response")
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var fullContent strings.Builder
	chunkCount := 0
	lineCount := 0
	dataLineCount := 0
	firstLines := make([]string, 0, 15)
	inContentStream := false
	// 缓冲不完整的 JSON 行（DeepSeek 的 v 值可能含换行，被 scanner 切断）
	var incompleteLine string

	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		if lineCount <= 15 {
			firstLines = append(firstLines, truncate(line, 250))
		}

		// 如果上一行 JSON 不完整，尝试拼接
		if incompleteLine != "" {
			incompleteLine += "\n" + line
			var testRaw map[string]json.RawMessage
			if json.Unmarshal([]byte(incompleteLine), &testRaw) == nil {
				line = "data: " + incompleteLine
				incompleteLine = ""
				// 继续正常处理
			} else {
				// 还是不完整，继续缓冲（最多 5 行）
				if strings.Count(incompleteLine, "\n") > 5 {
					incompleteLine = ""
				}
				continue
			}
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		dataLineCount++

		var raw map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &raw) != nil {
			// JSON 解析失败 — 可能是换行把 JSON 切断了，缓冲起来
			if strings.Contains(data, "\"v\"") || strings.Contains(data, "\"p\"") {
				incompleteLine = data
			}
			continue
		}

		// ── 含有 p（路径）的 JSON Patch 操作 ──
		if pRaw, hasP := raw["p"]; hasP {
			var path, op string
			json.Unmarshal(pRaw, &path)
			if oRaw, ok := raw["o"]; ok {
				json.Unmarshal(oRaw, &op)
			}
			if vRaw, ok := raw["v"]; ok {
				if op == "APPEND" && strings.Contains(path, "content") {
					// 直接 content APPEND: {"p":"response/fragments/-1/content","o":"APPEND","v":"text"}
					inContentStream = true
					var text string
					if json.Unmarshal(vRaw, &text) == nil && text != "" {
						fullContent.WriteString(text)
						chunkCount++
					}
				} else if op == "APPEND" && strings.Contains(path, "fragments") {
					// fragment 创建: {"p":"response/fragments","o":"APPEND","v":{"content_type":"text","content":"{\n"}}
					// v 是对象，里面的 content 字段包含初始文本（如 "{\n"）
					var fragObj struct {
						Content string `json:"content"`
					}
					if json.Unmarshal(vRaw, &fragObj) == nil && fragObj.Content != "" {
						fullContent.WriteString(fragObj.Content)
						chunkCount++
						inContentStream = true
					}
				} else {
					inContentStream = false
				}
			}
			continue
		}

		// ── 后续内容片段: 只有 {"v":"text"} ──
		if inContentStream {
			if vRaw, hasV := raw["v"]; hasV && len(raw) == 1 {
				var text string
				if json.Unmarshal(vRaw, &text) == nil {
					fullContent.WriteString(text)
					chunkCount++
					continue
				}
			}
			inContentStream = false
		}

		// ── 完整响应对象 ──
		if vRaw, ok := raw["v"]; ok {
			var vObj struct {
				Response struct {
					Content   string `json:"content"`
					Status    string `json:"status"`
					Fragments []struct {
						Content string `json:"content"`
					} `json:"fragments"`
				} `json:"response"`
			}
			if json.Unmarshal(vRaw, &vObj) == nil {
				status := vObj.Response.Status

				if status == "DONE" {
					// DONE 时用完整内容覆盖增量拼接的结果
					doneContent := vObj.Response.Content
					if doneContent == "" {
						var sb strings.Builder
						for _, f := range vObj.Response.Fragments {
							sb.WriteString(f.Content)
						}
						doneContent = sb.String()
					}
					if doneContent != "" {
						fullContent.Reset()
						fullContent.WriteString(doneContent)
						chunkCount++
						log.Printf("[DeepSeek] SSE: DONE packet content (%d bytes)", len(doneContent))
					}
				} else if status == "WIP" && fullContent.Len() == 0 {
					// WIP 包可能携带 fragments 的初始内容（如 "{\n"），
					// 后续 APPEND 只追加增量，不含此初始部分
					for _, f := range vObj.Response.Fragments {
						if f.Content != "" {
							fullContent.WriteString(f.Content)
							chunkCount++
							inContentStream = true
							log.Printf("[DeepSeek] SSE: WIP fragment seed (%d bytes): %q", len(f.Content), f.Content)
						}
					}
				}
			}
			continue
		}

		// ── Fallback: OpenAI 格式 ──
		if _, ok := raw["choices"]; ok {
			var oaiEvent struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &oaiEvent) == nil {
				for _, choice := range oaiEvent.Choices {
					if choice.Delta.Content != "" {
						fullContent.WriteString(choice.Delta.Content)
						chunkCount++
					}
				}
			}
		}
	}

	log.Printf("[DeepSeek] SSE parsed:\n"+
		"  total_lines  : %d\n"+
		"  data_lines   : %d\n"+
		"  content_chunks: %d\n"+
		"  content_len  : %d bytes\n"+
		"  first_lines  : %v",
		lineCount, dataLineCount, chunkCount, fullContent.Len(), firstLines)

	if err := scanner.Err(); err != nil {
		partial := fullContent.String()
		// 典型原因：http.Client.Timeout 在读长 SSE 时到期（context deadline exceeded）
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded") {
			return partial, fmt.Errorf("SSE scan error (读流超时，可调大 DEEPSEEK_CHAT_TIMEOUT_SEC): %w", err)
		}
		return partial, fmt.Errorf("SSE scan error: %w", err)
	}
	return fullContent.String(), nil
}
