package service

import (
	"sync"
	"testing"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/iplimit"
	"github.com/alireza0/x-ui/xray"
)

// fakeFirewall records what the store asks it to block instead of touching nftables.
type fakeFirewall struct {
	mu        sync.Mutex
	supported bool
	blocked   []iplimit.BlockKey
}

func (f *fakeFirewall) Supported() bool { return f.supported }
func (f *fakeFirewall) Init() error     { return nil }
func (f *fakeFirewall) Stop() error     { return nil }

func (f *fakeFirewall) Block(key iplimit.BlockKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked = append(f.blocked, key)
	return nil
}

func (f *fakeFirewall) calls() []iplimit.BlockKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]iplimit.BlockKey(nil), f.blocked...)
}

// resetStore puts the package-level store back to a known state. The store is a
// singleton, so every test has to start from a clean one.
func resetStore(t *testing.T, fw iplimit.Firewall, blockAfterRemove bool) {
	t.Helper()
	ipLimitMu.Lock()
	ipLimitClients = make(map[string]*IpLimitClientState)
	blockedIPs = make(map[blockedKey]int64)
	ipLimitMu.Unlock()
	ipLimitFw = fw
	ipBlockAfterRemove = blockAfterRemove
	t.Cleanup(func() {
		ipLimitMu.Lock()
		ipLimitClients = make(map[string]*IpLimitClientState)
		blockedIPs = make(map[blockedKey]int64)
		ipLimitMu.Unlock()
		ipLimitFw = nil
		ipBlockAfterRemove = false
	})
}

func onlineUser(email string, ips map[string]int64) xray.OnlineUserInfo {
	return xray.OnlineUserInfo{Email: email, IPs: ips}
}

func allowedSet(t *testing.T, email string) map[string]struct{} {
	t.Helper()
	ipLimitMu.RLock()
	defer ipLimitMu.RUnlock()
	state, ok := ipLimitClients[email]
	if !ok {
		t.Fatalf("no state for %q", email)
	}
	copied := make(map[string]struct{}, len(state.Allowed))
	for ip := range state.Allowed {
		copied[ip] = struct{}{}
	}
	return copied
}

func blockedIPSet(t *testing.T) map[string]struct{} {
	t.Helper()
	ipLimitMu.RLock()
	defer ipLimitMu.RUnlock()
	set := make(map[string]struct{}, len(blockedIPs))
	for key := range blockedIPs {
		set[key.IP] = struct{}{}
	}
	return set
}

// A nil firewall is the state before the web server initialises the store, and
// used to be a nil-interface panic on every inbound edit.
func TestIpLimitEnabledIsNilSafe(t *testing.T) {
	resetStore(t, nil, false)
	if ipLimitEnabled() {
		t.Fatal("ipLimitEnabled() = true with no firewall installed")
	}

	var service InboundService
	service.syncIpLimitStore([]IpLimitClientUpdate{{Email: "a@b.c", LimitIP: 1}}, nil)
	ProcessIpLimitCron([]xray.OnlineUserInfo{onlineUser("a@b.c", map[string]int64{"1.1.1.1": 1})})
	blockIPsForPort([]string{"1.1.1.1"}, 443)
}

func TestIpLimitEnabledUnsupportedPlatform(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: false}, true)
	if ipLimitEnabled() {
		t.Fatal("ipLimitEnabled() = true on an unsupported platform")
	}

	var service InboundService
	service.syncIpLimitStore([]IpLimitClientUpdate{{Email: "a@b.c", LimitIP: 1, ClientEnable: true, StatEnable: true, InboundEnable: true}}, nil)
	ipLimitMu.RLock()
	tracked := len(ipLimitClients)
	ipLimitMu.RUnlock()
	if tracked != 0 {
		t.Fatalf("tracked %d clients, want 0 when the firewall is unsupported", tracked)
	}
}

func TestCloneOnlineUsersIsADeepCopy(t *testing.T) {
	source := []xray.OnlineUserInfo{onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10})}
	clone := cloneOnlineUsers(source)
	clone[0].Email = "changed"
	clone[0].IPs["1.1.1.1"] = 99
	clone[0].IPs["2.2.2.2"] = 5

	if source[0].Email != "a@b.c" {
		t.Fatalf("source email changed to %q", source[0].Email)
	}
	if source[0].IPs["1.1.1.1"] != 10 || len(source[0].IPs) != 1 {
		t.Fatalf("source IPs mutated: %v", source[0].IPs)
	}
	if cloneOnlineUsers(nil) != nil {
		t.Fatal("cloneOnlineUsers(nil) should stay nil")
	}
}

func TestOnlineUsersCacheRoundTrip(t *testing.T) {
	SetOnlineUsersCache([]xray.OnlineUserInfo{onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10})})
	got := GetOnlineUsersCache()
	if len(got) != 1 || got[0].Email != "a@b.c" {
		t.Fatalf("cache = %v, want one entry for a@b.c", got)
	}

	got[0].IPs["1.1.1.1"] = 99
	if again := GetOnlineUsersCache(); again[0].IPs["1.1.1.1"] != 10 {
		t.Fatal("the cache handed out a reference callers can mutate")
	}

	ClearOnlineUsersCache()
	if len(GetOnlineUsersCache()) != 0 {
		t.Fatal("cache was not cleared")
	}
}

func TestSortIPsByLastSeen(t *testing.T) {
	got := sortIPsByLastSeen(map[string]int64{
		"3.3.3.3": 30,
		"1.1.1.1": 10,
		"2.2.2.2": 20,
		"0.0.0.0": 10, // ties break on the address, so the order is stable
	})
	want := []string{"0.0.0.0", "1.1.1.1", "2.2.2.2", "3.3.3.3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestUpdateIpLimitOnlineIPsWithinLimit(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 2, Port: 443}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10, "2.2.2.2": 20}),
	})

	if len(blockedIPSet(t)) != 0 {
		t.Fatalf("blocked %v while inside the limit", blockedIPSet(t))
	}
	if len(allowedSet(t, "a@b.c")) != 2 {
		t.Fatalf("safe set = %v, want both IPs", allowedSet(t, "a@b.c"))
	}
}

func TestUpdateIpLimitOnlineIPsBlocksBeyondLimit(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 2, Port: 443}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10, "2.2.2.2": 20, "3.3.3.3": 30}),
	})

	allowed := allowedSet(t, "a@b.c")
	if len(allowed) != 2 {
		t.Fatalf("safe set = %v, want 2 entries", allowed)
	}
	if _, ok := allowed["3.3.3.3"]; ok {
		t.Fatal("the most recently seen IP took a slot instead of being blocked")
	}
	blocked := blockedIPSet(t)
	if _, ok := blocked["3.3.3.3"]; !ok || len(blocked) != 1 {
		t.Fatalf("blocked = %v, want only 3.3.3.3", blocked)
	}
	ipLimitMu.RLock()
	port := blockedIPs[blockedKey{IP: "3.3.3.3", Port: 443}]
	ipLimitMu.RUnlock()
	if port == 0 {
		t.Fatal("the block was not recorded against the client's inbound port")
	}
}

// Once an IP holds a slot it must keep it, otherwise a client's established
// connection is dropped the moment a newer IP shows up.
func TestUpdateIpLimitOnlineIPsKeepsAdmittedIPs(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 1, Port: 443}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10}),
	})
	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 5, "2.2.2.2": 1}),
	})

	allowed := allowedSet(t, "a@b.c")
	if _, ok := allowed["1.1.1.1"]; !ok {
		t.Fatalf("safe set = %v, want the already-admitted 1.1.1.1 to keep its slot", allowed)
	}
	if _, ok := blockedIPSet(t)["2.2.2.2"]; !ok {
		t.Fatalf("blocked = %v, want 2.2.2.2", blockedIPSet(t))
	}
}

func TestUpdateIpLimitOnlineIPsReleasesGoneIPs(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 1, Port: 443}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10}),
	})
	// 1.1.1.1 disconnected; its slot must go to the newcomer rather than stay reserved.
	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"2.2.2.2": 20}),
	})

	allowed := allowedSet(t, "a@b.c")
	if _, ok := allowed["2.2.2.2"]; !ok || len(allowed) != 1 {
		t.Fatalf("safe set = %v, want only 2.2.2.2", allowed)
	}
	if _, ok := blockedIPSet(t)["2.2.2.2"]; ok {
		t.Fatal("2.2.2.2 was blocked even though a slot was free")
	}
}

func TestUpdateIpLimitOnlineIPsClearsOfflineClients(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{
		IpLimit: 2,
		Port:    443,
		IPs:     []string{"1.1.1.1"},
		Allowed: map[string]struct{}{"1.1.1.1": {}},
	}

	updateIpLimitOnlineIPs(nil)

	ipLimitMu.RLock()
	state := ipLimitClients["a@b.c"]
	ips, allowed := state.IPs, len(state.Allowed)
	ipLimitMu.RUnlock()
	if ips != nil || allowed != 0 {
		t.Fatalf("offline client kept IPs=%v allowed=%d", ips, allowed)
	}
}

// Lowering a client's IP limit has to shrink a safe set that was carried over
// from before the edit, keeping the connections seen longest ago.
func TestUpdateIpLimitOnlineIPsTrimsAfterLimitLowered(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{
		IpLimit: 1,
		Port:    443,
		Allowed: map[string]struct{}{"1.1.1.1": {}, "2.2.2.2": {}, "3.3.3.3": {}},
	}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10, "2.2.2.2": 20, "3.3.3.3": 30}),
	})

	allowed := allowedSet(t, "a@b.c")
	if len(allowed) != 1 {
		t.Fatalf("safe set = %v, want 1 entry after the limit was lowered", allowed)
	}
	if _, ok := allowed["1.1.1.1"]; !ok {
		t.Fatalf("safe set = %v, want the longest-seen 1.1.1.1 to keep the slot", allowed)
	}
	blocked := blockedIPSet(t)
	if _, ok := blocked["2.2.2.2"]; !ok {
		t.Fatalf("blocked = %v, want 2.2.2.2 released and blocked", blocked)
	}
	if _, ok := blocked["3.3.3.3"]; !ok {
		t.Fatalf("blocked = %v, want 3.3.3.3 released and blocked", blocked)
	}
}

// An inbound edit must not hand a blocked IP a fresh slot.
func TestApplyIpLimitMemoryChangesCarriesSafeSet(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{
		IpLimit: 1,
		Port:    443,
		IPs:     []string{"1.1.1.1", "2.2.2.2"},
		Allowed: map[string]struct{}{"1.1.1.1": {}},
	}

	applyIpLimitMemoryChanges([]IpLimitClientUpdate{{
		Email: "a@b.c", LimitIP: 1, Port: 443,
		ClientEnable: true, StatEnable: true, InboundEnable: true,
	}}, nil)

	allowed := allowedSet(t, "a@b.c")
	if _, ok := allowed["1.1.1.1"]; !ok || len(allowed) != 1 {
		t.Fatalf("safe set = %v, want it carried over as {1.1.1.1}", allowed)
	}

	updateIpLimitOnlineIPs([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 30, "2.2.2.2": 10}),
	})
	if _, ok := blockedIPSet(t)["2.2.2.2"]; !ok {
		t.Fatalf("blocked = %v, want 2.2.2.2 to stay blocked across the edit", blockedIPSet(t))
	}
}

func TestApplyIpLimitMemoryChangesResetIPs(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{
		IpLimit: 2, Port: 443,
		IPs:     []string{"1.1.1.1"},
		Allowed: map[string]struct{}{"1.1.1.1": {}},
	}

	applyIpLimitMemoryChanges([]IpLimitClientUpdate{{
		Email: "a@b.c", LimitIP: 2, Port: 443,
		ClientEnable: true, StatEnable: true, InboundEnable: true, ResetIPs: true,
	}}, nil)

	ipLimitMu.RLock()
	state := ipLimitClients["a@b.c"]
	ips, allowed := len(state.IPs), len(state.Allowed)
	ipLimitMu.RUnlock()
	if ips != 0 || allowed != 0 {
		t.Fatalf("reset left IPs=%d allowed=%d, want both empty", ips, allowed)
	}
}

func TestApplyIpLimitMemoryChangesDropsUntrackedClients(t *testing.T) {
	base := IpLimitClientUpdate{
		Email: "a@b.c", LimitIP: 2, Port: 443,
		ClientEnable: true, StatEnable: true, InboundEnable: true,
	}
	tests := []struct {
		name   string
		mutate func(u *IpLimitClientUpdate)
	}{
		{"inbound disabled", func(u *IpLimitClientUpdate) { u.InboundEnable = false }},
		{"client disabled", func(u *IpLimitClientUpdate) { u.ClientEnable = false }},
		{"stats disabled", func(u *IpLimitClientUpdate) { u.StatEnable = false }},
		{"no limit", func(u *IpLimitClientUpdate) { u.LimitIP = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetStore(t, &fakeFirewall{supported: true}, false)
			ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 2, Port: 443}

			update := base
			test.mutate(&update)
			applyIpLimitMemoryChanges([]IpLimitClientUpdate{update}, nil)

			ipLimitMu.RLock()
			_, tracked := ipLimitClients["a@b.c"]
			ipLimitMu.RUnlock()
			if tracked {
				t.Fatal("client is still tracked")
			}
		})
	}
}

func TestApplyIpLimitMemoryChangesRemovesEmails(t *testing.T) {
	resetStore(t, &fakeFirewall{supported: true}, false)
	ipLimitClients["gone@b.c"] = &IpLimitClientState{IpLimit: 2, Port: 443}
	ipLimitClients["stays@b.c"] = &IpLimitClientState{IpLimit: 2, Port: 443}

	applyIpLimitMemoryChanges(nil, []string{"gone@b.c"})

	ipLimitMu.RLock()
	_, gone := ipLimitClients["gone@b.c"]
	_, stays := ipLimitClients["stays@b.c"]
	ipLimitMu.RUnlock()
	if gone {
		t.Fatal("removed client is still tracked")
	}
	if !stays {
		t.Fatal("an unrelated client was removed")
	}
}

func TestProcessIpLimitCronReappliesAndExpiresBlocks(t *testing.T) {
	fw := &fakeFirewall{supported: true}
	resetStore(t, fw, false)
	ipLimitClients["a@b.c"] = &IpLimitClientState{IpLimit: 1, Port: 443}
	// An entry whose block window already closed must be forgotten, not re-pushed.
	blockedIPs[blockedKey{IP: "9.9.9.9", Port: 443}] = time.Now().Add(-time.Minute).Unix()

	ProcessIpLimitCron([]xray.OnlineUserInfo{
		onlineUser("a@b.c", map[string]int64{"1.1.1.1": 10, "2.2.2.2": 20}),
	})

	calls := fw.calls()
	if len(calls) != 1 || calls[0] != (iplimit.BlockKey{IP: "2.2.2.2", Port: 443}) {
		t.Fatalf("firewall calls = %v, want a single block of 2.2.2.2:443", calls)
	}
	if _, ok := blockedIPSet(t)["9.9.9.9"]; ok {
		t.Fatal("an expired block was kept")
	}
}

func TestIsClientStatEnabled(t *testing.T) {
	inbound := &model.Inbound{ClientStats: []xray.ClientTraffic{
		{Email: "on@b.c", Enable: true},
		{Email: "off@b.c", Enable: false},
	}}
	if !isClientStatEnabled(inbound, "on@b.c") {
		t.Fatal("enabled stat read as disabled")
	}
	if isClientStatEnabled(inbound, "off@b.c") {
		t.Fatal("disabled stat read as enabled")
	}
	// A client with no stat row yet has not been disabled.
	if !isClientStatEnabled(inbound, "new@b.c") {
		t.Fatal("a client without a stat row should count as enabled")
	}
}

func TestIpLimitUpdatesFromClients(t *testing.T) {
	inbound := &model.Inbound{
		Port:        443,
		Enable:      true,
		ClientStats: []xray.ClientTraffic{{Email: "off@b.c", Enable: false}},
	}
	updates := ipLimitUpdatesFromClients(inbound, []model.Client{
		{Email: "on@b.c", LimitIP: 3, Enable: true},
		{Email: "off@b.c", LimitIP: 3, Enable: true},
		{Email: "", LimitIP: 3, Enable: true}, // no email means no traffic stats to key on
	})

	if len(updates) != 2 {
		t.Fatalf("got %d updates, want 2 (the unnamed client is skipped)", len(updates))
	}
	if updates[0].Port != 443 || updates[0].LimitIP != 3 || !updates[0].StatEnable || !updates[0].InboundEnable {
		t.Fatalf("update for on@b.c = %+v", updates[0])
	}
	if updates[1].StatEnable {
		t.Fatalf("update for off@b.c should carry StatEnable=false, got %+v", updates[1])
	}
}

func TestIpLimitRemovedEmails(t *testing.T) {
	removed := ipLimitRemovedEmails(
		[]model.Client{{Email: "keep@b.c"}, {Email: "drop@b.c"}, {Email: ""}},
		[]model.Client{{Email: "keep@b.c"}, {Email: "new@b.c"}},
	)
	if len(removed) != 1 || removed[0] != "drop@b.c" {
		t.Fatalf("removed = %v, want [drop@b.c]", removed)
	}
}
