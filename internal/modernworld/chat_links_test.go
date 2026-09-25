package modernworld

import (
	"bytes"
	"strings"
	"testing"
)

func TestLegacyChatItemLinks(t *testing.T) {
	const modern = "|cffffffff|Hitem:29476::::::::63:::::::::|h[赤色水晶碎片]|h|r"
	const legacy = "|cffffffff|Hitem:29476:0:0:0:0:0:0:0:63|h[赤色水晶碎片]|h|r"
	for _, tt := range []struct{ name, input, want string }{
		{"questie", "{rt1} Questie: 拾取 " + modern + " 自动触发任务 [[63] 任务 (10134)]!", "{rt1} Questie: 拾取 " + legacy + " 自动触发任务 [[63] 任务 (10134)]!"},
		{"multiple", modern + " " + modern, legacy + " " + legacy},
		{"legacy", legacy, legacy},
		{"properties", "|Hitem:123:45:6:7:8:0:-12:345:63:99:0|h[物品]|h", "|Hitem:123:45:6:7:8:0:-12:345:63|h[物品]|h"},
		{"empty level", "|Hitem:123:::::::::::::::::|h[物品]|h", "|Hitem:123:0:0:0:0:0:0:0:0|h[物品]|h"},
		{"other link", "|cffffff00|Hquest:10134:63|h[任务]|h|r", "|cffffff00|Hquest:10134:63|h[任务]|h|r"},
		{"escaped", "||Hitem:29476::::::::63:::::::::||h[x]", "||Hitem:29476::::::::63:::::::::||h[x]"},
		{"short", "|Hitem:123|h[x]|h", "|Hitem:123|h[x]|h"},
		{"unterminated", "hello |Hitem:123::::::::63::::", "hello |Hitem:123::::::::63::::"},
		{"plain", "中文聊天 |", "中文聊天 |"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := legacyChatItemLinks(tt.input, 0); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	for _, language := range []uint32{legacyLanguageAddon, modernLanguageAddon, modernLanguageLogged} {
		if got := legacyChatItemLinks(modern, language); got != modern {
			t.Fatalf("modified addon language %d: %q", language, got)
		}
	}
}

func TestChatItemLinksAllOutgoingRoutes(t *testing.T) {
	const modern = "|cffffffff|Hitem:29476::::::::63:::::::::|h[赤色水晶碎片]|h|r"
	const legacy = "|cffffffff|Hitem:29476:0:0:0:0:0:0:0:63|h[赤色水晶碎片]|h|r"
	routes := map[string]func(string) ([]byte, error){
		"say": func(s string) ([]byte, error) { return EncodeLegacyChatMessage(legacyChatSay, 0, s) },
		"whisper": func(s string) ([]byte, error) {
			return EncodeLegacyChatWhisper(ChatWhisperRequest{Target: "Alice", Text: s})
		},
		"channel": func(s string) ([]byte, error) {
			return EncodeLegacyChatChannelMessage(ChatChannelMessageRequest{Target: "General", Text: s})
		},
	}
	for name, encode := range routes {
		t.Run(name, func(t *testing.T) {
			body, err := encode(modern)
			if err != nil || !bytes.HasSuffix(body, []byte(legacy+"\x00")) {
				t.Fatalf("body=%q err=%v", body, err)
			}
			// Zero expansion can increase the byte count: enforce the limit on
			// the converted text, not just on what the modern client sent.
			const compact = "|Hitem:29476::::::::63|h[物品]|h"
			input := strings.Repeat("a", maxLegacyChatText-len(compact)) + compact
			if _, err := encode(input); err == nil {
				t.Fatal("accepted text exceeding the legacy limit after conversion")
			}
		})
	}
}
