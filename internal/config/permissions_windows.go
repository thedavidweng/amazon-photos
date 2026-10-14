//go:build windows

package config

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

func enforceConfigDirPermissions(dir string) error {
	_ = os.Chmod(dir, 0o700)
	return restrictWindowsACL(dir, "(OI)(CI)F")
}

func enforceConfigFilePermissions(path string) error {
	_ = os.Chmod(path, 0o600)
	return restrictWindowsACL(path, "F")
}

func restrictWindowsACL(target, grantPerm string) error {
	u, err := user.Current()
	if err != nil || strings.TrimSpace(u.Username) == "" {
		return nil
	}
	cmd := exec.Command("icacls", target, "/inheritance:r", "/grant:r", fmt.Sprintf("%s:%s", u.Username, grantPerm))
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("icacls %s: %w (%s)", target, runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func CheckConfigPermissions(cfgPath string) (ok bool, msg, hint string) {
	if _, err := os.Stat(cfgPath); err != nil {
		return false, err.Error(), ""
	}
	out, err := exec.Command("icacls", cfgPath).CombinedOutput()
	if err != nil {
		return true, fmt.Sprintf("config file %s readable", cfgPath), ""
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, "everyone:") || strings.Contains(lower, `builtin\users:`) {
		uName := "%USERNAME%"
		if u, uErr := user.Current(); uErr == nil && strings.TrimSpace(u.Username) != "" {
			uName = u.Username
		}
		return false,
			fmt.Sprintf("config file %s ACL allows broad multi-user access", cfgPath),
			fmt.Sprintf(`run: icacls "%s" /inheritance:r /grant:r "%s:F"`, cfgPath, uName)
	}
	return true, fmt.Sprintf("config file %s readable (user ACL verified)", cfgPath), ""
}
