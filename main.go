package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"redscarf/internal/proxy"
)

const (
	bnetPort     = 7000
	defaultRealm = "127.0.0.1:3724"
)

var portalRe = regexp.MustCompile(`(?i)^\s*SET\s+portal\s+"[^"]*"\s*$`)

type paths struct {
	root       string
	proxyMode  string
	arctiumExe string
	clientDir  string
	clientExe  string
	clientWtf  string
	realm      string
}

func main() {
	if hasAdditionalClientArg(os.Args[1:]) {
		if err := runAdditionalClient(); err != nil {
			log.Fatal(err)
		}
		return
	}
	baseDir := exeDir()
	logFile, err := os.OpenFile(filepath.Join(baseDir, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	}
	log.SetFlags(log.LstdFlags)

	p, err := resolvePathsFrom(exeDir(), proxyModeFromArgs(os.Args[1:]))
	if err != nil {
		log.Fatalf("[Launcher] %v", err)
	}
	log.Printf("[Launcher] root   = %s", p.root)
	log.Printf("[Launcher] wow    = %s", p.clientExe)
	log.Printf("[Launcher] realm  = %s", p.realm)

	killStale()
	removed, err := clearQuestCache(p.clientDir)
	if err != nil {
		log.Fatalf("[Launcher] clear stale text caches: %v", err)
	}
	if removed {
		log.Println("[Launcher] stale quest/NPC text caches cleared")
	}
	mock := startMock(findBuildInfo(p.clientDir))
	defer mock.Close()
	time.Sleep(300 * time.Millisecond)

	if err := setPortal(p.clientWtf, bnetPort); err != nil {
		log.Fatalf("[Launcher] Config.wtf: %v", err)
	}
	if err := serveEmbeddedProxy(p); err != nil {
		log.Fatalf("[Launcher] %v", err)
	}
}

func proxyModeFromArgs(arguments []string) string {
	for _, argument := range arguments {
		value := strings.TrimSpace(strings.ToLower(argument))
		switch value {
		case "--proxy=go", "-proxy=go":
			return "go"
		}
	}
	return ""
}

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir, err := filepath.EvalSymlinks(filepath.Dir(exe)); err == nil {
			return dir
		}
		return filepath.Dir(exe)
	}
	cwd, _ := os.Getwd()
	return cwd
}

type appConfig struct {
	wowPath     string
	serverIP    string
	serverPort  string
	realm       string
	proxyMode   string
	arctiumPath string
}

func loadConfig(dir string) appConfig {
	cfg := appConfig{realm: defaultRealm, serverIP: "127.0.0.1", serverPort: "3724", proxyMode: "go"}
	data, err := os.ReadFile(filepath.Join(dir, "config.ini"))
	if err != nil {
		return cfg
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || (strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")) {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.ToLower(k))
		v = strings.TrimSpace(strings.Trim(v, `"'`))
		switch k {
		case "wow_path", "wow", "client_dir", "client", "path":
			cfg.wowPath = v
		case "server_ip", "ip", "host":
			if v != "" {
				cfg.serverIP = v
			}
		case "server_port", "port":
			if v != "" {
				cfg.serverPort = v
			}
		case "realm", "realm_addr":
			if v != "" {
				cfg.realm = v
			}
		case "proxy", "proxy_mode", "use_go_proxy":
			cfg.proxyMode = "go"
		case "arctium_path", "arctium":
			if v != "" {
				cfg.arctiumPath = v
			}
		}
	}
	if cfg.serverIP != "" && cfg.serverPort != "" {
		cfg.realm = cfg.serverIP + ":" + cfg.serverPort
	}
	return cfg
}

func wowDirFromPath(p string) string {
	p = filepath.Clean(p)
	if fileExists(p) && strings.EqualFold(filepath.Base(p), "WowClassic.exe") {
		return filepath.Dir(p)
	}
	if fileExists(filepath.Join(p, "WowClassic.exe")) {
		return p
	}
	if fileExists(filepath.Join(p, "_classic_", "WowClassic.exe")) {
		return filepath.Join(p, "_classic_")
	}
	return ""
}

func findClientDir(base, configured string) string {
	var candidates []string
	if configured != "" {
		if dir := wowDirFromPath(configured); dir != "" {
			return dir
		}
		candidates = append(candidates, configured)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "_classic_"))
	}
	candidates = append(candidates,
		filepath.Join(base, "_classic_"),
		filepath.Join(filepath.Dir(base), "_classic_"),
	)
	seen := map[string]bool{}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if got := wowDirFromPath(dir); got != "" {
			return got
		}
	}
	return ""
}

func resolvePaths() (paths, error) {
	return resolvePathsFrom(exeDir(), proxyModeFromArgs(os.Args[1:]))
}

func resolvePathsFrom(base, proxyOverride string) (paths, error) {
	var p paths
	p.root = base
	cfg := loadConfig(base)
	p.realm = cfg.realm
	p.proxyMode = cfg.proxyMode
	if proxyOverride != "" {
		p.proxyMode = proxyOverride
	}
	p.arctiumExe = findArctium(base, cfg.arctiumPath)
	cfgClient := cfg.wowPath

	if !fileExists(p.arctiumExe) {
		return p, fmt.Errorf("Arctium not found: %s", p.arctiumExe)
	}

	p.clientDir = findClientDir(base, cfgClient)
	if p.clientDir == "" {
		return p, fmt.Errorf("找不到 WowClassic.exe。请在 config.ini 里设置 wow_path")
	}
	p.clientExe = filepath.Join(p.clientDir, "WowClassic.exe")
	p.clientWtf = filepath.Join(p.clientDir, "WTF", "Config.wtf")
	return p, nil
}

func findBuildInfo(clientDir string) string {
	for _, p := range []string{
		filepath.Join(filepath.Dir(clientDir), ".build.info"),
		filepath.Join(clientDir, ".build.info"),
	} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func findArctium(base, configured string) string {
	if configured != "" {
		if filepath.IsAbs(configured) {
			return configured
		}
		return filepath.Join(base, configured)
	}
	beside := filepath.Join(base, "Arctium WoW Launcher.exe")
	if fileExists(beside) {
		return beside
	}
	legacy := filepath.Join(base, "tools", "arctium-launcher", "Arctium WoW Launcher.exe")
	if fileExists(legacy) {
		return legacy
	}
	return beside
}

func killStale() {
	killOtherLaunchers()
	names := []string{
		"redscarf-proxy.exe",
		"WowClassic.exe",
		"Arctium WoW Launcher.exe",
		"Arctium Game Launcher.exe",
	}
	for _, name := range names {
		exec.Command("taskkill", "/IM", name, "/F").Run()
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !portListening(bnetPort) {
			log.Println("[Launcher] port is free")
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	log.Println("[Launcher] WARNING: port did not free up in time")
}

func clearQuestCache(clientDir string) (bool, error) {
	removed := false
	for _, name := range []string{"questcache.wdb", "npccache.wdb"} {
		cachePath := filepath.Join(clientDir, "Cache", "WDB", "zhCN", name)
		err := os.Remove(cachePath)
		switch {
		case err == nil:
			removed = true
		case os.IsNotExist(err):
			continue
		default:
			return removed, fmt.Errorf("remove %s: %w", cachePath, err)
		}
	}
	return removed, nil
}

func portListening(port int) bool {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		return false
	}
	needle := fmt.Sprintf(":%d ", port)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, "LISTENING") {
			return true
		}
	}
	return false
}

func setPortal(wtfPath string, port int) error {
	if !fileExists(wtfPath) {
		log.Printf("[Launcher] WARNING: %s not found, skip portal", wtfPath)
		return nil
	}
	data, err := os.ReadFile(wtfPath)
	if err != nil {
		return err
	}
	text := string(data)
	useCRLF := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	target := fmt.Sprintf("SET portal \"127.0.0.1:%d\"", port)
	found := false
	for i, line := range lines {
		if portalRe.MatchString(line) {
			lines[i] = target
			found = true
			break
		}
	}
	if !found {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines[n-1] = target
			lines = append(lines, "")
		} else {
			lines = append(lines, target)
		}
	}
	body := strings.Join(lines, "\n")
	if useCRLF {
		body = strings.ReplaceAll(body, "\n", "\r\n")
	}
	if err := os.WriteFile(wtfPath, []byte(body), 0644); err != nil {
		return err
	}
	log.Printf("[Launcher] Config.wtf portal -> 127.0.0.1:%d", port)
	return nil
}

func killOtherLaunchers() {
	self := os.Getpid()
	output, err := exec.Command("tasklist.exe", "/FI", "IMAGENAME eq redscarf.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return
	}
	records, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
	if err != nil {
		return
	}
	for _, record := range records {
		if len(record) < 2 || !strings.EqualFold(strings.TrimSpace(record[0]), "redscarf.exe") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil || pid == 0 || pid == self {
			continue
		}
		exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/F").Run()
	}
}

func serveEmbeddedProxy(p paths) error {
	config := proxy.DefaultConfig()
	config.LegacyAuth = p.realm
	writers := []io.Writer{os.Stdout}
	logFile, err := os.OpenFile(filepath.Join(p.root, "proxy.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[Launcher] proxy.log: %v", err)
	} else {
		defer logFile.Close()
		writers = append(writers, logFile)
	}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: slog.LevelInfo}))
	server, err := proxy.New(config, logger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.Run(ctx) }()
	if err := waitBNet(errCh, 20*time.Second); err != nil {
		stop()
		return err
	}
	log.Println("[Launcher] proxy ready")
	log.Println("[Launcher] You can log in now.")
	if err := launchArctium(p); err != nil {
		stop()
		return fmt.Errorf("Arctium: %w", err)
	}
	log.Println("[Launcher] done. Keep this window open while playing.")
	err = <-errCh
	if err != nil && ctx.Err() == nil {
		return err
	}
	log.Println("[Launcher] Done.")
	return nil
}

func waitBNet(errCh <-chan error, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-errCh:
			if err == nil {
				return fmt.Errorf("proxy stopped before BNet was ready")
			}
			return err
		case <-deadline.C:
			return fmt.Errorf("proxy did not listen on port %d", bnetPort)
		case <-tick.C:
			if portListening(bnetPort) {
				return nil
			}
		}
	}
}

func isProxyReadyLine(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "bnet") &&
		(strings.Contains(line, "启动") || strings.Contains(low, "listen") || strings.Contains(line, "7000"))
}

func launchArctium(p paths) error {
	if !fileExists(p.clientExe) {
		return fmt.Errorf("client not found: %s", p.clientExe)
	}
	args := []string{
		"--version=Classic",
		"--path", p.clientDir,
		"--dev",
		"-config", "Config.wtf",
	}
	log.Printf("[Launcher] starting Arctium ...")
	log.Printf("[Launcher]   %s %s", p.arctiumExe, strings.Join(args, " "))
	cmd := exec.Command(p.arctiumExe, args...)
	cmd.Dir = p.clientDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Arctium exited: %v", err)
		}
		log.Println("[Launcher] Arctium finished patching")
		return nil
	case <-time.After(60 * time.Second):
		log.Println("[Launcher] WARNING: Arctium still running after 60s (client may already be up)")
		return nil
	}
}
