package xray

import (
	"bytes"

	"github.com/alireza0/x-ui/util/json_util"
)

type APIConfig struct {
	Listen   string   `json:"listen"`
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

func (c *APIConfig) Equals(other *APIConfig) bool {
	if c.Listen != other.Listen {
		return false
	}
	if c.Tag != other.Tag {
		return false
	}
	if len(c.Services) != len(other.Services) {
		return false
	}
	for i, service := range c.Services {
		if service != other.Services[i] {
			return false
		}
	}
	return true
}

type Config struct {
	API              APIConfig            `json:"api"`
	LogConfig        json_util.RawMessage `json:"log"`
	RouterConfig     json_util.RawMessage `json:"routing"`
	DNSConfig        json_util.RawMessage `json:"dns"`
	InboundConfigs   []InboundConfig      `json:"inbounds"`
	OutboundConfigs  []OutboundConfig     `json:"outbounds"`
	Transport        json_util.RawMessage `json:"transport"`
	Policy           json_util.RawMessage `json:"policy"`
	Stats            json_util.RawMessage `json:"stats"`
	FakeDNS          json_util.RawMessage `json:"fakedns"`
	Observatory      json_util.RawMessage `json:"observatory"`
	BurstObservatory json_util.RawMessage `json:"burstObservatory"`
	Metrics          json_util.RawMessage `json:"metrics,omitempty"`
	GeoData          json_util.RawMessage `json:"geodata,omitempty"`
}

func (c *Config) Equals(other *Config) bool {
	if len(c.InboundConfigs) != len(other.InboundConfigs) {
		return false
	}
	for i, inbound := range c.InboundConfigs {
		if !inbound.Equals(&other.InboundConfigs[i]) {
			return false
		}
	}
	if !bytes.Equal(c.LogConfig, other.LogConfig) {
		return false
	}
	if !bytes.Equal(c.RouterConfig, other.RouterConfig) {
		return false
	}
	if !bytes.Equal(c.DNSConfig, other.DNSConfig) {
		return false
	}
	if len(c.OutboundConfigs) != len(other.OutboundConfigs) {
		return false
	}
	for i, outbound := range c.OutboundConfigs {
		if !outbound.Equals(&other.OutboundConfigs[i]) {
			return false
		}
	}
	if !bytes.Equal(c.Transport, other.Transport) {
		return false
	}
	if !bytes.Equal(c.Policy, other.Policy) {
		return false
	}
	if !c.API.Equals(&other.API) {
		return false
	}
	if !bytes.Equal(c.Stats, other.Stats) {
		return false
	}
	if !bytes.Equal(c.FakeDNS, other.FakeDNS) {
		return false
	}
	if !bytes.Equal(c.Observatory, other.Observatory) {
		return false
	}
	if !bytes.Equal(c.BurstObservatory, other.BurstObservatory) {
		return false
	}
	if !bytes.Equal(c.Metrics, other.Metrics) {
		return false
	}
	if !bytes.Equal(c.GeoData, other.GeoData) {
		return false
	}
	return true
}
