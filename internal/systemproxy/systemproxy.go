package systemproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

type State struct {
	ProxyEnable   *string `json:"proxy_enable,omitempty"`
	ProxyServer   *string `json:"proxy_server,omitempty"`
	AutoConfigURL *string `json:"auto_config_url,omitempty"`
}

const internetSettingsKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func Enable(listen, statePath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("系统代理设置目前只支持 Windows")
	}
	state, err := readCurrent()
	if err != nil {
		return err
	}
	if err := saveState(statePath, state); err != nil {
		return err
	}
	if err := regAddDWORD("ProxyEnable", "1"); err != nil {
		return err
	}
	if err := regAddSZ("ProxyServer", listen); err != nil {
		return err
	}
	if err := notifyWindows(); err != nil {
		return err
	}
	return nil
}

func Restore(statePath string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	state, err := loadState(statePath)
	if err != nil {
		return err
	}
	if err := restoreValueDWORD("ProxyEnable", state.ProxyEnable); err != nil {
		return err
	}
	if err := restoreValueSZ("ProxyServer", state.ProxyServer); err != nil {
		return err
	}
	if err := restoreValueSZ("AutoConfigURL", state.AutoConfigURL); err != nil {
		return err
	}
	if err := notifyWindows(); err != nil {
		return err
	}
	return nil
}

func readCurrent() (State, error) {
	var state State
	values, err := regQuery()
	if err != nil {
		return state, err
	}
	state.ProxyEnable = optional(values, "ProxyEnable")
	state.ProxyServer = optional(values, "ProxyServer")
	state.AutoConfigURL = optional(values, "AutoConfigURL")
	return state, nil
}

func regQuery() (map[string]string, error) {
	out, err := exec.Command("reg", "query", internetSettingsKey).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("读取系统代理注册表失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			values[fields[0]] = strings.Join(fields[2:], " ")
		}
	}
	return values, nil
}

func optional(values map[string]string, key string) *string {
	if v, ok := values[key]; ok {
		return &v
	}
	return nil
}

func saveState(path string, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("未找到代理备份文件 %s", path)
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func restoreValueDWORD(name string, value *string) error {
	if value == nil {
		return regDelete(name)
	}
	return regAddDWORD(name, strings.TrimPrefix(*value, "0x"))
}

func restoreValueSZ(name string, value *string) error {
	if value == nil {
		return regDelete(name)
	}
	return regAddSZ(name, *value)
}

func regAddDWORD(name, value string) error {
	out, err := exec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_DWORD", "/d", value, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func regAddSZ(name, value string) error {
	out, err := exec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_SZ", "/d", value, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func regDelete(name string) error {
	out, err := exec.Command("reg", "delete", internetSettingsKey, "/v", name, "/f").CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "找不到") || strings.Contains(strings.ToLower(text), "unable to find") {
			return nil
		}
		return fmt.Errorf("删除 %s 失败: %w: %s", name, err, text)
	}
	return nil
}

func notifyWindows() error {
	wininet := syscall.NewLazyDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	if r, _, err := internetSetOption.Call(0, internetOptionSettingsChanged, 0, 0); r == 0 {
		return err
	}
	if r, _, err := internetSetOption.Call(0, internetOptionRefresh, 0, 0); r == 0 {
		return err
	}
	return nil
}
