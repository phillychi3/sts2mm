//go:build !windows

package sts2mm

import "fmt"

const SteamworksSupported = false

func DownloadViaSteamworks(gameDir string, items []WorkshopItem, progress func(WorkshopProgress)) (map[string]string, error) {
	return nil, fmt.Errorf("Steamworks 下載目前只支援 Windows，請改用 SteamCMD")
}
