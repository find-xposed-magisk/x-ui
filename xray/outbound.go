package xray

import (
	"bytes"

	"github.com/alireza0/x-ui/util/json_util"
)

type OutboundConfig struct {
	SendThrough    json_util.RawMessage `json:"sendThrough,omitempty"`
	Protocol       string               `json:"protocol"`
	Settings       json_util.RawMessage `json:"settings"`
	Tag            string               `json:"tag"`
	StreamSettings json_util.RawMessage `json:"streamSettings,omitempty"`
	ProxySettings  json_util.RawMessage `json:"proxySettings,omitempty"`
	Mux            json_util.RawMessage `json:"mux,omitempty"`
	TargetStrategy string               `json:"targetStrategy,omitempty"`
}

func (c *OutboundConfig) Equals(other *OutboundConfig) bool {
	if !bytes.Equal(c.SendThrough, other.SendThrough) {
		return false
	}
	if c.Protocol != other.Protocol {
		return false
	}
	if !bytes.Equal(c.Settings, other.Settings) {
		return false
	}
	if c.Tag != other.Tag {
		return false
	}
	if !bytes.Equal(c.StreamSettings, other.StreamSettings) {
		return false
	}
	if !bytes.Equal(c.ProxySettings, other.ProxySettings) {
		return false
	}
	if !bytes.Equal(c.Mux, other.Mux) {
		return false
	}
	if c.TargetStrategy != other.TargetStrategy {
		return false
	}
	return true
}
