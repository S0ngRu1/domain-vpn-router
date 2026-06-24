package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	App   AppConfig
	Proxy ProxyConfig
	VPN   VPNConfig
	Rules RulesConfig
}

type AppConfig struct {
	StartMode         string
	MinimizeToTray    bool
	ShowWindowOnStart bool
	LogFile           string
}

type ProxyConfig struct {
	Listen       string
	DirectBindIP string
	ForeignProxy string
}

type VPNConfig struct {
	Tyty          VPNEndpoint
	GlobalProtect VPNEndpoint
}

type VPNEndpoint struct {
	Exe             string
	Process         string
	StopCommand     string
	AdapterKeywords []string
}

type RulesConfig struct {
	CompanyDomains []string
	ForeignDomains []string
	DirectDomains  []string
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var cfg Config
	var section, subsection, list string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := scanner.Text()
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "\ufeff")

		if strings.HasPrefix(trimmed, "- ") {
			item := unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			switch list {
			case "vpn.tyty.adapter_keywords":
				cfg.VPN.Tyty.AdapterKeywords = append(cfg.VPN.Tyty.AdapterKeywords, item)
			case "vpn.globalprotect.adapter_keywords":
				cfg.VPN.GlobalProtect.AdapterKeywords = append(cfg.VPN.GlobalProtect.AdapterKeywords, item)
			case "rules.company_domains":
				cfg.Rules.CompanyDomains = append(cfg.Rules.CompanyDomains, item)
			case "rules.foreign_domains":
				cfg.Rules.ForeignDomains = append(cfg.Rules.ForeignDomains, item)
			case "rules.direct_domains":
				cfg.Rules.DirectDomains = append(cfg.Rules.DirectDomains, item)
			default:
				return Config{}, fmt.Errorf("未知列表项: %s", raw)
			}
			continue
		}

		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return Config{}, fmt.Errorf("非法配置行: %s", raw)
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))

		if value == "" {
			switch indent {
			case 0:
				section = key
				subsection = ""
				list = ""
			case 2:
				if section == "rules" {
					list = "rules." + key
				} else {
					subsection = key
					list = ""
				}
			case 4:
				list = section + "." + subsection + "." + key
			default:
				list = section + "." + key
			}
			continue
		}

		list = ""
		switch {
		case section == "app" && key == "start_mode":
			cfg.App.StartMode = value
		case section == "app" && key == "minimize_to_tray":
			cfg.App.MinimizeToTray = parseBool(value)
		case section == "app" && key == "show_window_on_start":
			cfg.App.ShowWindowOnStart = parseBool(value)
		case section == "app" && key == "log_file":
			cfg.App.LogFile = value
		case section == "proxy" && key == "listen":
			cfg.Proxy.Listen = value
		case section == "proxy" && key == "direct_bind_ip":
			cfg.Proxy.DirectBindIP = value
		case section == "proxy" && key == "foreign_proxy":
			cfg.Proxy.ForeignProxy = value
		case section == "vpn" && subsection == "tyty" && key == "exe":
			cfg.VPN.Tyty.Exe = value
		case section == "vpn" && subsection == "tyty" && key == "process":
			cfg.VPN.Tyty.Process = value
		case section == "vpn" && subsection == "tyty" && key == "stop_command":
			cfg.VPN.Tyty.StopCommand = value
		case section == "vpn" && subsection == "globalprotect" && key == "exe":
			cfg.VPN.GlobalProtect.Exe = value
		case section == "vpn" && subsection == "globalprotect" && key == "process":
			cfg.VPN.GlobalProtect.Process = value
		case section == "vpn" && subsection == "globalprotect" && key == "stop_command":
			cfg.VPN.GlobalProtect.StopCommand = value
		default:
			return Config{}, fmt.Errorf("未知配置项: %s", raw)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	if cfg.Proxy.Listen == "" {
		return Config{}, fmt.Errorf("缺少 proxy.listen")
	}
	if cfg.App.StartMode == "" {
		cfg.App.StartMode = "auto"
	}
	cfg.App.MinimizeToTray = true
	if cfg.VPN.Tyty.Exe == "" {
		return Config{}, fmt.Errorf("缺少 vpn.tyty.exe")
	}
	if cfg.VPN.GlobalProtect.Exe == "" {
		return Config{}, fmt.Errorf("缺少 vpn.globalprotect.exe")
	}
	if cfg.VPN.Tyty.Process == "" {
		cfg.VPN.Tyty.Process = "Tyty"
	}
	if cfg.VPN.GlobalProtect.Process == "" {
		cfg.VPN.GlobalProtect.Process = "PanGPA"
	}
	if len(cfg.VPN.Tyty.AdapterKeywords) == 0 {
		cfg.VPN.Tyty.AdapterKeywords = []string{"Mihomo", "Meta Tunnel"}
	}
	if len(cfg.VPN.GlobalProtect.AdapterKeywords) == 0 {
		cfg.VPN.GlobalProtect.AdapterKeywords = []string{"PANGP", "GlobalProtect"}
	}
	return cfg, nil
}

func UpdateProxyDirectBindIP(path, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inProxy := false
	updated := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(stripComment(line))
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			inProxy = strings.TrimSuffix(trimmed, ":") == "proxy"
			continue
		}
		if inProxy && indent == 2 && strings.HasPrefix(trimmed, "direct_bind_ip:") {
			lines[i] = "  direct_bind_ip: " + value
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("未找到 proxy.direct_bind_ip")
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func stripComment(line string) string {
	inQuote := false
	var quote rune
	for i, r := range line {
		if (r == '"' || r == '\'') && (i == 0 || line[i-1] != '\\') {
			if !inQuote {
				inQuote = true
				quote = r
			} else if quote == r {
				inQuote = false
			}
		}
		if r == '#' && !inQuote {
			return line[:i]
		}
	}
	return line
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
