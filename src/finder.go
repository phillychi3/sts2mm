package sts2mm

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func FindGameDir() string {
	switch runtime.GOOS {
	case "windows":
		return findGameDirWindows()
	case "darwin":
		return findGameDirDarwin()
	default:
		return findGameDirLinux()
	}
}

func findGameDirDarwin() string {
	home := os.Getenv("HOME")
	steamPath := filepath.Join(home, "Library", "Application Support", "Steam")

	candidate := filepath.Join(steamPath, "steamapps", "common", STS2DirName)
	if isValidGameDir(candidate) {
		return candidate
	}

	vdf := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
	for _, libPath := range parseLibraryFolders(vdf) {
		candidate := filepath.Join(libPath, "steamapps", "common", STS2DirName)
		if isValidGameDir(candidate) {
			return candidate
		}
	}

	return ""
}

func findGameDirLinux() string {

	home := os.Getenv("HOME")
	steamPaths := []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
	}

	for _, steamPath := range steamPaths {
		candidate := filepath.Join(steamPath, "steamapps", "common", STS2DirName)
		if isValidGameDir(candidate) {
			return candidate
		}

		vdf := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
		if dirs := parseLibraryFolders(vdf); len(dirs) > 0 {
			for _, libPath := range dirs {
				candidate := filepath.Join(libPath, "steamapps", "common", STS2DirName)
				if isValidGameDir(candidate) {
					return candidate
				}
			}
		}
	}

	return ""
}

func findGameDirWindows() string {
	steamRoots := []string{
		`C:\Program Files (x86)\Steam`,
		`C:\Program Files\Steam`,
		`D:\Steam`,
		`E:\Steam`,
	}

	for _, steamPath := range steamRoots {
		candidate := filepath.Join(steamPath, "steamapps", "common", STS2DirName)
		if isValidGameDir(candidate) {
			return candidate
		}

		vdf := filepath.Join(steamPath, "steamapps", "libraryfolders.vdf")
		for _, libPath := range parseLibraryFolders(vdf) {
			candidate := filepath.Join(libPath, "steamapps", "common", STS2DirName)
			if isValidGameDir(candidate) {
				return candidate
			}
		}
	}

	return bruteForceSearch()
}

func parseLibraryFolders(vdfPath string) []string {
	file, err := os.Open(vdfPath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var paths []string
	scanner := bufio.NewScanner(file)
	re := regexp.MustCompile(`"path"\s+"([^"]+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			path := strings.ReplaceAll(matches[1], `\\`, `\`)
			paths = append(paths, path)
		}
	}

	return paths
}

func bruteForceSearch() string {
	drives := []string{"C:", "D:", "E:", "F:", "G:"}
	subPaths := []string{
		`SteamLibrary\steamapps\common\` + STS2DirName,
		`Steam\steamapps\common\` + STS2DirName,
		`Program Files (x86)\Steam\steamapps\common\` + STS2DirName,
		`Program Files\Steam\steamapps\common\` + STS2DirName,
		`Games\Steam\steamapps\common\` + STS2DirName,
	}

	for _, drive := range drives {
		for _, sub := range subPaths {
			candidate := filepath.Join(drive, sub)
			if isValidGameDir(candidate) {
				return candidate
			}
		}
	}

	return ""
}

func isValidGameDir(path string) bool {
	exe := STS2Exe
	if runtime.GOOS == "darwin" {
		exe = STS2APP
	}
	_, err := os.Stat(filepath.Join(path, exe))
	return err == nil
}
