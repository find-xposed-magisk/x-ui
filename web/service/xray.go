package service

import (
	"encoding/json"
	"errors"
	"runtime"
	"sync"

	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/json_util"
	"github.com/alireza0/x-ui/xray"

	"go.uber.org/atomic"
)

var (
	p                 *xray.Process
	lock              sync.Mutex
	isNeedXrayRestart atomic.Bool
	result            string
)

type XrayService struct {
	inboundService     InboundService
	outboundService    OutboundService
	routingRuleService RoutingRuleService
	settingService     SettingService
	xrayAPI            xray.XrayAPI
}

func (s *XrayService) IsXrayRunning() bool {
	return p != nil && p.IsRunning()
}

func (s *XrayService) GetXrayErr() error {
	if p == nil {
		return nil
	}

	err := p.GetErr()
	if err == nil {
		return nil
	}

	if runtime.GOOS == "windows" && err.Error() == "exit status 1" {
		// exit status 1 on Windows means that Xray process was killed
		// as we kill process to stop in on Windows, this is not an error
		return nil
	}

	return err
}

func (s *XrayService) GetXrayResult() string {
	if result != "" {
		return result
	}
	if s.IsXrayRunning() {
		return ""
	}
	if p == nil {
		return ""
	}

	result = p.GetResult()

	if runtime.GOOS == "windows" && result == "exit status 1" {
		// exit status 1 on Windows means that Xray process was killed
		// as we kill process to stop in on Windows, this is not an error
		return ""
	}

	return result
}

func (s *XrayService) GetXrayVersion() string {
	if p == nil {
		return "Unknown"
	}
	return p.GetVersion()
}

func RemoveIndex(s []interface{}, index int) []interface{} {
	return append(s[:index], s[index+1:]...)
}

func (s *XrayService) GetXrayConfig() (*xray.Config, error) {
	templateConfig, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}

	xrayConfig := &xray.Config{}
	err = json.Unmarshal([]byte(templateConfig), xrayConfig)
	if err != nil {
		return nil, err
	}

	s.inboundService.AddTraffic(nil, nil)

	inbounds, err := s.inboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if !inbound.Enable {
			continue
		}
		// get settings clients
		settings := map[string]interface{}{}
		json.Unmarshal([]byte(inbound.Settings), &settings)
		clients, ok := settings["clients"].([]interface{})
		if ok {
			// check users active or not
			clientStats := inbound.ClientStats
			for _, clientTraffic := range clientStats {
				indexDecrease := 0
				for index, client := range clients {
					c := client.(map[string]interface{})
					if c["email"] == clientTraffic.Email {
						if !clientTraffic.Enable {
							clients = RemoveIndex(clients, index-indexDecrease)
							indexDecrease++
						}
					}
				}
			}

			// clear client config for additional parameters
			var final_clients []interface{}
			for _, client := range clients {

				c := client.(map[string]interface{})

				if c["enable"] != nil {
					if enable, ok := c["enable"].(bool); ok && !enable {
						continue
					}
				}
				for key := range c {
					if key != "email" && key != "id" && key != "password" && key != "flow" && key != "method" && key != "auth" && key != "reverse" {
						delete(c, key)
					}
					if flow, ok := c["flow"].(string); ok && flow == "xtls-rprx-vision-udp443" {
						c["flow"] = "xtls-rprx-vision"
					}
				}
				final_clients = append(final_clients, interface{}(c))
			}

			settings["clients"] = final_clients
			modifiedSettings, err := json.MarshalIndent(settings, "", "  ")
			if err != nil {
				return nil, err
			}

			inbound.Settings = string(modifiedSettings)
		}

		if len(inbound.StreamSettings) > 0 {
			// Unmarshal stream JSON
			var stream map[string]interface{}
			json.Unmarshal([]byte(inbound.StreamSettings), &stream)

			// Remove the "settings" field under "tlsSettings" and "realitySettings"
			tlsSettings, ok1 := stream["tlsSettings"].(map[string]interface{})
			realitySettings, ok2 := stream["realitySettings"].(map[string]interface{})
			if ok1 || ok2 {
				if ok1 {
					delete(tlsSettings, "settings")
				} else if ok2 {
					delete(realitySettings, "settings")
				}
			}

			delete(stream, "externalProxy")

			newStream, err := json.MarshalIndent(stream, "", "  ")
			if err != nil {
				return nil, err
			}
			inbound.StreamSettings = string(newStream)
		}

		inboundConfig := inbound.GenXrayInboundConfig()
		xrayConfig.InboundConfigs = append(xrayConfig.InboundConfigs, *inboundConfig)
	}

	xrayConfig.OutboundConfigs = []xray.OutboundConfig{}

	outbounds, err := s.outboundService.GetAllOutbounds()
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		outboundConfig := outbound.GenXrayOutboundConfig()
		xrayConfig.OutboundConfigs = append(xrayConfig.OutboundConfigs, *outboundConfig)
	}
	mergedRouting, err := s.mergeRoutingRules(xrayConfig.RouterConfig)
	if err != nil {
		return nil, err
	}
	xrayConfig.RouterConfig = mergedRouting

	return xrayConfig, nil
}

func (s *XrayService) mergeRoutingRules(routerConfig json_util.RawMessage) (json_util.RawMessage, error) {
	routing := map[string]interface{}{}
	if len(routerConfig) > 0 {
		json.Unmarshal(routerConfig, &routing)
	}
	ruleList, err := s.routingRuleService.BuildDbRulesArray()
	if err != nil {
		return nil, err
	}
	routing["rules"] = ruleList
	b, err := json.Marshal(routing)
	if err != nil {
		return nil, err
	}
	return json_util.RawMessage(b), nil
}

func (s *XrayService) GetXrayTraffic() ([]*xray.Traffic, []*xray.ClientTraffic, error) {
	if !s.IsXrayRunning() {
		return nil, nil, errors.New("xray is not running")
	}
	if err := s.xrayAPI.Init(p.GetAPIAddr()); err != nil {
		return nil, nil, err
	}
	defer s.xrayAPI.Close()
	return s.xrayAPI.GetTraffic(true)
}

func (s *XrayService) RefreshOnlineUsersCache() error {
	if !s.IsXrayRunning() {
		ClearOnlineUsersCache()
		return nil
	}
	if err := s.xrayAPI.Init(p.GetAPIAddr()); err != nil {
		return err
	}
	defer s.xrayAPI.Close()

	users, err := s.xrayAPI.GetUsersOnlineInfo()
	if err != nil {
		return err
	}
	SetOnlineUsersCache(users)
	return nil
}

func (s *XrayService) GetOnlineUsers() []xray.OnlineUserInfo {
	return GetOnlineUsersCache()
}

func (s *XrayService) RestartXray(isForce bool) error {
	lock.Lock()
	defer lock.Unlock()
	logger.Debug("restart xray, force:", isForce)

	xrayConfig, err := s.GetXrayConfig()
	if err != nil {
		return err
	}

	if p != nil && p.IsRunning() {
		if !isForce && p.GetConfig().Equals(xrayConfig) {
			logger.Debug("It does not need to restart xray")
			return nil
		}
		p.Stop()
	}

	p = xray.NewProcess(xrayConfig)
	result = ""
	err = p.Start()
	if err != nil {
		return err
	}
	return nil
}

func (s *XrayService) StopXray() error {
	lock.Lock()
	defer lock.Unlock()
	logger.Debug("stop xray")
	ClearOnlineUsersCache()
	if s.IsXrayRunning() {
		return p.Stop()
	}
	return errors.New("xray is not running")
}

func (s *XrayService) SetToNeedRestart() {
	isNeedXrayRestart.Store(true)
}

func (s *XrayService) IsNeedRestartAndSetFalse() bool {
	return isNeedXrayRestart.CompareAndSwap(true, false)
}
