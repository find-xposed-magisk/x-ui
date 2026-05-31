package model

import (
	"encoding/json"
	"fmt"

	"github.com/alireza0/x-ui/util/json_util"
	"github.com/alireza0/x-ui/xray"
)

type Protocol string

const (
	VMess       Protocol = "vmess"
	VLESS       Protocol = "vless"
	Dokodemo    Protocol = "Dokodemo-door"
	Http        Protocol = "http"
	Trojan      Protocol = "trojan"
	Shadowsocks Protocol = "shadowsocks"
	Hysteria    Protocol = "hysteria"
)

type User struct {
	Id       int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Inbound struct {
	Id          int                  `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int                  `json:"-"`
	Up          int64                `json:"up" form:"up"`
	Down        int64                `json:"down" form:"down"`
	Total       int64                `json:"total" form:"total"`
	Remark      string               `json:"remark" form:"remark"`
	Enable      bool                 `json:"enable" form:"enable"`
	ExpiryTime  int64                `json:"expiryTime" form:"expiryTime"`
	ClientStats []xray.ClientTraffic `gorm:"foreignKey:InboundId;references:Id" json:"clientStats" form:"clientStats"`

	// config part
	Listen         string   `json:"listen" form:"listen"`
	Port           int      `json:"port" form:"port"`
	Protocol       Protocol `json:"protocol" form:"protocol"`
	Settings       string   `json:"settings" form:"settings"`
	StreamSettings string   `json:"streamSettings" form:"streamSettings"`
	Tag            string   `json:"tag" form:"tag" gorm:"unique"`
	Sniffing       string   `json:"sniffing" form:"sniffing"`
}

type Outbound struct {
	Id             int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Up             int64  `json:"up" form:"up"`
	Down           int64  `json:"down" form:"down"`
	Sort           int    `json:"sort" form:"sort"`
	SendThrough    string `json:"sendThrough" form:"sendThrough"`
	Protocol       string `json:"protocol" form:"protocol"`
	Settings       string `json:"settings" form:"settings"`
	Tag            string `json:"tag" form:"tag" gorm:"unique"`
	StreamSettings string `json:"streamSettings" form:"streamSettings"`
	ProxySettings  string `json:"proxySettings" form:"proxySettings"`
	Mux            string `json:"mux" form:"mux"`
	TargetStrategy string `json:"targetStrategy" form:"targetStrategy"`
}

type RoutingRule struct {
	Id      int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Tag     string `json:"tag" form:"tag" gorm:"unique"`
	Sort    int    `json:"sort" form:"sort"`
	RawJson string `json:"rawJson" form:"rawJson"`
}

func (r *RoutingRule) RuleJSON() ([]byte, error) {
	var raw map[string]interface{}
	if len(r.RawJson) > 0 {
		if err := json.Unmarshal([]byte(r.RawJson), &raw); err != nil {
			return nil, err
		}
	} else {
		raw = map[string]interface{}{"type": "field"}
	}
	if r.Tag != "" {
		raw["ruleTag"] = r.Tag
	}
	return json.Marshal(raw)
}

func (o *Outbound) GenXrayOutboundConfig() *xray.OutboundConfig {
	cfg := &xray.OutboundConfig{
		Protocol:       o.Protocol,
		Tag:            o.Tag,
		TargetStrategy: o.TargetStrategy,
	}
	if o.SendThrough != "" {
		cfg.SendThrough = json_util.RawMessage(fmt.Sprintf("\"%s\"", o.SendThrough))
	}
	if len(o.Settings) > 0 {
		cfg.Settings = json_util.RawMessage(o.Settings)
	} else {
		cfg.Settings = json_util.RawMessage("{}")
	}
	if len(o.StreamSettings) > 0 {
		cfg.StreamSettings = json_util.RawMessage(o.StreamSettings)
	}
	if len(o.ProxySettings) > 0 {
		cfg.ProxySettings = json_util.RawMessage(o.ProxySettings)
	}
	if len(o.Mux) > 0 {
		cfg.Mux = json_util.RawMessage(o.Mux)
	}
	return cfg
}

func (i *Inbound) GenXrayInboundConfig() *xray.InboundConfig {
	listen := i.Listen
	if listen != "" {
		listen = fmt.Sprintf("\"%v\"", listen)
	}
	return &xray.InboundConfig{
		Listen:         json_util.RawMessage(listen),
		Port:           i.Port,
		Protocol:       string(i.Protocol),
		Settings:       json_util.RawMessage(i.Settings),
		StreamSettings: json_util.RawMessage(i.StreamSettings),
		Tag:            i.Tag,
		Sniffing:       json_util.RawMessage(i.Sniffing),
	}
}

type Setting struct {
	Id    int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Key   string `json:"key" form:"key"`
	Value string `json:"value" form:"value"`
}

type ClientReverse struct {
	Tag      string               `json:"tag"`
	Sniffing json_util.RawMessage `json:"sniffing,omitempty"`
}
type Client struct {
	ID         string         `json:"id,omitempty"`
	Password   string         `json:"password,omitempty"`
	Auth       string         `json:"auth,omitempty"`
	Flow       string         `json:"flow,omitempty"`
	Reverse    *ClientReverse `json:"reverse,omitempty"`
	Email      string         `json:"email"`
	TotalGB    int64          `json:"totalGB" form:"totalGB"`
	ExpiryTime int64          `json:"expiryTime" form:"expiryTime"`
	Enable     bool           `json:"enable" form:"enable"`
	TgID       string         `json:"tgId" form:"tgId"`
	SubID      string         `json:"subId" form:"subId"`
	Reset      int            `json:"reset" form:"reset"`
}

type VLESSSettings struct {
	Clients    []Client `json:"clients"`
	Decryption string   `json:"decryption"`
	Encryption string   `json:"encryption"`
	Fallbacks  []any    `json:"fallbacks"`
}
