package systemproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"

	"domain-vpn-router/internal/hiddenexec"
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
	if _, err := os.Stat(statePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := saveState(statePath, state); err != nil {
			return err
		}
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
	_ = os.Remove(statePath)
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
	out, err := hiddenexec.Command("reg", "query", internetSettingsKey).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("读取系统代理注册表失败: %w: %s", err, cleanCommandOutput(out))
	}
	values := map[string]string{}
	for _, line := range strings.Split(cleanCommandOutput(out), "\n") {
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
	out, err := hiddenexec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_DWORD", "/d", value, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w: %s", name, err, cleanCommandOutput(out))
	}
	return nil
}

func regAddSZ(name, value string) error {
	out, err := hiddenexec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_SZ", "/d", value, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w: %s", name, err, cleanCommandOutput(out))
	}
	return nil
}

func regDelete(name string) error {
	out, err := hiddenexec.Command("reg", "delete", internetSettingsKey, "/v", name, "/f").CombinedOutput()
	if err != nil {
		text := cleanCommandOutput(out)
		if isMissingRegistryValue(text) {
			return nil
		}
		return fmt.Errorf("删除 %s 失败: %w: %s", name, err, text)
	}
	return nil
}

func isMissingRegistryValue(text string) bool {
	text = strings.ToLower(text)
	markers := []string{
		"找不到",
		"指定的注册表项或值",
		"unable to find",
		"cannot find",
		"not found",
		"does not exist",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func cleanCommandOutput(out []byte) string {
	text := decodeCommandOutput(out)
	return strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
}

func decodeCommandOutput(out []byte) string {
	if len(out) >= 2 {
		if out[0] == 0xff && out[1] == 0xfe {
			return utf16BytesToString(out[2:], false)
		}
		if out[0] == 0xfe && out[1] == 0xff {
			return utf16BytesToString(out[2:], true)
		}
	}
	zeroCount := 0
	for _, b := range out {
		if b == 0 {
			zeroCount++
		}
	}
	if len(out) > 0 && zeroCount*4 > len(out) {
		return utf16BytesToString(out, false)
	}
	return string(out)
}

func utf16BytesToString(out []byte, bigEndian bool) string {
	if len(out)%2 == 1 {
		out = out[:len(out)-1]
	}
	words := make([]uint16, 0, len(out)/2)
	for i := 0; i < len(out); i += 2 {
		if bigEndian {
			words = append(words, uint16(out[i])<<8|uint16(out[i+1]))
		} else {
			words = append(words, uint16(out[i])|uint16(out[i+1])<<8)
		}
	}
	return string(utf16.Decode(words))
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
