package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigProxyMode(t *testing.T) {
	directory := t.TempDir()
	if got := loadConfig(directory).proxyMode; got != "go" {
		t.Fatalf("default proxy mode = %q, want go", got)
	}
	config := []byte("[RedScarf]\nproxy=go\nserver_ip=10.0.0.2\nserver_port=13724\n")
	if err := os.WriteFile(filepath.Join(directory, "config.ini"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadConfig(directory)
	if got.proxyMode != "go" || got.realm != "10.0.0.2:13724" {
		t.Fatalf("unexpected config: %#v", got)
	}
}

func TestProxyModeFromArgs(t *testing.T) {
	if got := proxyModeFromArgs([]string{"--proxy=go"}); got != "go" {
		t.Fatalf("proxy override = %q", got)
	}
	if got := proxyModeFromArgs(nil); got != "" {
		t.Fatalf("proxy override = %q", got)
	}
}

func TestProxyReadyLine(t *testing.T) {
	if !isProxyReadyLine("BNet listen on 127.0.0.1:7000") {
		t.Fatal("ready line not detected")
	}
	if isProxyReadyLine("size=12 opcode= SMSG_UPDATE_OBJECT") {
		t.Fatal("packet line should not look ready")
	}
}

func TestClearQuestCacheRemovesQuestAndNPCTextCaches(t *testing.T) {
	clientDir := t.TempDir()
	questCache := filepath.Join(clientDir, "Cache", "WDB", "zhCN", "questcache.wdb")
	npcCache := filepath.Join(clientDir, "Cache", "WDB", "zhCN", "npccache.wdb")
	creatureCache := filepath.Join(clientDir, "Cache", "WDB", "zhCN", "creaturecache.wdb")
	writeDummyFile(t, questCache)
	writeDummyFile(t, npcCache)
	writeDummyFile(t, creatureCache)

	removed, err := clearQuestCache(clientDir)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(questCache); !os.IsNotExist(err) {
		t.Fatalf("quest cache still exists: %v", err)
	}
	if _, err := os.Stat(npcCache); !os.IsNotExist(err) {
		t.Fatalf("NPC cache still exists: %v", err)
	}
	if _, err := os.Stat(creatureCache); err != nil {
		t.Fatalf("unrelated cache was changed: %v", err)
	}
	removed, err = clearQuestCache(clientDir)
	if err != nil || removed {
		t.Fatalf("second clear removed=%v err=%v", removed, err)
	}
}

func TestResolvePathsGoModeDoesNotRequireSeparateProxy(t *testing.T) {
	base := t.TempDir()
	writeDummyFile(t, filepath.Join(base, "tools", "arctium-launcher", "Arctium WoW Launcher.exe"))
	clientDir := filepath.Join(base, "_classic_")
	writeDummyFile(t, filepath.Join(clientDir, "WowClassic.exe"))
	if err := os.WriteFile(filepath.Join(base, "config.ini"), []byte("[RedScarf]\nproxy=go\nwow_path="+clientDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolvePathsFrom(base, "go")
	if err != nil {
		t.Fatal(err)
	}
	if got.proxyMode != "go" {
		t.Fatalf("proxy mode = %q, want go", got.proxyMode)
	}
	if got.arctiumExe != filepath.Join(base, "tools", "arctium-launcher", "Arctium WoW Launcher.exe") {
		t.Fatalf("arctium = %q", got.arctiumExe)
	}
}

func TestResolvePathsPrefersArctiumBesideLauncher(t *testing.T) {
	base := t.TempDir()
	writeDummyFile(t, filepath.Join(base, "Arctium WoW Launcher.exe"))
	writeDummyFile(t, filepath.Join(base, "tools", "arctium-launcher", "Arctium WoW Launcher.exe"))
	clientDir := filepath.Join(base, "_classic_")
	writeDummyFile(t, filepath.Join(clientDir, "WowClassic.exe"))
	got, err := resolvePathsFrom(base, "go")
	if err != nil {
		t.Fatal(err)
	}
	if got.arctiumExe != filepath.Join(base, "Arctium WoW Launcher.exe") {
		t.Fatalf("arctium = %q", got.arctiumExe)
	}
}

func writeDummyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
