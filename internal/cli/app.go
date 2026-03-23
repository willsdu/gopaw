package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"

	"gopaw/internal/agent"
	"gopaw/internal/app/routers"
	"gopaw/internal/config"
)

type appFlags struct {
	// 监听地址
	host string
	// 监听端口
	port int
	// 是否启用自动重载（开发模式）
	reload bool
	// 预留的 worker 数量参数（目前只支持 1）
	workers int
	// 日志级别字符串（critical|error|warning|info|debug|trace）
	logLevel string
	// 需要在访问日志中隐藏的路径子串列表
	hideAccessPaths []string
}

// registerRouters 注册基础路由
func registerRouters(engine *gin.Engine) {
	engine.Use(gin.Recovery())
	engine.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	engine.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "gopaw app is running\n")
	})
	// 将技能相关 HTTP 接口注册到引擎上。
	r := routers.NewRouter(agent.NewManagerService(), nil)
	r.Register(engine)
}

func newAppCmd(opts rootOpts, rootFlags *rootFlags) *cobra.Command {
	f := &appFlags{}

	cmd := &cobra.Command{
		Use:   "app",
		Short: "Run GoPaw app server", // 运行 GoPaw 应用服务
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// 将最近一次使用的 API 地址写入配置，便于其他命令复用
			if err := config.WriteLastAPI("gopaw", f.host, f.port); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warn: failed to persist last api:", err)
			}

			// 使用环境变量标记是否启用 reload 模式
			if f.reload {
				_ = os.Setenv("GOPAW_RELOAD_MODE", "1")
			} else {
				_ = os.Unsetenv("GOPAW_RELOAD_MODE")
			}

			// 解析日志级别并设置全局默认 logger
			level, err := parseLogLevel(f.logLevel)
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level}))
			slog.SetDefault(logger)

			if f.workers != 1 {
				fmt.Fprintln(cmd.ErrOrStderr(), "warn: --workers is accepted for compatibility, but currently only 1 worker is supported")
			}

			// 监听指定 host/port
			addr := net.JoinHostPort(f.host, fmt.Sprintf("%d", f.port))
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", addr, err)
			}

			// gin 路由：用于挂载 /api/skills 等接口
			engine := gin.New()
			// 注册基础路由
			registerRouters(engine)
			srv := &http.Server{
				Handler:           accessLogMiddleware(engine, f.hideAccessPaths),
				ReadHeaderTimeout: 10 * time.Second,
			}

			// 监听中断/终止信号，用于优雅关闭服务
			sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() {
				// 后台启动 HTTP 服务
				slog.Info("server started", "addr", addr, "config", rootFlags.configPath, "reload", f.reload)
				errCh <- srv.Serve(ln)
			}()

			select {
			case <-sigCtx.Done():
				// 收到退出信号后，给一个超时时间做优雅关闭
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				slog.Info("server stopped")
				return nil
			case err := <-errCh:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			}
		},
	}

	cmd.SetOut(opts.Out)
	cmd.SetErr(opts.Err)

	// 手动绑定 CLI 参数，避免使用反射
	cmd.Flags().StringVar(&f.host, "host", "127.0.0.1", "Bind host")
	cmd.Flags().IntVar(&f.port, "port", 8088, "Bind port")
	cmd.Flags().BoolVar(&f.reload, "reload", false, "Enable auto-reload (dev only)")
	cmd.Flags().IntVar(&f.workers, "workers", 1, "Worker processes")
	cmd.Flags().StringVar(&f.logLevel, "log-level", "info", "Log level (critical|error|warning|info|debug|trace)")
	cmd.Flags().StringSliceVar(&f.hideAccessPaths, "hide-access-paths", []string{"/console/push-messages"}, "Path substrings to hide from access log (repeatable)")

	return cmd
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "error":
		return slog.LevelError, nil
	case "warning", "warn":
		return slog.LevelWarn, nil
	case "info":
		return slog.LevelInfo, nil
	case "debug", "trace":
		return slog.LevelDebug, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid --log-level: %q (expected critical|error|warning|info|debug|trace)", s)
	}
}

func accessLogMiddleware(next http.Handler, hideSubstrings []string) http.Handler {
	var hides []string
	for _, s := range hideSubstrings {
		s = strings.TrimSpace(s)
		if s != "" {
			hides = append(hides, s)
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)

		path := r.URL.Path
		for _, sub := range hides {
			if strings.Contains(path, sub) {
				return
			}
		}
		slog.Info("access", "method", r.Method, "path", path, "remote", r.RemoteAddr, "dur_ms", time.Since(start).Milliseconds())
	})
}
