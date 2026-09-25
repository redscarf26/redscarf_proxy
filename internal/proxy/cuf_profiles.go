package proxy

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"redscarf/internal/modernworld"
)

func (s *Server) cufProfilePath(session *proxySession) (string, error) {
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	if session.legacy == nil || session.legacy.Username == "" || !session.hasRealm {
		return "", fmt.Errorf("CUF profiles require an authenticated account and selected realm")
	}
	// Hash structured identity fields to avoid path traversal and ambiguous keys.
	// Characters on the same account/realm intentionally share raid profiles.
	identity, err := json.Marshal([]any{strings.ToLower(strings.TrimSpace(s.config.LegacyAuth)), session.selectedRealm.ID, strings.ToUpper(session.legacy.Username)})
	if err != nil {
		return "", err
	}
	dir := s.config.CUFDataDir
	if dir == "" {
		dir = DefaultConfig().CUFDataDir
	}
	return filepath.Join(dir, fmt.Sprintf("%x", sha256.Sum256(identity)), "cuf.bin"), nil
}

func (s *Server) saveCUFProfiles(session *proxySession, body []byte) error {
	if err := modernworld.ValidateCUFProfiles(body); err != nil {
		return err
	}
	path, err := s.cufProfilePath(session)
	if err != nil {
		return err
	}
	s.cufMu.Lock()
	defer s.cufMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cuf-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(body); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Replace only after the complete new file has been flushed and closed.
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	if s.log != nil {
		s.log.Debug("CUF profiles saved", "bytes", len(body), "path", path)
	}
	return nil
}

func (s *Server) loadCUFProfiles(session *proxySession) ([]byte, error) {
	fallback := modernworld.EncodeLoadCUFProfiles(modernworld.DefaultCUFProfiles())
	path, err := s.cufProfilePath(session)
	if err != nil {
		return fallback, err
	}
	s.cufMu.Lock()
	defer s.cufMu.Unlock()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, modernworld.MaxCUFProfilesBytes+1))
	if err == nil {
		err = modernworld.ValidateCUFProfiles(body)
	}
	if err != nil {
		return fallback, err
	}
	return body, nil
}
