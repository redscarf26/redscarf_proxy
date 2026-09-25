package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"redscarf/internal/proxy"
)

func main() {
	config := proxy.DefaultConfig()
	flag.Var((*uint64Flag)(&config.ClientBuild), "client-build", "supported modern client build")
	flag.StringVar(&config.BNetAddress, "bnet", config.BNetAddress, "BNet TLS listen address")
	flag.StringVar(&config.RESTAddress, "rest", config.RESTAddress, "login REST TLS listen address")
	flag.StringVar(&config.WorldAddress, "world", config.WorldAddress, "modern world listen address")
	flag.StringVar(&config.LegacyAuth, "legacy-auth", config.LegacyAuth, "AzerothCore authserver address")
	flag.StringVar(&config.CUFDataDir, "cuf-data-dir", config.CUFDataDir, "persistent raid frame profile directory")
	flag.StringVar(&config.TLSCertFile, "tls-cert", "", "Arctium-compatible PEM certificate chain")
	flag.StringVar(&config.TLSKeyFile, "tls-key", "", "PEM private key")
	flag.Parse()

	writers := []io.Writer{os.Stdout}
	logFile, err := os.OpenFile("proxy.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open proxy.log: %v\n", err)
	} else {
		defer logFile.Close()
		writers = append(writers, logFile)
	}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: slog.LevelDebug}))
	server, err := proxy.New(config, logger)
	if err != nil {
		logger.Error("proxy configuration failed", "error", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("redscarf Go proxy starting", "client", "3.4.3.54261", "legacy", config.LegacyAuth, "spell_queue", "compat")
	if err := server.Run(ctx); err != nil {
		logger.Error("proxy stopped", "error", err)
		os.Exit(1)
	}
}

// flag.Value does not expose a uint32 target. This adapter validates overflow
// before assigning the exact client build field.
type uint64Flag uint32

func (u *uint64Flag) String() string { return fmt.Sprint(uint32(*u)) }

func (u *uint64Flag) Set(value string) error {
	var parsed uint64
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		return err
	}
	if parsed > uint64(^uint32(0)) {
		return fmt.Errorf("build number out of range")
	}
	*u = uint64Flag(parsed)
	return nil
}
