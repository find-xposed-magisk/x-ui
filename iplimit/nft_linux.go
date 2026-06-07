//go:build linux

package iplimit

import (
	"encoding/binary"
	"net"
	"sync"

	"github.com/alireza0/x-ui/util/common"
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	filterTableName = "xui"
	inputChainName  = "iplimit_input"
	nftSetNameV4    = "xui_blocked"
	nftSetNameV6    = "xui_blocked_ip6"
)

// Concatenated set keys are laid out in consecutive 4-byte registers, each field
// padded up to a multiple of the register size (see nftables ConcatSetType).
const (
	concatKeyLenV4 = 8  // ipv4_addr (4) + inet_service (2) + pad (2)
	concatKeyLenV6 = 20 // ipv6_addr (16) + inet_service (2) + pad (2)
)

func concatBlockKeyV4(ip net.IP, port uint16) []byte {
	key := make([]byte, concatKeyLenV4)
	copy(key, ip.To4())
	binary.BigEndian.PutUint16(key[4:6], port)
	return key
}

func concatBlockKeyV6(ip net.IP, port uint16) []byte {
	key := make([]byte, concatKeyLenV6)
	copy(key, ip.To16())
	binary.BigEndian.PutUint16(key[16:18], port)
	return key
}

type nftFirewall struct {
	mu    sync.Mutex
	conn  *nftables.Conn
	table *nftables.Table
	chain *nftables.Chain
	setV4 *nftables.Set
	setV6 *nftables.Set
	ready bool
}

func newPlatformFirewall() Firewall {
	return &nftFirewall{}
}

func (f *nftFirewall) Supported() bool {
	return true
}

// blockRuleExprs builds "<family> saddr . <l4> dport @set drop". The source
// address occupies the lookup base register (NFT_REG32_00); the destination port
// goes into the next free 4-byte register after the address (one slot for IPv4,
// four slots for IPv6), so the registers form the contiguous concat key.
func blockRuleExprs(set *nftables.Set, ipv4 bool, tcp bool) []expr.Any {
	nfproto := byte(unix.NFPROTO_IPV6)
	srcOffset := uint32(8)
	srcLen := uint32(16)
	dportReg := uint32(unix.NFT_REG32_04)
	if ipv4 {
		nfproto = byte(unix.NFPROTO_IPV4)
		srcOffset = 12
		srcLen = 4
		dportReg = uint32(unix.NFT_REG32_01)
	}
	l4proto := byte(unix.IPPROTO_UDP)
	if tcp {
		l4proto = byte(unix.IPPROTO_TCP)
	}

	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{nfproto}},
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{l4proto}},
		&expr.Payload{
			DestRegister: unix.NFT_REG32_00,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       srcOffset,
			Len:          srcLen,
		},
		&expr.Payload{
			DestRegister: dportReg,
			Base:         expr.PayloadBaseTransportHeader,
			Offset:       2,
			Len:          2,
		},
		&expr.Lookup{
			SourceRegister: unix.NFT_REG32_00,
			SetName:        set.Name,
			SetID:          set.ID,
		},
		&expr.Verdict{Kind: expr.VerdictDrop},
	}
}

func (f *nftFirewall) addBlockRules() {
	for _, rule := range []struct {
		set *nftables.Set
		v4  bool
		tcp bool
	}{
		{f.setV4, true, true},
		{f.setV4, true, false},
		{f.setV6, false, true},
		{f.setV6, false, false},
	} {
		f.conn.AddRule(&nftables.Rule{
			Table: f.table,
			Chain: f.chain,
			Exprs: blockRuleExprs(rule.set, rule.v4, rule.tcp),
		})
	}
}

func (f *nftFirewall) Init() error {
	conn, err := nftables.New()
	if err != nil {
		return err
	}
	f.conn = conn
	f.table = &nftables.Table{
		Name:   filterTableName,
		Family: nftables.TableFamilyINet,
	}

	// Remove leftovers from a previous run or older versions without Stop().
	f.conn.DelTable(f.table)
	_ = f.conn.Flush()

	policy := nftables.ChainPolicyAccept
	f.chain = &nftables.Chain{
		Name:     inputChainName,
		Table:    f.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policy,
	}
	f.setV4 = &nftables.Set{
		Name:          nftSetNameV4,
		Table:         f.table,
		KeyType:       nftables.MustConcatSetType(nftables.TypeIPAddr, nftables.TypeInetService),
		Concatenation: true,
		HasTimeout:    true,
		Timeout:       BlockDuration,
	}
	f.setV6 = &nftables.Set{
		Name:          nftSetNameV6,
		Table:         f.table,
		KeyType:       nftables.MustConcatSetType(nftables.TypeIP6Addr, nftables.TypeInetService),
		Concatenation: true,
		HasTimeout:    true,
		Timeout:       BlockDuration,
	}

	f.conn.AddTable(f.table)
	f.conn.AddChain(f.chain)
	f.conn.AddSet(f.setV4, nil)
	f.conn.AddSet(f.setV6, nil)
	f.addBlockRules()

	if err := f.conn.Flush(); err != nil {
		return err
	}

	f.ready = true
	return nil
}

func (f *nftFirewall) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ready || f.conn == nil || f.table == nil {
		return nil
	}
	f.conn.DelTable(f.table)
	err := f.conn.Flush()
	f.ready = false
	f.conn = nil
	return err
}

func (f *nftFirewall) Block(key BlockKey) error {
	if !f.ready {
		return common.NewError("nftables not ready")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	ipStr := key.IP
	if n := len(ipStr); n >= 2 && ipStr[0] == '[' && ipStr[n-1] == ']' {
		ipStr = ipStr[1 : n-1]
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return common.NewErrorf("invalid ip: %s", key.IP)
	}
	if v4 := ip.To4(); v4 != nil {
		if err := f.conn.SetAddElements(f.setV4, []nftables.SetElement{
			{Key: concatBlockKeyV4(v4, key.Port)},
		}); err != nil {
			return err
		}
	} else if v6 := ip.To16(); v6 != nil {
		if err := f.conn.SetAddElements(f.setV6, []nftables.SetElement{
			{Key: concatBlockKeyV6(v6, key.Port)},
		}); err != nil {
			return err
		}
	}
	if err := f.conn.Flush(); err != nil {
		return err
	}
	return nil
}
