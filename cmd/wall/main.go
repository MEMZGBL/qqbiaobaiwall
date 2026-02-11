package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	qzone "github.com/guohuiyuan/qzone-go"
	"github.com/guohuiyuan/qzonewall-go/internal/config"
	"github.com/guohuiyuan/qzonewall-go/internal/render"
	"github.com/guohuiyuan/qzonewall-go/internal/source"
	"github.com/guohuiyuan/qzonewall-go/internal/store"
	"github.com/guohuiyuan/qzonewall-go/internal/task"
	"github.com/guohuiyuan/qzonewall-go/internal/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// 加载配置
	cfgPath := "config.yaml"
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--config", "-c":
			if i+1 < len(os.Args) {
				i++
				cfgPath = os.Args[i]
			}
		default:
			cfgPath = os.Args[i]
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Println("[Main] 配置加载成功")

	// 初始化 SQLite 存储
	st, err := store.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer st.Close()
	log.Println("[Main] SQLite 初始化成功")

	// 初始化敏感词
	censorWords := store.LoadCensorWords(cfg.Censor.Words, cfg.Censor.WordsFile)
	log.Printf("[Main] 加载了 %d 个敏感词", len(censorWords))

	// 初始化截图渲染器
	renderer := render.NewRenderer(cfg.Render.FontPath, cfg.Render.FontSize)
	if renderer.Available() {
		log.Println("[Main] 截图渲染器已启用")
	} else {
		log.Println("[Main] 截图渲染器未启用 (未设置字体路径)")
	}

	// 启动 QQ Bot（内部启动 ZeroBot，需要先启动才能用 GetCookies）
	// 先用 nil client 占位，后续替换
	qqBot := source.NewQQBot(cfg.Bot, cfg.Wall, cfg.Qzone, st, renderer, nil, censorWords)
	if err := qqBot.Start(); err != nil {
		log.Fatalf("启动QQ Bot失败: %v", err)
	}
	log.Println("[Main] QQ Bot 已启动")

	// 尝试获取初始 Cookie
	initCookie, err := task.TryGetCookie(cfg.Qzone)
	if err != nil {
		log.Printf("[Main] ⚠️  Cookie初始化未成功: %v", err)
		log.Println("[Main] 请通过 /扫码 或 /刷新cookie 命令登录, 或在Web管理界面扫码")
		initCookie = "uin=o0;skey=@placeholder;p_skey=placeholder" // 占位, 后续刷新
	}

	// 创建 QQ空间 客户端，使用 WithOnSessionExpired 自动刷新
	qzClient, err := qzone.NewClient(initCookie,
		qzone.WithTimeout(cfg.Qzone.Timeout),
		qzone.WithMaxRetry(cfg.Qzone.MaxRetry),
		qzone.WithOnSessionExpired(task.RefreshCookie(cfg.Qzone, cfg.Bot)),
	)
	if err != nil {
		log.Printf("[Main] QQ空间客户端创建警告: %v", err)
	}
	log.Println("[Main] QQ空间客户端已创建(带自动刷新回调)")

	// 回填 client 到 QQBot
	qqBot.SetClient(qzClient)

	// 启动 Worker
	worker := task.NewWorker(cfg.Worker, cfg.Wall, qzClient, st, renderer)
	worker.Start()
	defer worker.Stop()

	// 启动 KeepAlive
	keepAlive := task.NewKeepAlive(cfg.Qzone, cfg.Bot, qzClient)
	keepAlive.Start()
	defer keepAlive.Stop()

	// 启动 Web 服务器
	if cfg.Web.Enable {
		webServer := web.NewServer(cfg.Web, cfg.Wall, st, qzClient)
		go func() {
			if err := webServer.Start(); err != nil {
				log.Printf("[Main] Web服务器停止: %v", err)
			}
		}()
		defer webServer.Stop()
		log.Printf("[Main] Web 服务器已启动: %s", cfg.Web.Addr)
	}

	log.Println("[Main] ========== 表白墙系统启动完成 ==========")

	// 等待退出信号
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("[Main] 收到信号 %v, 正在关闭...", s)
}
