//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func enforceConfigDirPermissions(dir string) error {
	return os.Chmod(dir, 0o700)
}

func enforceConfigFilePermissions(path string) error {
	return os.Chmod(path, 0o600)
}

func CheckConfigPermissions(cfgPath string) (ok bool, msg, hint string) {
	info, err := os.Stat(cfgPath)
	if err != nil {
		return false, err.Error(), ""
	}
	filePerm := info.Mode().Perm()
	dirPath := filepath.Dir(cfgPath)
	dirInfo, dErr := os.Stat(dirPath)
	if filePerm&0o077 != 0 {
		return false, fmt.Sprintf("config file %s has overly permissive mode %04o", cfgPath, filePerm), fmt.Sprintf("run: chmod 0600 %s", cfgPath)
	}
	if dErr == nil && dirInfo.Mode().Perm()&0o077 != 0 {
		return false, fmt.Sprintf("config directory %s has overly permissive mode %04o", dirPath, dirInfo.Mode().Perm()), fmt.Sprintf("run: chmod 0700 %s", dirPath)
	}
	return true, fmt.Sprintf("config file %s readable", cfgPath), ""
}
