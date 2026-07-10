package spotify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	widevine "github.com/iyear/gowidevine"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

const maxWVDUploadBytes = 2 << 20

type widevineDeviceUpdater interface {
	SetWidevineDevice(*widevine.Device) error
}

// ImportCredentialFile validates, atomically persists and activates an
// administrator-supplied Spotify Widevine device. The file is never returned
// by an API and is written with owner-only permissions.
func (p *SpotifyPlatform) ImportCredentialFile(_ context.Context, req platform.CredentialFileImportRequest) (platform.CredentialFileImportResult, error) {
	if p == nil {
		return platform.CredentialFileImportResult{}, fmt.Errorf("Spotify 插件未初始化")
	}
	if !strings.EqualFold(strings.TrimSpace(req.Kind), "widevine") {
		return platform.CredentialFileImportResult{}, fmt.Errorf("不支持的 Spotify 凭据类型")
	}
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(req.FileName))); ext != ".wvd" {
		return platform.CredentialFileImportResult{}, fmt.Errorf("请选择 .wvd 设备文件")
	}
	if len(req.Data) == 0 || len(req.Data) > maxWVDUploadBytes {
		return platform.CredentialFileImportResult{}, fmt.Errorf("WVD 文件大小必须在 1 字节到 2 MiB 之间")
	}
	destination := strings.TrimSpace(req.Destination)
	if destination == "" {
		return platform.CredentialFileImportResult{}, fmt.Errorf("WVD 保存路径未配置")
	}
	device, err := native.LoadWVDevice(bytes.NewReader(req.Data), filepath.Base(req.FileName))
	if err != nil {
		return platform.CredentialFileImportResult{}, fmt.Errorf("WVD 文件无效: %w", err)
	}
	updater, ok := p.native.(widevineDeviceUpdater)
	if !ok || updater == nil {
		return platform.CredentialFileImportResult{}, fmt.Errorf("Spotify Widevine 下载源未启用")
	}

	oldData, oldErr := os.ReadFile(destination)
	hadOld := oldErr == nil
	if oldErr != nil && !os.IsNotExist(oldErr) {
		return platform.CredentialFileImportResult{}, fmt.Errorf("读取旧 WVD 失败: %w", oldErr)
	}
	if err := atomicWriteCredential(destination, req.Data); err != nil {
		return platform.CredentialFileImportResult{}, fmt.Errorf("保存 WVD 失败: %w", err)
	}
	rollback := func() {
		if hadOld {
			_ = atomicWriteCredential(destination, oldData)
		} else {
			_ = os.Remove(destination)
		}
	}
	if p.persistFunc != nil {
		if err := p.persistFunc(map[string]string{"wvd_path": destination}); err != nil {
			rollback()
			return platform.CredentialFileImportResult{}, fmt.Errorf("写入 Spotify 配置失败: %w", err)
		}
	}
	if err := updater.SetWidevineDevice(device); err != nil {
		rollback()
		return platform.CredentialFileImportResult{}, fmt.Errorf("启用 WVD 失败: %w", err)
	}
	return platform.CredentialFileImportResult{
		Updated: true,
		Message: "Spotify WVD 已验证、保存并立即生效",
		Path:    destination,
	}, nil
}

func atomicWriteCredential(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".spotify-wvd-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

var _ platform.CredentialFileImporter = (*SpotifyPlatform)(nil)
