package iptables

import (
	"errors"
	"testing"
)

// fakeLookPath returns a LookPath-like function backed by a set of available names.
func fakeLookPath(available []string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, a := range available {
			if a == name {
				return "/fake/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestResolveBinFallback_KeepWhenPresent(t *testing.T) {
	fc := &FirewallConfiguration{BinPath: "iptables-nft", SaveBinPath: "iptables-nft-save"}
	lp := fakeLookPath([]string{
		"iptables-nft",
		"iptables-nft-save",
	})

	resolveBinFallback(fc, lp)

	if fc.BinPath != "iptables-nft" || fc.SaveBinPath != "iptables-nft-save" {
		t.Fatalf("expected to keep configured binaries, got bin=%q save=%q", fc.BinPath, fc.SaveBinPath)
	}
}

func TestResolveBinFallback_FallbackToNFT_IPv4(t *testing.T) {
	fc := &FirewallConfiguration{BinPath: "iptables-notreal", SaveBinPath: "iptables-notreal-save"}
	lp := fakeLookPath([]string{
		// Only nft pair is available
		"iptables-nft",
		"iptables-nft-save",
	})

	resolveBinFallback(fc, lp)

	if fc.BinPath != "iptables-nft" || fc.SaveBinPath != "iptables-nft-save" {
		t.Fatalf("expected fallback to iptables-nft, got bin=%q save=%q", fc.BinPath, fc.SaveBinPath)
	}
}

func TestResolveBinFallback_FallbackOrder_Plain(t *testing.T) {
	fc := &FirewallConfiguration{BinPath: "iptables-missing", SaveBinPath: "iptables-missing-save"}
	lp := fakeLookPath([]string{
		// Only plain iptables present
		"iptables",
		"iptables-save",
	})

	resolveBinFallback(fc, lp)

	if fc.BinPath != "iptables" || fc.SaveBinPath != "iptables-save" {
		t.Fatalf("expected fallback to iptables/iptable-save, got bin=%q save=%q", fc.BinPath, fc.SaveBinPath)
	}
}

func TestResolveBinFallback_IPv6_FallbackLegacy(t *testing.T) {
	fc := &FirewallConfiguration{BinPath: "ip6tables-missing", SaveBinPath: "ip6tables-missing-save"}
	lp := fakeLookPath([]string{
		// Only legacy pair present for IPv6
		"ip6tables-legacy",
		"ip6tables-legacy-save",
	})

	resolveBinFallback(fc, lp)

	if fc.BinPath != "ip6tables-legacy" || fc.SaveBinPath != "ip6tables-legacy-save" {
		t.Fatalf("expected fallback to ip6tables-legacy, got bin=%q save=%q", fc.BinPath, fc.SaveBinPath)
	}
}

func TestResolveBinFallback_NoCandidates(t *testing.T) {
	origBin, origSave := "iptables-missing", "iptables-missing-save"
	fc := &FirewallConfiguration{BinPath: origBin, SaveBinPath: origSave}
	lp := fakeLookPath([]string{})

	resolveBinFallback(fc, lp)

	if fc.BinPath != origBin || fc.SaveBinPath != origSave {
		t.Fatalf("expected no change when no candidates found, got bin=%q save=%q", fc.BinPath, fc.SaveBinPath)
	}
}
