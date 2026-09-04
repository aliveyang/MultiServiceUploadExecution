package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// ANSI 颜色代码
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorBold    = "\033[1m"
)

var serviceColors = []string{
	ColorCyan,
	ColorMagenta,
	ColorYellow,
	ColorBlue,
	ColorGreen,
}

// OnLog 全局日志回调（例如 Web SSE 广播），每行日志输出时触发；nil 表示无回调
var OnLog func(line string)

// Logger 全局线程安全日志器
type Logger struct {
	mu  sync.Mutex
	out io.Writer
}

var defaultLogger = &Logger{out: os.Stdout}

// SetOutput 设置全局日志输出
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.out = w
}

// write 在锁内输出一行日志并触发回调
func (l *Logger) write(formatted string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.out, formatted)
	if OnLog != nil {
		OnLog(formatted)
	}
}

func timestamp() string { return time.Now().Format("15:04:05") }

// logf 输出全局级日志（带 [TAG] 前缀）
func logf(color, tag, format string, args ...interface{}) {
	defaultLogger.write(fmt.Sprintf("[%s] %s[%s]%s %s", timestamp(), color, tag, ColorReset, fmt.Sprintf(format, args...)))
}

// System 输出系统级普通信息
func System(format string, args ...interface{}) { logf(ColorBold+ColorBlue, "DEPLOY", format, args...) }

// Success 输出全局成功信息
func Success(format string, args ...interface{}) { logf(ColorBold+ColorGreen, "SUCCESS", format, args...) }

// Error 输出全局错误信息
func Error(format string, args ...interface{}) { logf(ColorBold+ColorRed, "ERROR", format, args...) }

// ServiceLogger 单个服务专用的彩色前缀日志器
type ServiceLogger struct {
	name  string
	color string
}

// NewServiceLogger 根据服务索引分配区分度明显的颜色
func NewServiceLogger(name string, index int) *ServiceLogger {
	idx := index % len(serviceColors)
	if idx < 0 {
		idx = (idx + len(serviceColors)) % len(serviceColors)
	}
	return &ServiceLogger{name: name, color: serviceColors[idx]}
}

func (l *ServiceLogger) prefix() string {
	return fmt.Sprintf("[%s] %s[%s]%s", timestamp(), l.color, l.name, ColorReset)
}

// emit 输出带颜色的服务日志行
func (l *ServiceLogger) emit(msg, color string) {
	defaultLogger.write(fmt.Sprintf("%s %s%s%s", l.prefix(), color, msg, ColorReset))
}

// Info 打印服务信息日志
func (l *ServiceLogger) Info(format string, args ...interface{}) {
	l.emit(fmt.Sprintf(format, args...), "")
}

// Success 打印服务阶段成功日志
func (l *ServiceLogger) Success(format string, args ...interface{}) {
	l.emit(fmt.Sprintf(format, args...), ColorGreen)
}

// Error 打印服务错误日志
func (l *ServiceLogger) Error(format string, args ...interface{}) {
	l.emit("[ERROR] "+fmt.Sprintf(format, args...), ColorRed)
}

// CommandOutput 输出命令执行的实时行
func (l *ServiceLogger) CommandOutput(line string) {
	defaultLogger.write(fmt.Sprintf("%s  | %s", l.prefix(), line))
}
