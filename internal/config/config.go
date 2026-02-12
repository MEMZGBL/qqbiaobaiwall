package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用总配置
type Config struct {
	Qzone    QzoneConfig    `yaml:"qzone"`
	Bot      BotConfig      `yaml:"bot"`
	Wall     WallConfig     `yaml:"wall"`
	Database DatabaseConfig `yaml:"database"`
	Web      WebConfig      `yaml:"web"`
	Censor   CensorConfig   `yaml:"censor"`
	Worker   WorkerConfig   `yaml:"worker"`
	Log      LogConfig      `yaml:"log"`
}

// QzoneConfig QQ空间账号配置
type QzoneConfig struct {
	CookieFile string        `yaml:"cookie_file"`
	KeepAlive time.Duration `yaml:"keep_alive"`
	MaxRetry  int           `yaml:"max_retry"`
	Timeout   time.Duration `yaml:"timeout"`
}

// BotConfig QQ机器人配置
type BotConfig struct {
	Zero        ZeroBotConfig `yaml:"zero"`
	WS          []WSConfig    `yaml:"ws"`
	ManageGroup int64         `yaml:"manage_group"`
}

// ZeroBotConfig ZeroBot 核心配置
type ZeroBotConfig struct {
	NickName       []string `yaml:"nickname" json:"nickname"`
	CommandPrefix  string   `yaml:"command_prefix" json:"command_prefix"`
	SuperUsers     []int64  `yaml:"super_users" json:"super_users"`
	RingLen        uint     `yaml:"ring_len" json:"ring_len"`
	Latency        int64    `yaml:"latency" json:"latency"`                   // 纳秒
	MaxProcessTime int64    `yaml:"max_process_time" json:"max_process_time"` // 纳秒
}

// WSConfig WebSocket 连接配置
type WSConfig struct {
	Url         string `yaml:"url" json:"Url"`
	AccessToken string `yaml:"access_token" json:"AccessToken"`
}

// WallConfig 表白墙配置
type WallConfig struct {
	ShowAuthor   bool          `yaml:"show_author"`
	AnonDefault  bool          `yaml:"anon_default"`
	MaxImages    int           `yaml:"max_images"`
	MaxTextLen   int           `yaml:"max_text_len"`
	PublishDelay time.Duration `yaml:"publish_delay"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// WebConfig 网页配置
type WebConfig struct {
	Enable    bool   `yaml:"enable"`
	Addr      string `yaml:"addr"`
	AdminUser string `yaml:"admin_user"`
	AdminPass string `yaml:"admin_pass"`
}

// CensorConfig 敏感词过滤配置
type CensorConfig struct {
	Enable    bool     `yaml:"enable"`
	Words     []string `yaml:"words"`
	WordsFile string   `yaml:"words_file"`
}

// WorkerConfig 任务调度配置
type WorkerConfig struct {
	Workers      int           `yaml:"workers"`
	RetryCount   int           `yaml:"retry_count"`
	RetryDelay   time.Duration `yaml:"retry_delay"`
	RateLimit    time.Duration `yaml:"rate_limit"`
	PollInterval time.Duration `yaml:"poll_interval"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `yaml:"level"`
}

// Load 加载配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err = yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.setDefaults()
	return cfg, nil
}

func (c *Config) setDefaults() {
	if c.Qzone.KeepAlive == 0 {
		c.Qzone.KeepAlive = 30 * time.Minute
	}
	if c.Qzone.MaxRetry == 0 {
		c.Qzone.MaxRetry = 2
	}
	if c.Qzone.Timeout == 0 {
		c.Qzone.Timeout = 30 * time.Second
	}
	if c.Bot.Zero.CommandPrefix == "" {
		c.Bot.Zero.CommandPrefix = "/"
	}
	if c.Bot.Zero.NickName == nil {
		c.Bot.Zero.NickName = []string{"表白墙Bot"}
	}
	if c.Wall.MaxImages == 0 {
		c.Wall.MaxImages = 9
	}
	if c.Wall.MaxTextLen == 0 {
		c.Wall.MaxTextLen = 2000
	}
	if c.Database.Path == "" {
		c.Database.Path = "data.db"
	}
	if c.Web.Addr == "" {
		c.Web.Addr = ":8080"
	}
	if c.Web.AdminUser == "" {
		c.Web.AdminUser = "admin"
	}
	if c.Web.AdminPass == "" {
		c.Web.AdminPass = "admin123"
	}
	if c.Worker.Workers == 0 {
		c.Worker.Workers = 1
	}
	if c.Worker.RetryCount == 0 {
		c.Worker.RetryCount = 3
	}
	if c.Worker.RetryDelay == 0 {
		c.Worker.RetryDelay = 5 * time.Second
	}
	if c.Worker.RateLimit == 0 {
		c.Worker.RateLimit = 30 * time.Second
	}
	if c.Worker.PollInterval == 0 {
		c.Worker.PollInterval = 5 * time.Second
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}
