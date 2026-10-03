//go:build windows

package sts2mm

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const SteamworksSupported = true

// EItemState
const (
	itemStateInstalled       = 4
	itemStateNeedsUpdate     = 8
	itemStateDownloading     = 16
	itemStateDownloadPending = 32
)

const (
	steamworksStallTimeout = 2 * time.Minute
	steamworksQueueTimeout = 10 * time.Second
)

func findSteamAPIDLL(gameDir string) string {
	p := filepath.Join(gameDir, "data_sts2_windows_x86_64", "steam_api64.dll")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if matches, _ := filepath.Glob(filepath.Join(gameDir, "*", "steam_api64.dll")); len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func boolRet(r uintptr) bool { return r&0xff != 0 }

// DownloadViaSteamworks 透過遊戲附帶的 steam_api64.dll 請正在執行的 Steam 客戶端下載工作坊項目。
func DownloadViaSteamworks(gameDir string, items []WorkshopItem, progress func(WorkshopProgress)) (map[string]string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dllPath := findSteamAPIDLL(gameDir)
	if dllPath == "" {
		return nil, fmt.Errorf("找不到遊戲的 steam_api64.dll")
	}

	os.Setenv("SteamAppId", STS2AppID)
	os.Setenv("SteamGameId", STS2AppID)

	dll := syscall.NewLazyDLL(dllPath)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("載入 steam_api64.dll 失敗: %w", err)
	}
	var (
		initFlat        = dll.NewProc("SteamAPI_InitFlat")
		shutdown        = dll.NewProc("SteamAPI_Shutdown")
		runCallbacks    = dll.NewProc("SteamAPI_RunCallbacks")
		steamUGC        = dll.NewProc("SteamAPI_SteamUGC_v020")
		downloadItem    = dll.NewProc("SteamAPI_ISteamUGC_DownloadItem")
		getItemState    = dll.NewProc("SteamAPI_ISteamUGC_GetItemState")
		getDownloadInfo = dll.NewProc("SteamAPI_ISteamUGC_GetItemDownloadInfo")
		getInstallInfo  = dll.NewProc("SteamAPI_ISteamUGC_GetItemInstallInfo")
	)
	for _, p := range []*syscall.LazyProc{initFlat, shutdown, runCallbacks, steamUGC, downloadItem, getItemState, getDownloadInfo, getInstallInfo} {
		if err := p.Find(); err != nil {
			return nil, fmt.Errorf("steam_api64.dll 版本不相容: %w", err)
		}
	}

	var errMsg [1024]byte
	if r, _, _ := initFlat.Call(uintptr(unsafe.Pointer(&errMsg[0]))); r != 0 {
		msg := string(errMsg[:clen(errMsg[:])])
		if msg == "" {
			msg = fmt.Sprintf("錯誤碼 %d", r)
		}
		return nil, fmt.Errorf("Steam 初始化失敗（請確認 Steam 已開啟並登入）: %s", msg)
	}
	defer shutdown.Call()

	ugc, _, _ := steamUGC.Call()
	if ugc == 0 {
		return nil, fmt.Errorf("無法取得 ISteamUGC")
	}

	state := func(id uint64) uint32 {
		r, _, _ := getItemState.Call(ugc, uintptr(id))
		return uint32(r)
	}

	paths := map[string]string{}
	for i, it := range items {
		p := WorkshopProgress{Index: i, Total: len(items), ID: it.ID}
		id, err := parseWorkshopID(it.ID)
		if err == nil && it.Missing {
			err = fmt.Errorf("Steam 上找不到此項目")
		}
		if err != nil {
			p.Err, p.Done = err, true
			progress(p)
			continue
		}

		if r, _, _ := downloadItem.Call(ugc, uintptr(id), 1); !boolRet(r) {
			p.Err, p.Done = fmt.Errorf("Steam 拒絕下載（ID 無效或未擁有遊戲）"), true
			progress(p)
			continue
		}

		start := time.Now()
		lastChange := start
		var lastDownloaded uint64
		queued := false
		for {
			runCallbacks.Call()
			s := state(id)
			busy := s&(itemStateNeedsUpdate|itemStateDownloading|itemStateDownloadPending) != 0
			if s&itemStateInstalled != 0 && !busy {
				break
			}
			if busy {
				queued = true
			} else if !queued && time.Since(start) > steamworksQueueTimeout {
				p.Err = fmt.Errorf("Steam 未開始下載（ID 無效或項目已移除？）")
				break
			}

			var downloaded, total uint64
			getDownloadInfo.Call(ugc, uintptr(id),
				uintptr(unsafe.Pointer(&downloaded)), uintptr(unsafe.Pointer(&total)))
			if downloaded != lastDownloaded {
				lastDownloaded = downloaded
				lastChange = time.Now()
			}
			p.Downloaded, p.Size = downloaded, total
			progress(p)

			if time.Since(lastChange) > steamworksStallTimeout {
				p.Err = fmt.Errorf("下載逾時")
				break
			}
			time.Sleep(300 * time.Millisecond)
		}

		if p.Err == nil {
			var size uint64
			var ts uint32
			var folder [1024]byte
			r, _, _ := getInstallInfo.Call(ugc, uintptr(id),
				uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&folder[0])),
				uintptr(len(folder)), uintptr(unsafe.Pointer(&ts)))
			if boolRet(r) {
				paths[it.ID] = string(folder[:clen(folder[:])])
				p.Downloaded, p.Size = size, size
			} else {
				p.Err = fmt.Errorf("取得安裝路徑失敗")
			}
		}
		p.Done = true
		progress(p)
	}
	return paths, nil
}

func clen(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}
