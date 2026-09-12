package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// handlePickPath 唤起操作系统原生的文件/文件夹选择框，获取宿主机真实绝对路径
func (s *Server) handlePickPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "folder" // 默认选择文件夹
	}

	selectedPath, err := pickNativeSystemPath(mode)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if selectedPath == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "canceled",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"path":   selectedPath,
	})
}

// pickNativeSystemPath 跨平台调用系统原生对话框，Windows 下调用 System.Windows.Forms
func pickNativeSystemPath(mode string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		var psScript string
		if mode == "file" {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = '请选择要部署上传的本地文件'; $f.Filter = '所有文件 (*.*)|*.*'; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Write($f.FileName) }`
		} else {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = '请选择要部署上传的本地目录 (文件夹)'; $d.ShowNewFolderButton = $true; if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Write($d.SelectedPath) }`
		}
		cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", psScript)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to open Windows file dialog: %w", err)
		}
		return strings.TrimSpace(string(out)), nil

	case "darwin":
		var appleScript string
		if mode == "file" {
			appleScript = `POSIX path of (choose file with prompt "请选择要上传的本地文件")`
		} else {
			appleScript = `POSIX path of (choose folder with prompt "请选择要上传的本地目录")`
		}
		cmd := exec.Command("osascript", "-e", appleScript)
		out, err := cmd.Output()
		if err != nil {
			// 用户点击取消时 osascript 会返回非零退出码
			return "", nil
		}
		return strings.TrimSpace(string(out)), nil

	default:
		// Linux: 优先尝试 zenity，若无则尝试 kdialog
		var cmd *exec.Cmd
		if mode == "file" {
			cmd = exec.Command("zenity", "--file-selection", "--title=请选择要上传的本地文件")
		} else {
			cmd = exec.Command("zenity", "--file-selection", "--directory", "--title=请选择要上传的本地目录")
		}
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
		return "", fmt.Errorf("native file dialog requires 'zenity' or desktop environment on Linux: %w", err)
	}
}

// openBrowser 跨平台自动打开浏览器
func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
