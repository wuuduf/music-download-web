package app

import (
	"context"
	"fmt"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/dynplugin"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/recognize"
	"github.com/liuran001/MusicBot-Go/bot/worker"
)

// Core contains the transport-agnostic application dependencies. It is used by
// the web entrypoint and mirrors the non-Telegram part of App.
type Core struct {
	Config                   *config.Config
	ConfigPath               string
	Logger                   *logpkg.Logger
	DB                       *db.Repository
	Pool                     *worker.Pool
	DownloadPool             *worker.Pool
	PlatformManager          platform.Manager
	DynPlugins               *dynplugin.Manager
	AdminIDs                 map[int64]struct{}
	AdminCommands            []admincmd.Command
	RecognizeService         recognize.Service
	TagProviders             map[string]id3.ID3TagProvider
	PluginSettingDefinitions []botpkg.PluginSettingDefinition
}

// NewCore builds dependencies shared by non-Telegram transports. Unlike New,
// it loads config in web mode and therefore does not require BOT_TOKEN.
func NewCore(ctx context.Context, configPath string) (*Core, error) {
	conf, log, err := loadWebConfigAndLogger(configPath)
	if err != nil {
		return nil, err
	}

	repo, err := initRepository(conf, log)
	if err != nil {
		_ = log.Close()
		return nil, err
	}

	pool := initWorkerPool(conf, log)
	platformManager, dynManager, adminIDs, pluginTagProviders, adminCommands, pluginSettingDefinitions, recognizeService := initPluginRuntime(ctx, conf, log)

	return &Core{
		Config:                   conf,
		ConfigPath:               configPath,
		Logger:                   log,
		DB:                       repo,
		Pool:                     pool,
		PlatformManager:          platformManager,
		DynPlugins:               dynManager,
		AdminIDs:                 adminIDs,
		AdminCommands:            adminCommands,
		RecognizeService:         recognizeService,
		TagProviders:             pluginTagProviders,
		PluginSettingDefinitions: pluginSettingDefinitions,
	}, nil
}

func loadWebConfigAndLogger(configPath string) (*config.Config, *logpkg.Logger, error) {
	conf, err := config.LoadWeb(configPath)
	if err != nil {
		return nil, nil, err
	}
	log, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), conf.GetBool("LogSource"))
	if err != nil {
		return nil, nil, err
	}
	return conf, log, nil
}

// Shutdown releases Core resources.
func (c *Core) Shutdown(ctx context.Context) error {
	var firstErr error
	if c == nil {
		return nil
	}
	if c.RecognizeService != nil {
		if err := c.RecognizeService.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop recognition service: %w", err)
		}
	}
	if c.Pool != nil {
		if err := c.Pool.Shutdown(ctx); err != nil {
			c.Pool.StopNow()
			if firstErr == nil {
				firstErr = fmt.Errorf("shutdown worker pool: %w", err)
			}
		}
	}
	if c.DownloadPool != nil && c.DownloadPool != c.Pool {
		if err := c.DownloadPool.Shutdown(ctx); err != nil {
			c.DownloadPool.StopNow()
			if firstErr == nil {
				firstErr = fmt.Errorf("shutdown download worker pool: %w", err)
			}
		}
	}
	if dm, ok := c.PlatformManager.(*platform.DefaultManager); ok {
		_ = dm.Close()
	}
	if c.DB != nil {
		if err := c.DB.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close database: %w", err)
		}
	}
	if c.Logger != nil {
		if err := c.Logger.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close logger: %w", err)
		}
	}
	return firstErr
}
