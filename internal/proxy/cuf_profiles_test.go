package proxy

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"redscarf/internal/legacyauth"
	"redscarf/internal/modernworld"
)

func cufTestSession(account string, realm uint32) *proxySession {
	return &proxySession{legacy: &legacyauth.Session{Username: account}, hasRealm: true, selectedRealm: legacyauth.Realm{ID: realm}}
}

func TestCUFSaveDispatchAndRestart(t *testing.T) {
	config := DefaultConfig()
	config.CUFDataDir = t.TempDir()
	s := &Server{config: config}
	session := cufTestSession("player", 1)
	defaults := modernworld.EncodeLoadCUFProfiles(modernworld.DefaultCUFProfiles())
	if got, err := s.loadCUFProfiles(session); err != nil || !bytes.Equal(got, defaults) {
		t.Fatalf("first login=%x err=%v", got, err)
	}
	profiles := modernworld.DefaultCUFProfiles()
	profiles[0].Name = "治疗布局"
	profiles[0].FrameWidth = 123
	profiles[0].TopOffset = 65530
	profiles[0].BoolOptions[3] = true
	profiles = append(profiles, modernworld.CUFProfile{Name: "副配置", FrameHeight: 44})
	custom := modernworld.EncodeLoadCUFProfiles(profiles)
	for _, body := range [][]byte{defaults, custom, {0, 0, 0, 0}, custom} {
		handled, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: modernworld.CMSGSaveCUFProfiles, Body: body})
		if !handled || err != nil {
			t.Fatalf("save handled=%v err=%v", handled, err)
		}
		// A fresh server and session must restore disk data, including an
		// explicitly empty list instead of resurrecting the default profile.
		restarted := &Server{config: config}
		if got, err := restarted.loadCUFProfiles(cufTestSession("PLAYER", 1)); err != nil || !bytes.Equal(got, body) {
			t.Fatalf("restored=%x want=%x err=%v", got, body, err)
		}
	}
	for _, other := range []*proxySession{cufTestSession("other", 1), cufTestSession("player", 2)} {
		if got, err := s.loadCUFProfiles(other); err != nil || !bytes.Equal(got, defaults) {
			t.Fatalf("account/realm isolation=%x err=%v", got, err)
		}
	}
	otherAuth := config
	otherAuth.LegacyAuth = "another-server:3724"
	if got, err := (&Server{config: otherAuth}).loadCUFProfiles(session); err != nil || !bytes.Equal(got, defaults) {
		t.Fatalf("auth server isolation=%x err=%v", got, err)
	}
	if err := s.saveCUFProfiles(session, []byte{1, 0, 0, 0}); err == nil {
		t.Fatal("accepted truncated save")
	}
	if got, err := s.loadCUFProfiles(session); err != nil || !bytes.Equal(got, custom) {
		t.Fatalf("invalid save damaged previous profile: %x %v", got, err)
	}
}

func TestCUFStorageFailuresAndConcurrentAccess(t *testing.T) {
	config := DefaultConfig()
	config.CUFDataDir = t.TempDir()
	s := &Server{config: config}
	session := cufTestSession("../player", 1)
	body := modernworld.EncodeLoadCUFProfiles(modernworld.DefaultCUFProfiles())
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.saveCUFProfiles(session, body); err != nil {
				t.Error(err)
			}
			if got, err := s.loadCUFProfiles(session); err != nil || !bytes.Equal(got, body) {
				t.Errorf("load %x %v", got, err)
			}
		}()
	}
	wg.Wait()
	path, err := s.cufProfilePath(session)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(path)) != config.CUFDataDir {
		t.Fatal("escaped data directory")
	}
	for _, corrupt := range [][]byte{{1}, make([]byte, modernworld.MaxCUFProfilesBytes+1)} {
		if err := os.WriteFile(path, corrupt, 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := s.loadCUFProfiles(session); err == nil || !bytes.Equal(got, body) {
			t.Fatalf("corrupt fallback=%x %v", got, err)
		}
	}
	config.CUFDataDir = path // A regular file cannot be used as a directory.
	if err := (&Server{config: config}).saveCUFProfiles(session, body); err == nil {
		t.Fatal("write failure hidden")
	}
	if err := s.saveCUFProfiles(&proxySession{}, body); err == nil {
		t.Fatal("unauthenticated save accepted")
	}
}
