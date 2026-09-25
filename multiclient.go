package main

import (
	"fmt"
	"log"
	"os"
	"strings"
)

func hasAdditionalClientArg(args []string) bool {
	for _, arg := range args {
		if arg == "--additional-client" {
			return true
		}
	}
	return false
}

// This path must stay separate from normal startup: no process cleanup,
// proxy ownership, cache deletion, binary patching or log truncation.
func runAdditionalClient() error {
	p, err := resolvePathsFrom(exeDir(), proxyModeFromArgs(os.Args[1:]))
	if err != nil {
		return err
	}
	if !portListening(bnetPort) {
		return fmt.Errorf("请先用原来的启动方式打开第一个游戏窗口，进入登录界面后再双开；当前没有运行中的代理")
	}
	wtf, err := os.ReadFile(p.clientWtf)
	if err != nil {
		return fmt.Errorf("读取游戏配置: %w", err)
	}
	if !usesLocalProxyPortal(string(wtf)) {
		return fmt.Errorf("游戏配置未指向本地 7000 代理；请先正常启动第一个游戏窗口")
	}
	log.Println("[Launcher] 复用已运行的代理打开额外游戏窗口；服务器沿用第一个窗口。请保持第一个启动器窗口打开。")
	return launchArctium(p)
}

func usesLocalProxyPortal(wtf string) bool {
	for _, line := range strings.Split(wtf, "\n") {
		if portalRe.MatchString(line) {
			parts := strings.Split(line, "\"")
			return len(parts) >= 3 && strings.EqualFold(parts[1], "127.0.0.1:7000")
		}
	}
	return false
}
