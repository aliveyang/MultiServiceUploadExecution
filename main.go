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

const (
	Version = "1.0.0"
	Banner  = `
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
			configFile  string
			targetsStr  string
			parallelStr string
			maxWorkers  int
			webFlag     bool
			webAddr     string
			initFlag    bool
			versionFlag bool
		)

		flag.StringVar(&configFile, "c", "", "Path to configuration file (default: deploy.json or deploy.yaml)")
		flag.StringVar(&configFile, "config", "", "Path to configuration file")
		flag.StringVar(&targetsStr, "t", "", "Comma-separated target service names to deploy (e.g. -t web-1,api-1)")
		flag.StringVar(&targetsStr, "target", "", "Comma-separated target service names to deploy")
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
			fmt.Printf(Banner, Version)
			fmt.Println("Usage:")
			fmt.Println("  deploy [options]")
			fmt.Println("\nOptions:")
			fmt.Println("  -web                    Start Web configuration UI & Live Console")
			fmt.Println("  -addr <addr>            Web UI listen address, used with -web (default 127.0.0.1:8080)")
			fmt.Println("  -c, --config <file>     Specify configuration file path (default: deploy.json / deploy.yaml)")
			fmt.Println("  -t, --target <names>    Only deploy specific services (comma-separated, e.g. web-1,api-1)")
			fmt.Println("  -p, --parallel <bool>   Override concurrency mode: true (default) or false (serial)")
			fmt.Println("  -j, --max-workers <n>   Max parallel workers (default 10, 0 for unlimited)")
			fmt.Println("  -init                   Generate an example deploy.example.json template in current directory")
			fmt.Println("  -v, --version           Show tool version")
			fmt.Println("  -h, --help              Show help information")
			fmt.Println("\nExamples:")
			fmt.Println("  deploy -web             # Launch visual Web UI in browser (127.0.0.1:8080)")
			fmt.Println("  deploy -web -addr :9000 # Launch Web UI on a custom port")
			fmt.Println("  deploy -j 5             # Concurrently deploy with at most 5 workers")
			fmt.Println("  deploy -init            # Generate configuration template")
			fmt.Println("  deploy                  # Run deployment using deploy.json")
			fmt.Println("  deploy -c deploy.yaml -t api-server-01")
		}

	flag.Parse()

	if versionFlag {
		fmt.Printf("Multi-Service Deployment Tool v%s (Zero-dependency Go build)\n", Version)
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
		resolvedConfig := resolveConfigFile(configFile)
		if resolvedConfig == "" {
			resolvedConfig = "deploy.json"
		}
		server := web.NewServer(webAddr, resolvedConfig)
		// DEPLOY_NO_OPEN=1 时跳过自动打开浏览器（用于无头/CI 环境）
		autoOpen := os.Getenv("DEPLOY_NO_OPEN") != "1"
		if err := server.StartContext(ctx, autoOpen); err != nil {
			logger.Error("Failed to start Web server: %v", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf(Banner, Version)

	// 命令行模式：自动探测配置文件
	resolvedConfigPath := resolveConfigFile(configFile)
	if resolvedConfigPath == "" {
		logger.Error("Configuration file not found. Please provide one with '-c <path>' or run 'deploy -init' to create an example, or use 'deploy -web' for GUI.")
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
	}

		if strings.TrimSpace(targetsStr) != "" {
			parts := strings.Split(targetsStr, ",")
			opts.TargetServices = parts
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

// resolveConfigFile 查找配置文件：若未指定则依次查找 deploy.json, deploy.yaml, deploy.yml
func resolveConfigFile(specified string) string {
	if specified != "" {
		if _, err := os.Stat(specified); err == nil {
			return specified
		}
		return ""
	}

	defaults := []string{"deploy.json", "deploy.yaml", "deploy.yml"}
	for _, f := range defaults {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}
