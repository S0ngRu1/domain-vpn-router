package proxy

import (
	"net"
	"testing"

	"domain-vpn-router/internal/rules"
)

func TestEffectiveDialActionFallsBackToDirectWhenCompanyIPMissing(t *testing.T) {
	if got := effectiveDialAction(rules.ActionCompany, nil); got != rules.ActionDirect {
		t.Fatalf("effectiveDialAction(company, nil) = %s, want direct", got)
	}
}

func TestEffectiveDialActionKeepsCompanyWhenCompanyIPExists(t *testing.T) {
	ip := net.ParseIP("10.10.10.10")
	if got := effectiveDialAction(rules.ActionCompany, ip); got != rules.ActionCompany {
		t.Fatalf("effectiveDialAction(company, ip) = %s, want company", got)
	}
}
