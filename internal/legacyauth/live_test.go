package legacyauth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestLiveAuthRejectsUnknownAccount(t *testing.T) {
	address := os.Getenv("REDSCARF_LIVE_AUTH_ADDR")
	if address == "" {
		t.Skip("set REDSCARF_LIVE_AUTH_ADDR to exercise a running AzerothCore authserver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Login(ctx, address, Credentials{
		Username: "RS_NO_SUCH_USER",
		Password: "invalid",
		Locale:   "zhCN",
	})
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Code != ResultUnknownAccount {
		t.Fatalf("got %v, want unknown account", err)
	}
}
