package service

import (
	"sort"
	"sync"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/iplimit"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/xray"
)

var (
	onlineUsersMu  sync.RWMutex
	onlineUsers    []xray.OnlineUserInfo
	ipLimitMu      sync.RWMutex
	ipLimitClients map[string]*IpLimitClientState
	blockedIPs     map[blockedKey]int64
	ipLimitFw      iplimit.Firewall
)

type blockedKey struct {
	IP   string
	Port uint16
}

type IpLimitClientState struct {
	IpLimit uint16
	Port    uint16
	IPs     []string
	Allowed map[string]struct{}
}

type IpLimitClientUpdate struct {
	Email         string
	LimitIP       uint16
	Port          uint16
	ClientEnable  bool
	StatEnable    bool
	InboundEnable bool
	ResetIPs      bool
}

func cloneOnlineUsers(users []xray.OnlineUserInfo) []xray.OnlineUserInfo {
	if users == nil {
		return nil
	}
	result := make([]xray.OnlineUserInfo, len(users))
	for i, u := range users {
		result[i].Email = u.Email
		result[i].IPs = make(map[string]int64, len(u.IPs))
		for ip, ts := range u.IPs {
			result[i].IPs[ip] = ts
		}
	}
	return result
}

func SetOnlineUsersCache(users []xray.OnlineUserInfo) {
	onlineUsersMu.Lock()
	onlineUsers = cloneOnlineUsers(users)
	onlineUsersMu.Unlock()
}

func GetOnlineUsersCache() []xray.OnlineUserInfo {
	onlineUsersMu.RLock()
	defer onlineUsersMu.RUnlock()
	return cloneOnlineUsers(onlineUsers)
}

func ClearOnlineUsersCache() {
	SetOnlineUsersCache(nil)
}

func InitOnlineStore(fw iplimit.Firewall) error {
	ipLimitFw = fw
	ipLimitClients = make(map[string]*IpLimitClientState)
	blockedIPs = make(map[blockedKey]int64)

	var inboundService InboundService
	if err := RefreshIpLimitClients(&inboundService); err != nil {
		return err
	}
	return nil
}

func RefreshIpLimitClients(inboundService *InboundService) error {
	inbounds, err := inboundService.GetAllInbounds()
	if err != nil {
		return err
	}

	newMap := make(map[string]*IpLimitClientState)
	for _, inbound := range inbounds {
		if !inbound.Enable {
			continue
		}
		clients, err := inboundService.GetClients(inbound)
		if err != nil {
			continue
		}
		for _, client := range clients {
			if client.LimitIP <= 0 || !client.Enable {
				continue
			}
			if !isClientStatEnabled(inbound, client.Email) {
				continue
			}
			existingIPs := []string{}
			ipLimitMu.RLock()
			if old, ok := ipLimitClients[client.Email]; ok {
				existingIPs = append([]string(nil), old.IPs...)
			}
			ipLimitMu.RUnlock()
			newMap[client.Email] = &IpLimitClientState{
				IpLimit: client.LimitIP,
				Port:    uint16(inbound.Port),
				IPs:     existingIPs,
			}
		}
	}

	ipLimitMu.Lock()
	ipLimitClients = newMap
	ipLimitMu.Unlock()
	return nil
}

func ipLimitUpdatesFromClients(inbound *model.Inbound, clients []model.Client) []IpLimitClientUpdate {
	updates := make([]IpLimitClientUpdate, 0, len(clients))
	for _, client := range clients {
		if client.Email == "" {
			continue
		}
		updates = append(updates, IpLimitClientUpdate{
			Email:         client.Email,
			LimitIP:       uint16(client.LimitIP),
			Port:          uint16(inbound.Port),
			ClientEnable:  client.Enable,
			StatEnable:    isClientStatEnabled(inbound, client.Email),
			InboundEnable: inbound.Enable,
		})
	}
	return updates
}

func ipLimitRemovedEmails(oldClients, newClients []model.Client) []string {
	newEmails := make(map[string]struct{}, len(newClients))
	for _, client := range newClients {
		if client.Email != "" {
			newEmails[client.Email] = struct{}{}
		}
	}
	removed := make([]string, 0)
	for _, client := range oldClients {
		if client.Email == "" {
			continue
		}
		if _, ok := newEmails[client.Email]; !ok {
			removed = append(removed, client.Email)
		}
	}
	return removed
}

func applyIpLimitMemoryChanges(updates []IpLimitClientUpdate, removeEmails []string) {
	ipLimitMu.Lock()
	defer ipLimitMu.Unlock()

	for _, email := range removeEmails {
		delete(ipLimitClients, email)
	}
	for _, update := range updates {
		if update.Email == "" {
			continue
		}
		if !update.InboundEnable || !update.ClientEnable || !update.StatEnable || update.LimitIP <= 0 {
			delete(ipLimitClients, update.Email)
			continue
		}
		existingIPs := []string{}
		if !update.ResetIPs {
			if old, ok := ipLimitClients[update.Email]; ok {
				existingIPs = append([]string(nil), old.IPs...)
			}
		}
		ipLimitClients[update.Email] = &IpLimitClientState{
			IpLimit: update.LimitIP,
			Port:    update.Port,
			IPs:     existingIPs,
		}
	}
}

func (s *InboundService) syncIpLimitStore(updates []IpLimitClientUpdate, removeEmails []string) {
	if !ipLimitFw.Supported() {
		return
	}
	applyIpLimitMemoryChanges(updates, removeEmails)
}

func blockIPsForPort(ips []string, port uint16) {
	if len(ips) == 0 || ipLimitFw == nil || !ipLimitFw.Supported() {
		return
	}
	for _, ip := range ips {
		if err := ipLimitFw.Block(iplimit.BlockKey{IP: ip, Port: port}); err != nil {
			logger.Debug("block ip failed:", err)
		}
	}
}

func isClientStatEnabled(inbound *model.Inbound, email string) bool {
	for _, stat := range inbound.ClientStats {
		if stat.Email == email {
			return stat.Enable
		}
	}
	return true
}

func sortIPsByLastSeen(ips map[string]int64) []string {
	type ipSeen struct {
		ip string
		ts int64
	}
	list := make([]ipSeen, 0, len(ips))
	for ip, ts := range ips {
		list = append(list, ipSeen{ip: ip, ts: ts})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].ts == list[j].ts {
			return list[i].ip < list[j].ip
		}
		return list[i].ts < list[j].ts
	})
	result := make([]string, len(list))
	for i, item := range list {
		result[i] = item.ip
	}
	return result
}

func updateIpLimitOnlineIPs(onlineUsers []xray.OnlineUserInfo) {
	onlineMap := make(map[string]xray.OnlineUserInfo, len(onlineUsers))
	for _, user := range onlineUsers {
		onlineMap[user.Email] = user
	}

	ipLimitMu.Lock()
	defer ipLimitMu.Unlock()

	deadline := time.Now().Add(iplimit.BlockDuration).Unix()

	for email, state := range ipLimitClients {
		if state.Allowed == nil {
			state.Allowed = make(map[string]struct{})
		}
		info, online := onlineMap[email]
		if !online {
			state.IPs = nil
			for ip := range state.Allowed {
				delete(state.Allowed, ip)
			}
			continue
		}

		for ip := range state.Allowed {
			if _, ok := info.IPs[ip]; !ok {
				delete(state.Allowed, ip)
			}
		}

		limit := int(state.IpLimit)
		ordered := sortIPsByLastSeen(info.IPs)
		state.IPs = ordered
		for _, ip := range ordered {
			if _, ok := state.Allowed[ip]; ok {
				continue // already within the safe set
			}
			if len(state.Allowed) < limit {
				state.Allowed[ip] = struct{}{} // reserve a safe slot
				continue
			}
			// Beyond the limit: block this IP on the client's inbound port.
			key := blockedKey{IP: ip, Port: state.Port}
			if _, exists := blockedIPs[key]; !exists {
				logger.Debug("blocked ip: ", ip, " for user:", email)
			}
			blockedIPs[key] = deadline
		}
	}
}

func ProcessIpLimitCron(onlineUsers []xray.OnlineUserInfo) {
	if !ipLimitFw.Supported() {
		return
	}
	updateIpLimitOnlineIPs(onlineUsers)
	reapplyBlocks()
}

func GetBlockedList() []xray.OnlineUserInfo {
	ipLimitMu.RLock()
	defer ipLimitMu.RUnlock()
	if len(blockedIPs) == 0 {
		return nil
	}
	ips := make(map[string]int64, len(blockedIPs))
	for key, deadline := range blockedIPs {
		ips[key.IP] = deadline
	}
	return []xray.OnlineUserInfo{{IPs: ips}}
}

func reapplyBlocks() {
	ipLimitMu.Lock()
	defer ipLimitMu.Unlock()
	now := time.Now().Unix()
	for key, deadline := range blockedIPs {
		if deadline <= now {
			delete(blockedIPs, key)
			continue
		}
		if err := ipLimitFw.Block(iplimit.BlockKey{IP: key.IP, Port: key.Port}); err != nil {
			logger.Debug("block ip limit failed:", err)
		}
	}
}
