package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"filippo.io/edwards25519"
)

const mockAddr = "127.0.0.1:8080"

var mockSeed, _ = hex.DecodeString("25FD812475DCF26F9F1383AED37FC99E")

var ed25519PrivateKey = []byte{
	0x08, 0xBD, 0xC7, 0xA3, 0xCC, 0xC3, 0x4F, 0x3F,
	0x6A, 0x0B, 0xFF, 0xCF, 0x31, 0xC1, 0xB6, 0x97,
	0x69, 0x1E, 0x72, 0x9A, 0x0A, 0xAB, 0x2C, 0x77,
	0xC3, 0x6F, 0x8A, 0xE7, 0x5A, 0x9A, 0xA7, 0xC9,
}

var ed25519Context = []byte{
	0xA7, 0x1F, 0xB6, 0x9B, 0xC9, 0x7C, 0xDD, 0x96,
	0xE9, 0xBB, 0xB8, 0x21, 0x39, 0x8D, 0x5A, 0xD4,
}

func jsonOK(w http.ResponseWriter, content any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"code": 200, "msg": "ok", "content": content})
}

func startMock(buildInfo string) *http.Server {
	mux := http.NewServeMux()
	versions, cdns := buildTactFiles(buildInfo)

	logMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[mock] %s %s", r.Method, r.URL.RequestURI())
		if handleTact(w, r, versions, cdns) {
			return
		}
		mux.ServeHTTP(w, r)
	})

	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Hello, World!"})
	})
	mux.HandleFunc("/api/slide/list", func(w http.ResponseWriter, r *http.Request) { jsonOK(w, []any{}) })
	mux.HandleFunc("/api/index/live", func(w http.ResponseWriter, r *http.Request) { jsonOK(w, "") })
	mux.HandleFunc("/api/index/get_config", func(w http.ResponseWriter, r *http.Request) { jsonOK(w, "") })
	mux.HandleFunc("/api/index/get_new_version_info", func(w http.ResponseWriter, r *http.Request) { jsonOK(w, "") })
	mux.HandleFunc("/api/wx/create_wx_ewm", func(w http.ResponseWriter, r *http.Request) { jsonOK(w, "") })
	mux.HandleFunc("/api/time/get_now_time", handleGetNowTime)
	mux.HandleFunc("/api/time/get_now_time_1", handleGetNowTime1)
	mux.HandleFunc("/api/time/m1", handleM1)
	mux.HandleFunc("/api/time/get_now_time_wlk", handleWLK)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if handleTact(w, r, versions, cdns) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			jsonOK(w, "")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: mockAddr, Handler: logMux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("[Launcher] mock API on http://%s", mockAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Launcher] mock API error: %v", err)
		}
	}()
	return srv
}

func handleTact(w http.ResponseWriter, r *http.Request, versions, cdns string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/versions"):
		writeTact(w, r, versions)
		return true
	case strings.HasSuffix(path, "/cdns"), strings.HasSuffix(path, "/cdn"):
		writeTact(w, r, cdns)
		return true
	default:
		return false
	}
}

func writeTact(w http.ResponseWriter, r *http.Request, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

type buildInfo struct {
	region   string
	buildKey string
	cdnKey   string
	cdnPath  string
	hosts    string
	servers  string
	version  string
}

func parseBuildInfo(path string) (buildInfo, error) {
	var z buildInfo
	raw, err := os.ReadFile(path)
	if err != nil {
		return z, err
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return z, fmt.Errorf("short .build.info")
	}
	headers := strings.Split(lines[0], "|")
	values := strings.Split(lines[1], "|")
	get := func(name string) string {
		for i, h := range headers {
			key, _, _ := strings.Cut(h, "!")
			if strings.EqualFold(strings.TrimSpace(key), name) && i < len(values) {
				return values[i]
			}
		}
		return ""
	}
	z.region = get("Branch")
	z.buildKey = get("Build Key")
	z.cdnKey = get("CDN Key")
	z.cdnPath = get("CDN Path")
	z.hosts = get("CDN Hosts")
	z.servers = get("CDN Servers")
	z.version = get("Version")
	if z.buildKey == "" || z.version == "" {
		return z, fmt.Errorf("missing Build Key/Version in %s", path)
	}
	return z, nil
}

func buildTactFiles(buildInfoPath string) (versions, cdns string) {
	info := buildInfo{
		region:   "eu",
		buildKey: "c91609c69ed2ab39d44039390a1be969",
		cdnKey:   "1cff9f9c9690683bc0a75d7965aca302",
		cdnPath:  "tpr/wow",
		hosts:    "blzddist1-a.akamaihd.net level3.blizzard.com eu.cdn.blizzard.com cdn.arctium.tools",
		servers:  "http://blzddist1-a.akamaihd.net/?fallback=1&maxhosts=4 http://eu.cdn.blizzard.com/?maxhosts=4 http://level3.blizzard.com/?maxhosts=4 http://cdn.arctium.tools",
		version:  "3.4.3.54261",
	}
	if buildInfoPath != "" {
		if parsed, err := parseBuildInfo(buildInfoPath); err != nil {
			log.Printf("[Launcher] .build.info: %v (using built-in 3.4.3.54261)", err)
		} else {
			info = parsed
			log.Printf("[Launcher] TACT pin from %s (%s)", buildInfoPath, info.version)
		}
	} else {
		log.Println("[Launcher] TACT pin built-in 3.4.3.54261 (no .build.info)")
	}
	if !strings.Contains(info.hosts, "cdn.arctium.tools") {
		info.hosts = strings.TrimSpace(info.hosts + " cdn.arctium.tools")
	}
	if info.cdnPath == "" {
		info.cdnPath = "tpr/wow"
	}
	if info.servers == "" {
		info.servers = "http://cdn.arctium.tools"
	}
	buildID := info.version
	if i := strings.LastIndex(info.version, "."); i >= 0 {
		buildID = info.version[i+1:]
	}
	regions := []string{"us", "eu", "cn", "kr", "tw", "xx", "sg"}
	if info.region != "" {
		seen := false
		for _, r := range regions {
			if r == info.region {
				seen = true
				break
			}
		}
		if !seen {
			regions = append([]string{info.region}, regions...)
		}
	}

	var vb strings.Builder
	vb.WriteString("Region!STRING:0|BuildConfig!HEX:16|CDNConfig!HEX:16|KeyRing!HEX:16|BuildId!DEC:4|VersionsName!String:0|ProductConfig!HEX:16\n")
	vb.WriteString("## seqn = 1\n")
	for _, r := range regions {
		fmt.Fprintf(&vb, "%s|%s|%s||%s|%s|\n", r, info.buildKey, info.cdnKey, buildID, info.version)
	}
	versions = vb.String()

	var cb strings.Builder
	cb.WriteString("Name!STRING:0|Path!STRING:0|Hosts!STRING:0|Servers!STRING:0|ConfigPath!STRING:0\n")
	cb.WriteString("## seqn = 9999999\n")
	for _, r := range regions {
		fmt.Fprintf(&cb, "%s|%s|%s|%s|tpr/configs/data\n", r, info.cdnPath, info.hosts, info.servers)
	}
	cdns = cb.String()
	return versions, cdns
}

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}

func setBytesLE(b []byte) *big.Int { return new(big.Int).SetBytes(reverseBytes(b)) }

func toBytesLE32(n *big.Int) []byte {
	be := n.Bytes()
	out := make([]byte, 32)
	for i := 0; i < len(be) && i < 32; i++ {
		out[i] = be[len(be)-1-i]
	}
	return out
}

func handleGetNowTime(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if len(raw) < 115 {
		http.Error(w, "Data too short", 400)
		return
	}
	B, N, x := raw[8:40], raw[45:77], raw[94:114]
	BInt, NInt, xInt := setBytesLE(B), setBytesLE(N), setBytesLE(x)
	rnd := make([]byte, 32)
	rand.Read(rnd)
	a := setBytesLE(rnd)
	g := big.NewInt(7)
	A := new(big.Int).Exp(g, a, NInt)
	ABytes := toBytesLE32(A)
	h := sha1.Sum(append(append([]byte{}, ABytes...), B...))
	u := setBytesLE(h[:])
	k := big.NewInt(3)
	gx := new(big.Int).Exp(g, xInt, NInt)
	temp1 := new(big.Int).Add(BInt, new(big.Int).Mul(k, new(big.Int).Sub(NInt, gx)))
	temp1.Mod(temp1, NInt)
	temp2 := new(big.Int).Add(a, new(big.Int).Mul(u, xInt))
	S := new(big.Int).Exp(temp1, temp2, NInt)
	sData := toBytesLE32(S)
	keyData := make([]byte, 40)
	temp := make([]byte, 16)
	for i := 0; i < 16; i++ {
		temp[i] = sData[i*2]
	}
	kh := sha1.Sum(temp)
	for i := 0; i < 20; i++ {
		keyData[i*2] = kh[i]
	}
	for i := 0; i < 16; i++ {
		temp[i] = sData[i*2+1]
	}
	kh = sha1.Sum(temp)
	for i := 0; i < 20; i++ {
		keyData[i*2+1] = kh[i]
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(append(append(ABytes, B...), keyData...))
}

func handleGetNowTime1(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if len(raw) < 65 {
		http.Error(w, "data too short", 400)
		return
	}
	h := sha256.New()
	h.Write(raw[1:65])
	h.Write(mockSeed)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(append([]byte{0x01}, h.Sum(nil)...))
}

func bytesToU32(b []byte) uint32 {
	var n uint32
	for i := 0; i < len(b) && i < 4; i++ {
		n |= uint32(b[i]) << (8 * i)
	}
	return n
}

func jsonDirect(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func handleM1(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if len(raw) < 68 {
		http.Error(w, "data too short", 400)
		return
	}
	d := make([]int, 16)
	for i, v := range raw[22:38] {
		d[i] = int(v)
	}
	e := make([]int, 24)
	for i, v := range raw[38:62] {
		e[i] = int(v)
	}
	size := int(bytesToU32(raw[63:66]))
	if len(raw) < 67+size {
		http.Error(w, "data parse error", 400)
		return
	}
	jsonDirect(w, map[string]any{
		"a": int32(bytesToU32(raw[10:14])), "b": int32(bytesToU32(raw[14:18])),
		"c": int32(bytesToU32(raw[18:22])), "d": d, "e": e,
		"f": int32(bytesToU32(raw[2:6])), "g": string(raw[67 : 67+size]), "h": false,
	})
}

func handleWLK(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if len(raw) < 32 {
		http.Error(w, "data too short", 400)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(ed25519SignCtx(ed25519PrivateKey, raw[:32], ed25519Context))
}

func ed25519SignCtx(seed, message, ctx []byte) []byte {
	h := sha512.Sum512(seed)
	scalar, _ := edwards25519.NewScalar().SetBytesWithClamping(h[:32])
	pubPoint := (&edwards25519.Point{}).ScalarBaseMult(scalar)
	pubBytes := pubPoint.Bytes()
	prefix := []byte("SigEd25519 no Ed25519 collisions")
	dom2 := append(append(prefix, 0x00, byte(len(ctx))), ctx...)
	nonceH := sha512.New()
	nonceH.Write(dom2)
	nonceH.Write(h[32:64])
	nonceH.Write(message)
	r, _ := edwards25519.NewScalar().SetUniformBytes(nonceH.Sum(nil))
	R := (&edwards25519.Point{}).ScalarBaseMult(r)
	encodedR := R.Bytes()
	kH := sha512.New()
	kH.Write(dom2)
	kH.Write(encodedR)
	kH.Write(pubBytes)
	kH.Write(message)
	k, _ := edwards25519.NewScalar().SetUniformBytes(kH.Sum(nil))
	S := edwards25519.NewScalar().MultiplyAdd(k, scalar, r)
	sig := make([]byte, 64)
	copy(sig[:32], encodedR)
	copy(sig[32:], S.Bytes())
	return sig
}
