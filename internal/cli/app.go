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

	"gopaw/internal/app/routers"
	"gopaw/internal/config"
)

type appFlags struct {
	// 监听地址
	host string `flag:"host" default:"127.0.0.1" usage:"Bind host"`
	// 监听端口
	port int `flag:"port" default:"8088" usage:"Bind port"`
	// 是否启用自动重载（开发模式）
	reload bool `flag:"reload" default:"false" usage:"Enable auto-reload (dev only)"`
	// 预留的 worker 数量参数（目前只支持 1）
	workers int `flag:"workers" default:"1" usage:"Worker processes"`
	// 日志级别字符串（critical|error|warning|info|debug|trace）
	logLevel string `flag:"log-level" default:"info" usage:"Log level (critical|error|warning|info|debug|trace)"`
	// 需要在访问日志中隐藏的路径子串列表
	hideAccessPaths []string `flag:"hide-access-paths" default:"/console/push-messages" usage:"Path substrings to hide from access log (repeatable)" multi:"true"`
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
			engine.Use(gin.Recovery())
			engine.GET("/healthz", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})
			engine.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, "gopaw app is running\n")
			})
			// 将技能相关 HTTP 接口注册到引擎上。
			// 目前仓库里还没有 SkillService 的具体实现，因此先使用 stub。
			r := routers.NewRouter(&skillServiceStub{}, nil)
			r.Register(engine)
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

	// 使用反射工具函数，根据 appFlags 上的 tag 自动绑定 CLI 参数
	if err := BindFlagsFromStruct(cmd, f); err != nil {
		// 这里直接 panic / 返回错误都可以，根据项目风格选择。
		// 为了让调用方有机会处理，这里选择返回错误。
		panic(err)
	}

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

// skillServiceStub 是技能服务的最小占位实现。
// 目前仓库中只有路由/接口定义，没有实际的 SkillService 实现；
// 该 stub 仅用于确保接口注册与编译通过。
type skillServiceStub struct{}

func (s *skillServiceStub) ListAllSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	return nil, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) ListAvailableSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	return nil, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) DisableSkill(ctx context.Context, name string) (bool, error) {
	return false, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) EnableSkill(ctx context.Context, name string) (bool, error) {
	return false, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) CreateSkill(ctx context.Context, req routers.CreateSkillRequest) (bool, error) {
	return false, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) DeleteSkill(ctx context.Context, name string) (bool, error) {
	return false, errors.New("SkillService not implemented")
}

func (s *skillServiceStub) LoadSkillFile(ctx context.Context, skillName, source, filePath string) (string, error) {
	return "", errors.New("SkillService not implemented")
}
