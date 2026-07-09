package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/app"
	"github.com/liuran001/MusicBot-Go/bot/musicservice"
	_ "github.com/liuran001/MusicBot-Go/plugins/all"
	webserver "github.com/liuran001/MusicBot-Go/web/server"
)

func main() {
	configPath := flag.String("c", "config.ini", "配置文件")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	core, err := app.NewCore(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create core: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = core.Shutdown(shutdownCtx)
	}()

	music := musicservice.New(core)
	errCh := make(chan error, 1)
	go func() {
		errCh <- webserver.ListenAndServe(core, music)
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Web server stopped: %v\n", err)
			os.Exit(1)
		}
	}
}
