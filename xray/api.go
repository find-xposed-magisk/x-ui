package xray

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"

	"github.com/xtls/xray-core/app/proxyman/command"
	routingcommand "github.com/xtls/xray-core/app/router/command"
	statsService "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/infra/conf"
	hysteriaAccount "github.com/xtls/xray-core/proxy/hysteria/account"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/shadowsocks_2022"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type XrayAPI struct {
	HandlerServiceClient *command.HandlerServiceClient
	RoutingServiceClient *routingcommand.RoutingServiceClient
	StatsServiceClient   *statsService.StatsServiceClient
	grpcClient           *grpc.ClientConn
	isConnected          bool
}

func (x *XrayAPI) Init(apiAddr string) (err error) {
	if apiAddr == "" {
		return common.NewError("xray api port wrong:", apiAddr)
	}
	x.grpcClient, err = grpc.NewClient(apiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	x.isConnected = true

	hsClient := command.NewHandlerServiceClient(x.grpcClient)
	rsClient := routingcommand.NewRoutingServiceClient(x.grpcClient)
	ssClient := statsService.NewStatsServiceClient(x.grpcClient)

	x.HandlerServiceClient = &hsClient
	x.RoutingServiceClient = &rsClient
	x.StatsServiceClient = &ssClient

	return
}

func (x *XrayAPI) Close() {
	if x.grpcClient != nil {
		x.grpcClient.Close()
		x.grpcClient = nil
	}
	x.HandlerServiceClient = nil
	x.RoutingServiceClient = nil
	x.StatsServiceClient = nil
	x.isConnected = false
}

func (x *XrayAPI) AddRule(ruleJSON []byte, shouldAppend bool) error {
	if x.RoutingServiceClient == nil {
		return common.NewError("routing api is not initialized")
	}
	rc := &conf.RouterConfig{RuleList: []json.RawMessage{ruleJSON}}
	built, err := rc.Build()
	if err != nil {
		logger.Debug("Failed to build routing rule:", err)
		return err
	}
	if len(built.Rule) == 0 {
		return common.NewError("empty routing rule")
	}
	client := *x.RoutingServiceClient
	_, err = client.AddRule(context.Background(), &routingcommand.AddRuleRequest{
		Config:       serial.ToTypedMessage(built),
		ShouldAppend: shouldAppend,
	})
	return err
}

func (x *XrayAPI) DelRule(ruleTag string) error {
	if x.RoutingServiceClient == nil {
		return common.NewError("routing api is not initialized")
	}
	client := *x.RoutingServiceClient
	_, err := client.RemoveRule(context.Background(), &routingcommand.RemoveRuleRequest{
		RuleTag: ruleTag,
	})
	return err
}

func (x *XrayAPI) AddInbound(inbound []byte) error {
	client := *x.HandlerServiceClient

	conf := new(conf.InboundDetourConfig)
	err := json.Unmarshal(inbound, conf)
	if err != nil {
		logger.Debug("Failed to unmarshal inbound:", err)
		return err
	}
	config, err := conf.Build()
	if err != nil {
		logger.Debug("Failed to build inbound Detur:", err)
		return err
	}
	inboundConfig := command.AddInboundRequest{Inbound: config}

	_, err = client.AddInbound(context.Background(), &inboundConfig)

	return err
}

func (x *XrayAPI) DelInbound(tag string) error {
	client := *x.HandlerServiceClient
	_, err := client.RemoveInbound(context.Background(), &command.RemoveInboundRequest{
		Tag: tag,
	})
	return err
}

func (x *XrayAPI) AddOutbound(outbound []byte) error {
	client := *x.HandlerServiceClient

	conf := new(conf.OutboundDetourConfig)
	err := json.Unmarshal(outbound, conf)
	if err != nil {
		logger.Debug("Failed to unmarshal outbound:", err)
		return err
	}
	config, err := conf.Build()
	if err != nil {
		logger.Debug("Failed to build outbound:", err)
		return err
	}
	outboundConfig := command.AddOutboundRequest{Outbound: config}

	_, err = client.AddOutbound(context.Background(), &outboundConfig)
	return err
}

func (x *XrayAPI) DelOutbound(tag string) error {
	client := *x.HandlerServiceClient
	_, err := client.RemoveOutbound(context.Background(), &command.RemoveOutboundRequest{
		Tag: tag,
	})
	return err
}

func (x *XrayAPI) AddUser(Protocol string, inboundTag string, user map[string]interface{}) error {
	var account *serial.TypedMessage
	switch Protocol {
	case "vmess":
		account = serial.ToTypedMessage(&vmess.Account{
			Id: user["id"].(string),
		})
	case "vless":
		vlessAccount := &vless.Account{
			Id:   user["id"].(string),
			Flow: user["flow"].(string),
		}
		// Add testseed if provided
		if testseedVal, ok := user["testseed"]; ok {
			if testseedArr, ok := testseedVal.([]interface{}); ok && len(testseedArr) >= 4 {
				testseed := make([]uint32, len(testseedArr))
				for i, v := range testseedArr {
					if num, ok := v.(float64); ok {
						testseed[i] = uint32(num)
					}
				}
				vlessAccount.Testseed = testseed
			} else if testseedArr, ok := testseedVal.([]uint32); ok && len(testseedArr) >= 4 {
				vlessAccount.Testseed = testseedArr
			}
		}
		// Add testpre if provided (for outbound, but can be in user for compatibility)
		if testpreVal, ok := user["testpre"]; ok {
			if testpre, ok := testpreVal.(float64); ok && testpre > 0 {
				vlessAccount.Testpre = uint32(testpre)
			} else if testpre, ok := testpreVal.(uint32); ok && testpre > 0 {
				vlessAccount.Testpre = testpre
			}
		}
		account = serial.ToTypedMessage(vlessAccount)
	case "trojan":
		account = serial.ToTypedMessage(&trojan.Account{
			Password: user["password"].(string),
		})
	case "shadowsocks":
		var ssCipherType shadowsocks.CipherType
		switch user["cipher"].(string) {
		case "aes-128-gcm":
			ssCipherType = shadowsocks.CipherType_AES_128_GCM
		case "aes-256-gcm":
			ssCipherType = shadowsocks.CipherType_AES_256_GCM
		case "chacha20-poly1305", "chacha20-ietf-poly1305":
			ssCipherType = shadowsocks.CipherType_CHACHA20_POLY1305
		case "xchacha20-poly1305", "xchacha20-ietf-poly1305":
			ssCipherType = shadowsocks.CipherType_XCHACHA20_POLY1305
		default:
			ssCipherType = shadowsocks.CipherType_NONE
		}

		if ssCipherType != shadowsocks.CipherType_NONE {
			account = serial.ToTypedMessage(&shadowsocks.Account{
				Password:   user["password"].(string),
				CipherType: ssCipherType,
			})
		} else {
			account = serial.ToTypedMessage(&shadowsocks_2022.ServerConfig{
				Key:   user["password"].(string),
				Email: user["email"].(string),
			})
		}
	case "hysteria":
		account = serial.ToTypedMessage(&hysteriaAccount.Account{
			Auth: user["auth"].(string),
		})
	default:
		return nil
	}

	client := *x.HandlerServiceClient

	_, err := client.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: inboundTag,
		Operation: serial.ToTypedMessage(&command.AddUserOperation{
			User: &protocol.User{
				Email:   user["email"].(string),
				Account: account,
			},
		}),
	})
	return err
}

func (x *XrayAPI) RemoveUser(inboundTag string, email string) error {
	client := *x.HandlerServiceClient
	_, err := client.AlterInbound(context.Background(), &command.AlterInboundRequest{
		Tag: inboundTag,
		Operation: serial.ToTypedMessage(&command.RemoveUserOperation{
			Email: email,
		}),
	})
	return err
}

func (x *XrayAPI) GetTraffic(reset bool) ([]*Traffic, []*ClientTraffic, error) {
	if x.grpcClient == nil {
		return nil, nil, common.NewError("xray api is not initialized")
	}
	trafficRegex := regexp.MustCompile("(inbound|outbound)>>>([^>]+)>>>traffic>>>(downlink|uplink)")
	ClientTrafficRegex := regexp.MustCompile("(user)>>>([^>]+)>>>traffic>>>(downlink|uplink)")

	client := *x.StatsServiceClient
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	request := &statsService.QueryStatsRequest{
		Reset_: reset,
	}
	resp, err := client.QueryStats(ctx, request)
	if err != nil {
		return nil, nil, err
	}
	tagTrafficMap := map[string]*Traffic{}
	emailTrafficMap := map[string]*ClientTraffic{}

	clientTraffics := make([]*ClientTraffic, 0)
	traffics := make([]*Traffic, 0)
	for _, stat := range resp.GetStat() {
		matchs := trafficRegex.FindStringSubmatch(stat.Name)
		if len(matchs) < 3 {

			matchs := ClientTrafficRegex.FindStringSubmatch(stat.Name)
			if len(matchs) < 3 {
				continue
			} else {

				isUser := matchs[1] == "user"
				email := matchs[2]
				isDown := matchs[3] == "downlink"
				if !isUser {
					continue
				}
				traffic, ok := emailTrafficMap[email]
				if !ok {
					traffic = &ClientTraffic{
						Email: email,
					}
					emailTrafficMap[email] = traffic
					clientTraffics = append(clientTraffics, traffic)
				}
				if isDown {
					traffic.Down = stat.Value
				} else {
					traffic.Up = stat.Value
				}

			}
			continue
		}
		isInbound := matchs[1] == "inbound"
		tag := matchs[2]
		isDown := matchs[3] == "downlink"
		if tag == "api" {
			continue
		}
		traffic, ok := tagTrafficMap[tag]
		if !ok {
			traffic = &Traffic{
				IsInbound: isInbound,
				Tag:       tag,
			}
			tagTrafficMap[tag] = traffic
			traffics = append(traffics, traffic)
		}
		if isDown {
			traffic.Down = stat.Value
		} else {
			traffic.Up = stat.Value
		}
	}

	return traffics, clientTraffics, nil
}
