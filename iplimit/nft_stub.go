//go:build !linux

package iplimit

type stubFirewall struct{}

func newPlatformFirewall() Firewall {
	return &stubFirewall{}
}

func (stubFirewall) Block(key BlockKey) error {
	return nil
}

func (stubFirewall) Init() error {
	return nil
}

func (stubFirewall) Stop() error {
	return nil
}

func (stubFirewall) Supported() bool {
	return false
}
