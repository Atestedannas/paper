package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// WechatSandboxConfig 微信沙箱配置
type WechatSandboxConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	SandboxSignKey string `mapstructure:"sandbox_sign_key"`
}

// DeepSeekConfig DeepSeek AI 配置
type DeepSeekConfig struct {
	Cookie              string `mapstructure:"cookie"`
	Bearer              string `mapstructure:"bearer"`
	Enabled             bool   `mapstructure:"enabled"`
	MaxCallsPerDocument int    `mapstructure:"max_calls_per_document"`
	RetrainThreshold    int    `mapstructure:"retrain_threshold"` // 累积多少样本后触发重训练
}

// FormatterConfig 格式修正引擎配置（python-docx 样式引用引擎）
type FormatterConfig struct {
	Enabled           bool   `mapstructure:"enabled"`
	PythonBin         string `mapstructure:"python_bin"`
	ScriptPath        string `mapstructure:"script_path"`
	TimeoutSec        int    `mapstructure:"timeout"`
	ValidateMaxRepair int    `mapstructure:"validate_max_repair"` // 验收失败后的自我修复轮数（0=只校验不修复）
	SchoolSpecPath    string `mapstructure:"school_spec_path"`    // 可选：高校 *.spec.json 绝对路径
}

// Config 应用配置结构体
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	JWT           JWTConfig           `mapstructure:"jwt"`
	File          FileConfig          `mapstructure:"file"`
	Log           LogConfig           `mapstructure:"log"`
	RBAC          RBACConfig          `mapstructure:"rbac"`
	Wechat        WechatConfig        `mapstructure:"wechat"`
	WechatSandbox WechatSandboxConfig `mapstructure:"wechat_sandbox"`
	Alipay        AlipayConfig        `mapstructure:"alipay"`
	Payment       PaymentConfig       `mapstructure:"payment"`
	DeepSeek      DeepSeekConfig      `mapstructure:"deepseek"`
	Formatter     FormatterConfig     `mapstructure:"formatter"`
}

// RBACConfig RBAC 配置
type RBACConfig struct {
	// 主模型：permission（默认）。保留 authority 仅用于历史兼容观测。
	Model string `mapstructure:"model"`
}

// PaymentConfig 支付配置
type PaymentConfig struct {
	PaperDownload  float64 `mapstructure:"paper_download"`  // 论文下载（元/次）
	FormatCheck    float64 `mapstructure:"format_check"`    // 格式检查（元/次）
	FormatFix      float64 `mapstructure:"format_fix"`      // 格式修复（元/次）
	ReportDownload float64 `mapstructure:"report_download"` // 报告下载（元/次）
	Compare        float64 `mapstructure:"compare"`         // 比较功能（元/次）
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
	Env  string `mapstructure:"env"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"sslmode"`
}

// JWTConfig JWT配置
type JWTConfig struct {
	Secret             string        `mapstructure:"secret"`
	Expiration         time.Duration `mapstructure:"expiration"`
	AccessTokenExpiry  time.Duration `mapstructure:"access_token_expiry"`
	RefreshTokenExpiry time.Duration `mapstructure:"refresh_token_expiry"`
	MaxRefreshCount    int           `mapstructure:"max_refresh_count"`
}

// FileConfig 文件存储配置
type FileConfig struct {
	UploadPath   string `mapstructure:"upload_path"`
	MaxSize      int64  `mapstructure:"max_size"`
	AllowedTypes string `mapstructure:"allowed_types"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `mapstructure:"level"`
	Path  string `mapstructure:"path"`
}

// WechatConfig 微信配置
type WechatConfig struct {
	AppID       string `mapstructure:"app_id"`
	MchID       string `mapstructure:"mch_id"`
	ApiKey      string `mapstructure:"api_key"`
	NotifyURL   string `mapstructure:"notify_url"`
	RedirectURL string `mapstructure:"redirect_url"`
	Scope       string `mapstructure:"scope"`
}

// AlipayConfig 支付宝配置
type AlipayConfig struct {
	AppID                string `mapstructure:"app_id"`
	AppPrivateKey        string `mapstructure:"app_private_key"`
	AlipayPublicKey      string `mapstructure:"alipay_public_key"`
	SignType             string `mapstructure:"sign_type"` // RSA 或 RSA2（默认 RSA2）
	NotifyURL            string `mapstructure:"notify_url"`
	ReturnURL            string `mapstructure:"return_url"`
	RedirectURL          string `mapstructure:"redirect_url"`
	Scope                string `mapstructure:"scope"`
	AuthorizeURL         string `mapstructure:"authorize_url"`
	GatewayURL           string `mapstructure:"gateway_url"`
	SandboxEnabled       bool   `mapstructure:"sandbox_enabled"`
	SandboxAppID         string `mapstructure:"sandbox_app_id"`
	SandboxAppPrivateKey string `mapstructure:"sandbox_app_private_key"`
	SandboxSignType      string `mapstructure:"sandbox_sign_type"` // 沙箱签名类型（默认 RSA2）
	SandboxGatewayURL    string `mapstructure:"sandbox_gateway_url"`
	SandboxNotifyURL     string `mapstructure:"sandbox_notify_url"`
	SandboxReturnURL     string `mapstructure:"sandbox_return_url"`
}

// LoadConfig 从环境变量和配置文件加载配置
func LoadConfig(configPath string) (*Config, error) {
	// 设置默认值
	config := &Config{
		Server: ServerConfig{
			Port: 8000,
			Host: "localhost",
			Env:  "development",
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "password",
			Name:     "paper_checker",
			SSLMode:  "disable",
		},
		JWT: JWTConfig{
			Secret:             "your-secret-key",
			Expiration:         24 * time.Hour,
			AccessTokenExpiry:  1 * time.Hour,
			RefreshTokenExpiry: 30 * 24 * time.Hour,
			MaxRefreshCount:    5,
		},
		File: FileConfig{
			UploadPath:   "./uploads",
			MaxSize:      10 * 1024 * 1024, // 10MB
			AllowedTypes: "application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		Log: LogConfig{
			Level: "info",
			Path:  "./logs",
		},
		RBAC: RBACConfig{
			Model: "permission",
		},
		Wechat: WechatConfig{
			AppID:       "",
			MchID:       "",
			ApiKey:      "",
			NotifyURL:   "",
			RedirectURL: "",
			Scope:       "snsapi_login",
		},
		WechatSandbox: WechatSandboxConfig{
			Enabled:        false,
			SandboxSignKey: "",
		},
		Alipay: AlipayConfig{
			AppID:             "",
			AppPrivateKey:     "",
			AlipayPublicKey:   "",
			NotifyURL:         "",
			ReturnURL:         "",
			RedirectURL:       "",
			Scope:             "auth_user",
			AuthorizeURL:      "https://open.auth.alipay.com/oauth2/publicAppAuthorize.htm",
			GatewayURL:        "https://openapi.alipay.com/gateway.do",
			SandboxEnabled:    false,
			SandboxGatewayURL: "https://openapi.alipaydev.com/gateway.do",
		},
		Payment: PaymentConfig{
			PaperDownload:  0,  // 默认免费
			FormatCheck:    10, // 默认10元/次
			FormatFix:      15, // 默认15元/次
			ReportDownload: 5,  // 默认5元/次
			Compare:        8,  // 默认8元/次
		},
	}

	// 加载.env文件
	if err := godotenv.Load(configPath); err != nil {
		log.Println("Warning: .env file not found")
	}

	// 从环境变量加载服务器配置
	if port := os.Getenv("SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Server.Port = p
		}
	}
	if host := os.Getenv("SERVER_HOST"); host != "" {
		config.Server.Host = host
	}
	if env := os.Getenv("SERVER_MODE"); env != "" {
		config.Server.Env = env
	}

	// 从环境变量加载数据库配置
	if dbHost := os.Getenv("DATABASE_HOST"); dbHost != "" {
		config.Database.Host = dbHost
	}
	if dbPort := os.Getenv("DATABASE_PORT"); dbPort != "" {
		if p, err := strconv.Atoi(dbPort); err == nil {
			config.Database.Port = p
		}
	}
	if dbUser := os.Getenv("DATABASE_USER"); dbUser != "" {
		config.Database.User = dbUser
	}
	if dbPassword := os.Getenv("DATABASE_PASSWORD"); dbPassword != "" {
		config.Database.Password = dbPassword
	}
	if dbName := os.Getenv("DATABASE_NAME"); dbName != "" {
		config.Database.Name = dbName
	}
	if dbSSLMode := os.Getenv("DATABASE_SSL_MODE"); dbSSLMode != "" {
		config.Database.SSLMode = dbSSLMode
	}

	// 从环境变量加载JWT配置
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		config.JWT.Secret = jwtSecret
	}
	if jwtExpiryHours := os.Getenv("JWT_EXPIRY_HOURS"); jwtExpiryHours != "" {
		if h, err := strconv.Atoi(jwtExpiryHours); err == nil {
			config.JWT.Expiration = time.Duration(h) * time.Hour
		}
	}

	// 从环境变量加载文件配置
	if fileUploadPath := os.Getenv("FILE_UPLOAD_PATH"); fileUploadPath != "" {
		config.File.UploadPath = fileUploadPath
	}
	if maxFileSize := os.Getenv("MAX_FILE_SIZE"); maxFileSize != "" {
		if s, err := strconv.ParseInt(maxFileSize, 10, 64); err == nil {
			config.File.MaxSize = s
		}
	}
	if allowedTypes := os.Getenv("ALLOWED_FILE_TYPES"); allowedTypes != "" {
		config.File.AllowedTypes = allowedTypes
	}

	// 从环境变量加载日志配置
	if logPath := os.Getenv("LOG_PATH"); logPath != "" {
		config.Log.Path = logPath
	}
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		config.Log.Level = logLevel
	}

	// 从环境变量加载 RBAC 配置
	if rbacModel := os.Getenv("RBAC_MODEL"); rbacModel != "" {
		switch rbacModel {
		case "permission", "authority":
			config.RBAC.Model = rbacModel
		default:
			log.Printf("Warning: invalid RBAC_MODEL=%s, fallback to permission", rbacModel)
			config.RBAC.Model = "permission"
		}
	}

	// 从环境变量加载微信配置
	if wechatAppID := os.Getenv("WECHAT_APP_ID"); wechatAppID != "" {
		config.Wechat.AppID = wechatAppID
	}
	if wechatMchID := os.Getenv("WECHAT_MCH_ID"); wechatMchID != "" {
		config.Wechat.MchID = wechatMchID
	}
	if wechatApiKey := os.Getenv("WECHAT_API_KEY"); wechatApiKey != "" {
		config.Wechat.ApiKey = wechatApiKey
	}
	if wechatNotifyURL := os.Getenv("WECHAT_NOTIFY_URL"); wechatNotifyURL != "" {
		config.Wechat.NotifyURL = wechatNotifyURL
	}
	if wechatRedirectURL := os.Getenv("WECHAT_REDIRECT_URL"); wechatRedirectURL != "" {
		config.Wechat.RedirectURL = wechatRedirectURL
	}
	if wechatScope := os.Getenv("WECHAT_SCOPE"); wechatScope != "" {
		config.Wechat.Scope = wechatScope
	}
	// 从环境变量加载支付宝配置
	if alipayAppID := os.Getenv("ALIPAY_APP_ID"); alipayAppID != "" {
		config.Alipay.AppID = alipayAppID
	}
	if alipayPrivateKey := os.Getenv("ALIPAY_APP_PRIVATE_KEY"); alipayPrivateKey != "" {
		config.Alipay.AppPrivateKey = alipayPrivateKey
	}
	if alipayPublicKey := os.Getenv("ALIPAY_PUBLIC_KEY"); alipayPublicKey != "" {
		config.Alipay.AlipayPublicKey = alipayPublicKey
	}
	if alipaySignType := os.Getenv("ALIPAY_SIGN_TYPE"); alipaySignType != "" {
		config.Alipay.SignType = alipaySignType
	}
	if config.Alipay.SignType == "" {
		config.Alipay.SignType = "RSA2" // 默认 RSA2
	}
	if alipayNotifyURL := os.Getenv("ALIPAY_NOTIFY_URL"); alipayNotifyURL != "" {
		config.Alipay.NotifyURL = alipayNotifyURL
	}
	if alipayReturnURL := os.Getenv("ALIPAY_RETURN_URL"); alipayReturnURL != "" {
		config.Alipay.ReturnURL = alipayReturnURL
	}
	if alipayRedirectURL := os.Getenv("ALIPAY_REDIRECT_URL"); alipayRedirectURL != "" {
		config.Alipay.RedirectURL = alipayRedirectURL
	}
	if alipayScope := os.Getenv("ALIPAY_SCOPE"); alipayScope != "" {
		config.Alipay.Scope = alipayScope
	}
	if alipayAuthorizeURL := os.Getenv("ALIPAY_AUTHORIZE_URL"); alipayAuthorizeURL != "" {
		config.Alipay.AuthorizeURL = alipayAuthorizeURL
	}
	if alipayGatewayURL := os.Getenv("ALIPAY_GATEWAY_URL"); alipayGatewayURL != "" {
		config.Alipay.GatewayURL = alipayGatewayURL
	}
	if alipaySandboxEnabled := os.Getenv("ALIPAY_SANDBOX_ENABLED"); alipaySandboxEnabled != "" {
		if enabled, err := strconv.ParseBool(alipaySandboxEnabled); err == nil {
			config.Alipay.SandboxEnabled = enabled
		}
	}
	if v := os.Getenv("ALIPAY_SANDBOX_APP_ID"); v != "" {
		config.Alipay.SandboxAppID = v
	}
	if v := os.Getenv("ALIPAY_SANDBOX_APP_PRIVATE_KEY"); v != "" {
		config.Alipay.SandboxAppPrivateKey = v
	}
	if v := os.Getenv("ALIPAY_SANDBOX_SIGN_TYPE"); v != "" {
		config.Alipay.SandboxSignType = v
	}
	if v := os.Getenv("ALIPAY_SANDBOX_NOTIFY_URL"); v != "" {
		config.Alipay.SandboxNotifyURL = v
	}
	if v := os.Getenv("ALIPAY_SANDBOX_RETURN_URL"); v != "" {
		config.Alipay.SandboxReturnURL = v
	}
	if alipaySandboxGatewayURL := os.Getenv("ALIPAY_SANDBOX_GATEWAY_URL"); alipaySandboxGatewayURL != "" {
		config.Alipay.SandboxGatewayURL = alipaySandboxGatewayURL
	}

	// 从环境变量加载微信沙箱配置
	if wechatSandboxEnabled := os.Getenv("WECHAT_SANDBOX_ENABLED"); wechatSandboxEnabled != "" {
		if enabled, err := strconv.ParseBool(wechatSandboxEnabled); err == nil {
			config.WechatSandbox.Enabled = enabled
		}
	}
	if wechatSandboxSignKey := os.Getenv("WECHAT_SANDBOX_SIGN_KEY"); wechatSandboxSignKey != "" {
		config.WechatSandbox.SandboxSignKey = wechatSandboxSignKey
	}

	// 从环境变量加载支付配置
	if paperDownload := os.Getenv("PAYMENT_PAPER_DOWNLOAD"); paperDownload != "" {
		if v, err := strconv.ParseFloat(paperDownload, 64); err == nil {
			config.Payment.PaperDownload = v
		}
	}
	if formatCheck := os.Getenv("PAYMENT_FORMAT_CHECK"); formatCheck != "" {
		if v, err := strconv.ParseFloat(formatCheck, 64); err == nil {
			config.Payment.FormatCheck = v
		}
	}
	if formatFix := os.Getenv("PAYMENT_FORMAT_FIX"); formatFix != "" {
		if v, err := strconv.ParseFloat(formatFix, 64); err == nil {
			config.Payment.FormatFix = v
		}
	}
	if reportDownload := os.Getenv("PAYMENT_REPORT_DOWNLOAD"); reportDownload != "" {
		if v, err := strconv.ParseFloat(reportDownload, 64); err == nil {
			config.Payment.ReportDownload = v
		}
	}
	if compare := os.Getenv("PAYMENT_COMPARE"); compare != "" {
		if v, err := strconv.ParseFloat(compare, 64); err == nil {
			config.Payment.Compare = v
		}
	}

	// 从环境变量加载 DeepSeek 配置
	if cookie := os.Getenv("DEEPSEEK_COOKIE"); cookie != "" {
		config.DeepSeek.Cookie = cookie
		config.DeepSeek.Enabled = true
	}
	if bearer := os.Getenv("DEEPSEEK_BEARER"); bearer != "" {
		config.DeepSeek.Bearer = bearer
	}
	if enabled := os.Getenv("DEEPSEEK_ENABLED"); enabled != "" {
		if v, err := strconv.ParseBool(enabled); err == nil {
			config.DeepSeek.Enabled = v
		}
	}
	if maxCalls := os.Getenv("DEEPSEEK_MAX_CALLS_PER_DOC"); maxCalls != "" {
		if v, err := strconv.Atoi(maxCalls); err == nil {
			config.DeepSeek.MaxCallsPerDocument = v
		}
	}
	if config.DeepSeek.MaxCallsPerDocument == 0 {
		config.DeepSeek.MaxCallsPerDocument = 20
	}
	if threshold := os.Getenv("DEEPSEEK_RETRAIN_THRESHOLD"); threshold != "" {
		if v, err := strconv.Atoi(threshold); err == nil {
			config.DeepSeek.RetrainThreshold = v
		}
	}
	if config.DeepSeek.RetrainThreshold == 0 {
		config.DeepSeek.RetrainThreshold = 200
	}

	// 从环境变量加载 Formatter 配置
	config.Formatter.Enabled = true // 默认启用
	if formatterEnabled := os.Getenv("FORMATTER_ENABLED"); formatterEnabled != "" {
		if v, err := strconv.ParseBool(formatterEnabled); err == nil {
			config.Formatter.Enabled = v
		}
	}
	if pythonBin := os.Getenv("FORMATTER_PYTHON_BIN"); pythonBin != "" {
		config.Formatter.PythonBin = pythonBin
	}
	if scriptPath := os.Getenv("FORMATTER_SCRIPT_PATH"); scriptPath != "" {
		config.Formatter.ScriptPath = scriptPath
	}
	if timeout := os.Getenv("FORMATTER_TIMEOUT"); timeout != "" {
		if v, err := strconv.Atoi(timeout); err == nil {
			config.Formatter.TimeoutSec = v
		}
	}
	if config.Formatter.TimeoutSec == 0 {
		config.Formatter.TimeoutSec = 120
	}
	config.Formatter.ValidateMaxRepair = 3
	if vmr := os.Getenv("FORMATTER_VALIDATE_MAX_REPAIR"); vmr != "" {
		if v, err := strconv.Atoi(vmr); err == nil {
			config.Formatter.ValidateMaxRepair = v
		}
	}
	if sp := os.Getenv("FORMATTER_SCHOOL_SPEC"); sp != "" {
		config.Formatter.SchoolSpecPath = sp
	}

	return config, nil
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
