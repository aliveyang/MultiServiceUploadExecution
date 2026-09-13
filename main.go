package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
	"multi-service-deploy/web"
)

var (
	Version = "1.3.0"
)

const (
	Banner = `
  __  __       _ _   _ _____                 _          
 |  \/  |_   _| | |_(_)  ___|__ _ __ __   _(_) ___ ___ 
 | |\/| | | | | | __| | |_ / _ \ '__|\ \ / / |/ __/ _ \
 | |  | | |_| | | |_| |  _|  __/ |    \ V /| | (_|  __/
 |_|  |_|\__,_|_|\__|_|_|  \___|_|     \_/ |_|\___\___|
              Multi-Service Deployment Tool (v%s)
`
)

func main() {
	var (
		configFile   string
		workspaceStr string
		targetsStr   string
		tagsStr      string
		typesStr     string
		parallelStr  string
		maxWorkers   int
		webFlag      bool
		webAddr      string
		initFlag     bool
		versionFlag  bool
	)

	flag.StringVar(&configFile, "c", "", "Path to configuration file (default: deploy.json or deploy.yaml)")
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.StringVar(&workspaceStr, "w", "", "Target workspace to deploy or open (e.g. -w prod, -w default)")
	flag.StringVar(&workspaceStr, "workspace", "", "Target workspace to deploy or open")
	flag.StringVar(&targetsStr, "t", "", "Comma-separated target service names to deploy (e.g. -t web-1,api-1)")
	flag.StringVar(&targetsStr, "target", "", "Comma-separated target service names to deploy")
	flag.StringVar(&tagsStr, "g", "", "Comma-separated target tags filter, union semantics (e.g. -g backend,data)")
	flag.StringVar(&tagsStr, "tags", "", "Comma-separated target tags filter, union semantics")
	flag.StringVar(&typesStr, "type", "", "Deploy type filter matched against service steps (e.g. --type exec_only, standard, sync_only)")
	flag.StringVar(&parallelStr, "p", "", "Override parallel mode (true or false)")
	flag.StringVar(&parallelStr, "parallel", "", "Override parallel mode (true or false)")
	flag.IntVar(&maxWorkers, "j", 10, "Maximum concurrent worker goroutines when in parallel mode (default 10)")
	flag.IntVar(&maxWorkers, "max-workers", 10, "Maximum concurrent worker goroutines when in parallel mode")
	flag.BoolVar(&webFlag, "web", false, "Start Web-based configuration & deployment UI")
	flag.StringVar(&webAddr, "addr", "127.0.0.1:8080", "Web UI listen address, used with -web (default 127.0.0.1:8080)")
	flag.BoolVar(&initFlag, "init", false, "Generate example configuration file (deploy.example.json)")
	flag.BoolVar(&versionFlag, "v", false, "Print version information")
	flag.BoolVar(&versionFlag, "version", false, "Print version information")

	flag.Usage = func() {
		fmt.Printf(Banner, strings.TrimPrefix(Version, "v"))
		fmt.Println("Usage:")
		fmt.Println("  deploy [options]")
		fmt.Println("\nOptions:")
		fmt.Println("  -web                    Start Web configuration UI & Live Console")
		fmt.Println("  -addr <addr>            Web UI listen address, used with -web (default 127.0.0.1:8080)")
		fmt.Println("  -w, --workspace <name>  Specify isolated workspace to deploy (e.g. -w prod, -w default)")
		fmt.Println("  -c, --config <file>     Specify configuration file path (default: deploy.json / deploy.yaml)")
		fmt.Println("  -g, --tags <names>      Filter by service tags, union semantics (comma-separated, e.g. -g backend,data)")
		fmt.Println("  -t, --target <names>    Only deploy specific services by name (comma-separated, e.g. -t web-1,api-1)")
		fmt.Println("      --type <type>       Filter deploy by task type: standard, exec_only, sync_only")
		fmt.Println("  -p, --parallel <bool>   Override concurrency mode: true (default) or false (serial)")
		fmt.Println("  -j, --max-workers <n>   Max parallel workers within each stage (default 10, 0 for unlimited)")
		fmt.Println("  -init                   Generate an example deploy.example.json template in current directory")
		fmt.Println("  -v, --version           Show tool version")
		fmt.Println("  -h, --help              Show help information")
		fmt.Println("\nExamples:")
		fmt.Println("  deploy -web                     # Launch visual Web UI in browser (127.0.0.1:8080)")
		fmt.Println("  deploy -w prod                  # Deploy default scenario in 'prod' workspace")
		fmt.Println("  deploy -g backend,data          # Deploy all services tagged 'backend' or 'data' (union)")
		fmt.Println("  deploy -g backend --type exec_only # Deploy exec-only tasks among 'backend' tagged services")
		fmt.Println("  deploy -t api-01                # Deploy single service by name")
		fmt.Println("  deploy --type exec_only         # Only execute remote commands (e.g. migrations, restarts)")
		fmt.Println("  deploy -g backend -j 5          # Concurrently deploy backend-tagged services with 5 workers")
		fmt.Println("  deploy -c deploy.yaml -t api-01 # Deploy single service with custom config")
	}

	flag.Parse()

	if versionFlag {
		fmt.Printf("Multi-Service Deployment Tool v%s (Zero-dependency Go build)\n", strings.TrimPrefix(Version, "v"))
		os.Exit(0)
	}

	// 初始化示例配置文件
	if initFlag {
		targetFile := "deploy.example.json"
		if err := os.WriteFile(targetFile, []byte(config.GenerateExampleJSON()), 0644); err != nil {
			logger.Error("Failed to create example config file: %v", err)
			os.Exit(1)
		}
		logger.Success("Example configuration file generated: %s", targetFile)
		logger.System("You can copy it to deploy.json and modify it with your servers.")
		os.Exit(0)
	}

	// 监听中断信号 (Ctrl+C / SIGTERM)，实现全链路生命周期控制
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 启动 Web 管理界面
	if webFlag {
		fmt.Printf(Banner, Version)
		resolvedConfig := resolveConfigFile(configFile, workspaceStr)
		if resolvedConfig == "" {
			resolvedConfig = "deploy.json"
		}

		// DEPLOY_NO_OPEN=1 时强制跳过自动打开浏览器（用于无头/CI 环境）；
		// 否则遵循 settings.json 的 autoOpen（未配置时默认打开）
		autoOpen := os.Getenv("DEPLOY_NO_OPEN") != "1"

		// settings.json 中的监听地址仅在用户未显式指定 -addr 时生效（默认值恒为回环 127.0.0.1:8080）
		if st, err := config.LoadSettings(config.ResolveWorkspaceDir(resolvedConfig)); err == nil && st != nil {
			if strings.TrimSpace(st.ListenAddr) != "" && webAddr == "127.0.0.1:8080" {
				webAddr = strings.TrimSpace(st.ListenAddr)
				logger.System("Using listen address from settings.json: %s", webAddr)
			}
			if st.AutoOpenBrowser != nil {
				autoOpen = autoOpen && *st.AutoOpenBrowser
			}
		}

		server := web.NewServer(webAddr, resolvedConfig)
		if workspaceStr != "" && config.IsValidWorkspaceID(workspaceStr) {
			server.SetCurrentWorkspace(workspaceStr)
		}
		if err := server.StartContext(ctx, autoOpen); err != nil {
			logger.Error("Failed to start Web server: %v", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf(Banner, Version)

	// 命令行模式：自动探测配置文件
	resolvedConfigPath := resolveConfigFile(configFile, workspaceStr)
	if resolvedConfigPath == "" {
		if workspaceStr != "" {
			logger.Error("Workspace %q not found in %s directory. Run 'deploy -web' to manage workspaces.", workspaceStr, config.DefaultWorkspaceDir)
		} else {
			logger.Error("Configuration file not found. Please provide one with '-c <path>', '-w <workspace>', or run 'deploy -init' to create an example, or use 'deploy -web' for GUI.")
		}
		os.Exit(1)
	}

	logger.System("Loading configuration: %s", resolvedConfigPath)
	cfg, err := config.LoadConfig(resolvedConfigPath)
	if err != nil {
		logger.Error("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// 构建运行选项
	opts := deployer.DeployOptions{
		MaxWorkers: maxWorkers,
		ConfigPath: resolvedConfigPath,
	}

	if strings.TrimSpace(targetsStr) != "" {
		opts.TargetServices = strings.Split(targetsStr, ",")
	}
	if strings.TrimSpace(tagsStr) != "" {
		opts.TargetTags = strings.Split(tagsStr, ",")
	}
	if strings.TrimSpace(typesStr) != "" {
		opts.TargetTypes = strings.Split(typesStr, ",")
	}

	if parallelStr != "" {
		p := strings.ToLower(parallelStr) == "true" || parallelStr == "1"
		opts.Parallel = &p
	}

	// 启动部署调度
	mgr := deployer.NewDeployManager(cfg, opts)
	allSuccess, err := mgr.RunWithContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			logger.Error("Deployment interrupted by user signal: %v", ctx.Err())
		} else {
			logger.Error("Deployment failed: %v", err)
		}
		os.Exit(1)
	}

	if !allSuccess {
		os.Exit(1)
	}
}

// resolveConfigFile 查找配置文件：
// 1. 若显式指定 -c/--config，优先读取该文件；
// 2. 若指定 -w/--workspace，在 workspaces/ 目录下查找对应文件；
// 3. 若均未指定，优先查找 workspaces/default.json，其次查找当前目录下 deploy.json, deploy.yaml, deploy.yml
func resolveConfigFile(specifiedConfig, specifiedWorkspace string) string {
	if specifiedConfig != "" {
		if _, err := os.Stat(specifiedConfig); err == nil {
			return specifiedConfig
		}
		return ""
	}

	if specifiedWorkspace != "" {
		if config.IsValidWorkspaceID(specifiedWorkspace) {
			if wsPath, err := config.GetWorkspacePath(config.DefaultWorkspaceDir, specifiedWorkspace); err == nil {
				if _, err := os.Stat(wsPath); err == nil {
					return wsPath
				}
			}
		}
		return ""
	}

	// 优先查找 default 工作空间
	defaultWsPath, err := config.GetWorkspacePath(config.DefaultWorkspaceDir, config.DefaultWorkspaceID)
	if err == nil {
		if _, err := os.Stat(defaultWsPath); err == nil {
			return defaultWsPath
		}
	}

	defaults := []string{"deploy.json", "deploy.yaml", "deploy.yml"}
	for _, f := range defaults {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}
