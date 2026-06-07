package iplimit

import "time"

const BlockDuration = 60 * time.Second

type BlockKey struct {
	IP   string
	Port uint16
}

type Firewall interface {
	Supported() bool
	Init() (err error)
	Stop() (err error)
	Block(key BlockKey) (err error)
}

func NewFirewall() Firewall {
	return newPlatformFirewall()
}
