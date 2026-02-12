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
		log.Fatalf("load config failed: %v", err)
	}
	log.Println("[Main] config loaded")

	st, err := store.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("init sqlite failed: %v", err)
	}
	defer st.Close()
	log.Println("[Main] sqlite ready")

	censorWords := store.LoadCensorWords(cfg.Censor.Words, cfg.Censor.WordsFile)
	log.Printf("[Main] loaded censor words: %d", len(censorWords))

	renderer := render.NewRenderer()
	if renderer.Available() {
		log.Println("[Main] renderer enabled")
	} else {
		log.Println("[Main] renderer disabled")
	}

	qqBot := source.NewQQBot(cfg.Bot, cfg.Wall, cfg.Qzone, st, renderer, nil, censorWords)
	if err := qqBot.Start(); err != nil {
		log.Fatalf("start qq bot failed: %v", err)
	}
	log.Println("[Main] qq bot started")

	initCookie, err := task.TryGetCookie(cfg.Qzone)
	if err != nil {
		log.Printf("[Main] initial cookie unavailable: %v", err)
		log.Println("[Main] use /扫码 or web admin QR login to refresh cookie")
		initCookie = "uin=o0;skey=@placeholder;p_skey=placeholder"
	}

	qzClient, err := qzone.NewClient(initCookie,
		qzone.WithTimeout(cfg.Qzone.Timeout),
		qzone.WithMaxRetry(cfg.Qzone.MaxRetry),
		qzone.WithOnSessionExpired(task.RefreshCookie(cfg.Bot)),
	)
	if err != nil {
		log.Printf("[Main] qzone client create failed: %v", err)

		refreshFn := task.RefreshCookie(cfg.Bot)
		newCookie, refreshErr := refreshFn()
		if refreshErr != nil {
			log.Fatalf("[Main] qzone client init failed and cookie refresh failed: %v", refreshErr)
		}

		qzClient, err = qzone.NewClient(newCookie,
			qzone.WithTimeout(cfg.Qzone.Timeout),
			qzone.WithMaxRetry(cfg.Qzone.MaxRetry),
			qzone.WithOnSessionExpired(task.RefreshCookie(cfg.Bot)),
		)
		if err != nil {
			log.Fatalf("[Main] qzone client recreate failed after cookie refresh: %v", err)
		}
	}

	if qzClient == nil {
		log.Fatal("[Main] qzone client is nil after initialization")
	}
	log.Println("[Main] qzone client created")

	if err := task.EnsureCookieValidOnStartup(cfg.Qzone, cfg.Bot, qzClient); err != nil {
		log.Printf("[Main] startup cookie validation failed: %v", err)
	}

	qqBot.SetClient(qzClient)

	worker := task.NewWorker(cfg.Worker, cfg.Wall, qzClient, st, renderer)
	worker.Start()
	defer worker.Stop()

	keepAlive := task.NewKeepAlive(cfg.Qzone, cfg.Bot, qzClient)
	keepAlive.Start()
	defer keepAlive.Stop()

	if cfg.Web.Enable {
		webServer := web.NewServer(cfg.Web, cfg.Wall, st, qzClient)
		go func() {
			if err := webServer.Start(); err != nil {
				log.Printf("[Main] web server stopped: %v", err)
			}
		}()
		defer webServer.Stop()
		log.Printf("[Main] web server started: %s", cfg.Web.Addr)
	}

	log.Println("[Main] system started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("[Main] got signal %v, shutting down...", s)
}

