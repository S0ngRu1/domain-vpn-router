package rules

import (
	"net"
	"strings"

	"domain-vpn-router/internal/config"
)

type Action string

const (
	ActionDirect  Action = "direct"
	ActionForeign Action = "foreign"
	ActionCompany Action = "company"
)

type Match struct {
	Action Action
	Rule   string
}

func IsLocalDirect(match Match) bool {
	return match.Action == ActionDirect && (match.Rule == "empty-host" || match.Rule == "private-ip")
}

type Matcher struct {
	company []string
	foreign []string
	direct  []string
}

func NewMatcher(cfg config.RulesConfig) *Matcher {
	return &Matcher{
		company: normalizeRules(cfg.CompanyDomains),
		foreign: normalizeRules(cfg.ForeignDomains),
		direct:  normalizeRules(cfg.DirectDomains),
	}
}

func (m *Matcher) Match(hostport string) Match {
	host := normalizeHost(hostport)
	if host == "" {
		return Match{Action: ActionDirect, Rule: "empty-host"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() {
			return Match{Action: ActionDirect, Rule: "private-ip"}
		}
	}
	if rule := firstDomainMatch(host, m.direct); rule != "" {
		return Match{Action: ActionDirect, Rule: rule}
	}
	if rule := firstDomainMatch(host, m.company); rule != "" {
		return Match{Action: ActionCompany, Rule: rule}
	}
	if rule := firstDomainMatch(host, m.foreign); rule != "" {
		return Match{Action: ActionForeign, Rule: rule}
	}
	return Match{Action: ActionDirect, Rule: "default"}
}

func normalizeRules(in []string) []string {
	out := make([]string, 0, len(in))
	for _, rule := range in {
		rule = strings.TrimSpace(strings.ToLower(rule))
		rule = strings.TrimSuffix(rule, ".")
		if rule != "" {
			out = append(out, rule)
		}
	}
	return out
}

func normalizeHost(hostport string) string {
	hostport = strings.TrimSpace(strings.ToLower(hostport))
	if hostport == "" {
		return ""
	}
	if strings.Contains(hostport, "://") {
		if idx := strings.Index(hostport, "://"); idx >= 0 {
			hostport = hostport[idx+3:]
		}
		if idx := strings.IndexByte(hostport, '/'); idx >= 0 {
			hostport = hostport[:idx]
		}
	}
	host := hostport
	if strings.HasPrefix(hostport, "[") {
		if h, _, err := net.SplitHostPort(hostport); err == nil {
			host = h
		}
	} else if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	host = strings.TrimSuffix(host, ".")
	return host
}

func firstDomainMatch(host string, rules []string) string {
	for _, rule := range rules {
		if matchDomain(host, rule) {
			return rule
		}
	}
	return ""
}

func matchDomain(host, rule string) bool {
	switch {
	case rule == host:
		return true
	case strings.HasPrefix(rule, "*."):
		suffix := strings.TrimPrefix(rule, "*.")
		return host == suffix || strings.HasSuffix(host, "."+suffix)
	default:
		return strings.HasSuffix(host, "."+rule)
	}
}
