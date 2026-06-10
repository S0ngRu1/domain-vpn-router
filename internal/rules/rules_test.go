package rules

import (
	"testing"

	"domain-vpn-router/internal/config"
)

func TestMatcherDomainRules(t *testing.T) {
	m := NewMatcher(config.RulesConfig{
		CompanyDomains: []string{"*.corp.example.com"},
		ForeignDomains: []string{"github.com", "*.google.com"},
		DirectDomains:  []string{"localhost", "*.local"},
	})

	tests := []struct {
		host string
		want Action
	}{
		{"api.corp.example.com:443", ActionCompany},
		{"github.com", ActionForeign},
		{"www.github.com", ActionForeign},
		{"mail.google.com:443", ActionForeign},
		{"LOCALHOST:8080", ActionDirect},
		{"printer.local", ActionDirect},
		{"192.168.1.1", ActionDirect},
		{"example.cn", ActionDirect},
	}

	for _, tt := range tests {
		if got := m.Match(tt.host); got.Action != tt.want {
			t.Fatalf("Match(%q) = %s, want %s, rule %s", tt.host, got.Action, tt.want, got.Rule)
		}
	}
}

func TestDirectRulesHaveHighestPriority(t *testing.T) {
	m := NewMatcher(config.RulesConfig{
		CompanyDomains: []string{"example.com"},
		ForeignDomains: []string{"example.com"},
		DirectDomains:  []string{"api.example.com"},
	})
	if got := m.Match("api.example.com"); got.Action != ActionDirect {
		t.Fatalf("direct should win, got %s", got.Action)
	}
}

func TestOnlyLocalDirectIsProtectedFromForcedModes(t *testing.T) {
	if IsLocalDirect(Match{Action: ActionDirect, Rule: "default"}) {
		t.Fatal("default direct should be overridable by forced modes")
	}
	if !IsLocalDirect(Match{Action: ActionDirect, Rule: "private-ip"}) {
		t.Fatal("private IP direct should stay local in forced modes")
	}
}
