package sub

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/util/random"
	"github.com/alireza0/x-ui/web/service"
	"github.com/alireza0/x-ui/xray"

	"github.com/goccy/go-json"
)

type SubService struct {
	address     string
	showInfo    bool
	remarkModel string

	inboundService service.InboundService
}

func NewSubService(showInfo bool, remarkModel string) *SubService {
	return &SubService{
		showInfo:    showInfo,
		remarkModel: remarkModel,
	}
}

func (s *SubService) GetSubs(subId string, host string) ([]string, string, error) {
	s.address = host
	var result []string
	var header string
	var traffic xray.ClientTraffic
	var clientTraffics []xray.ClientTraffic
	inbounds, err := s.getInboundsBySubId(subId)
	if err != nil {
		return nil, "", err
	}

	// Prepare Inbounds
	for _, inbound := range inbounds {
		clients, err := s.inboundService.GetClients(inbound)
		if err != nil {
			logger.Error("SubService - GetClients: Unable to get clients from inbound")
		}
		if clients == nil {
			continue
		}
		if len(inbound.Listen) > 0 && inbound.Listen[0] == '@' {
			listen, port, streamSettings, err := s.getFallbackMaster(inbound.Listen, inbound.StreamSettings)
			if err == nil {
				inbound.Listen = listen
				inbound.Port = port
				inbound.StreamSettings = streamSettings
			}
		}
		for _, client := range clients {
			if client.Enable && client.SubID == subId {
				link := s.getLink(inbound, client.Email)
				result = append(result, link)
				clientTraffics = append(clientTraffics, s.getClientTraffics(inbound.ClientStats, client.Email))
			}
		}
	}

	// Prepare statistics
	for index, clientTraffic := range clientTraffics {
		if index == 0 {
			traffic.Up = clientTraffic.Up
			traffic.Down = clientTraffic.Down
			traffic.Total = clientTraffic.Total
			if clientTraffic.ExpiryTime > 0 {
				traffic.ExpiryTime = clientTraffic.ExpiryTime
			}
		} else {
			traffic.Up += clientTraffic.Up
			traffic.Down += clientTraffic.Down
			if traffic.Total == 0 || clientTraffic.Total == 0 {
				traffic.Total = 0
			} else {
				traffic.Total += clientTraffic.Total
			}
			if clientTraffic.ExpiryTime != traffic.ExpiryTime {
				traffic.ExpiryTime = 0
			}
		}
	}
	header = fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", traffic.Up, traffic.Down, traffic.Total, traffic.ExpiryTime/1000)
	return result, header, nil
}

func (s *SubService) getInboundsBySubId(subId string) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Where(`id in (
		SELECT DISTINCT inbounds.id
		FROM inbounds,
			JSON_EACH(JSON_EXTRACT(inbounds.settings, '$.clients')) AS client 
		WHERE
			protocol in ('vmess','vless','trojan','shadowsocks','hysteria')
			AND JSON_EXTRACT(client.value, '$.subId') = ? AND enable = ?
	)`, subId, true).Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *SubService) getClientTraffics(traffics []xray.ClientTraffic, email string) xray.ClientTraffic {
	for _, traffic := range traffics {
		if traffic.Email == email {
			return traffic
		}
	}
	return xray.ClientTraffic{}
}

func (s *SubService) getFallbackMaster(dest string, streamSettings string) (string, int, string, error) {
	db := database.GetDB()
	var inbound *model.Inbound
	err := db.Model(model.Inbound{}).
		Where("JSON_TYPE(settings, '$.fallbacks') = 'array'").
		Where("EXISTS (SELECT * FROM json_each(settings, '$.fallbacks') WHERE json_extract(value, '$.dest') = ?)", dest).
		Find(&inbound).Error
	if err != nil {
		return "", 0, "", err
	}

	var stream map[string]interface{}
	json.Unmarshal([]byte(streamSettings), &stream)
	var masterStream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &masterStream)
	stream["security"] = masterStream["security"]
	stream["tlsSettings"] = masterStream["tlsSettings"]
	stream["externalProxy"] = masterStream["externalProxy"]
	modifiedStream, _ := json.MarshalIndent(stream, "", "  ")

	return inbound.Listen, inbound.Port, string(modifiedStream), nil
}

func (s *SubService) getLink(inbound *model.Inbound, email string) string {
	switch inbound.Protocol {
	case "vmess":
		return s.genVmessLink(inbound, email)
	case "vless":
		return s.genVlessLink(inbound, email)
	case "trojan":
		return s.genTrojanLink(inbound, email)
	case "shadowsocks":
		return s.genShadowsocksLink(inbound, email)
	case "hysteria":
		return s.genHysteriaLink(inbound, email)
	}
	return ""
}

func (s *SubService) genVmessLink(inbound *model.Inbound, email string) string {
	if inbound.Protocol != model.VMess {
		return ""
	}
	obj := map[string]interface{}{
		"v":    "2",
		"add":  s.address,
		"port": inbound.Port,
		"type": "none",
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	network, _ := stream["network"].(string)
	obj["net"] = network
	switch network {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		obj["type"] = typeStr
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			obj["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		obj["type"], _ = header["type"].(string)
		obj["path"], _ = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		obj["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		obj["path"], _ = grpc["serviceName"].(string)
		obj["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			obj["type"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		obj["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		obj["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			if len(headers) == 0 {
				if h, ok := searchKey(xhttp["extra"], "headers"); ok {
					headers, _ = h.(map[string]interface{})
				}
			}
			obj["host"] = searchHost(headers)
		}
		obj["mode"] = xhttp["mode"].(string)
		if xExtra := buildXhttpExtraForShare(xhttp); xExtra != nil {
			obj["extra"] = xExtra
		}
	}

	security, _ := stream["security"].(string)
	obj["tls"] = security
	if security == "tls" {
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		if len(alpns) > 0 {
			var alpn []string
			for _, a := range alpns {
				alpn = append(alpn, a.(string))
			}
			obj["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			obj["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				obj["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				obj["allowInsecure"], _ = insecure.(bool)
			}
			if pcs := pinnedPeerCertSha256ToString(tlsSettings); pcs != "" {
				obj["pcs"] = pcs
			}
			if vcn, ok := searchKey(tlsSettings, "verifyPeerCertByName"); ok {
				if vcnStr, _ := vcn.(string); vcnStr != "" {
					obj["vcn"] = vcnStr
				}
			}
		}
	}

	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	obj["id"] = clients[clientIndex].ID

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			newObj := map[string]interface{}{}
			for key, value := range obj {
				if !(newSecurity == "none" && (key == "alpn" || key == "sni" || key == "fp" || key == "allowInsecure")) {
					newObj[key] = value
				}
			}
			newObj["ps"] = s.genRemark(inbound, email, ep["remark"].(string))
			newObj["add"] = ep["dest"].(string)
			newObj["port"] = int(ep["port"].(float64))

			if newSecurity != "same" {
				newObj["tls"] = newSecurity
			}
			if utlsValue, ok := ep["utls"].(string); ok && len(utlsValue) > 0 {
				newObj["fp"] = utlsValue
			}
			if sniValue, ok := ep["sni"].(string); ok && len(sniValue) > 0 {
				newObj["sni"] = sniValue
			}
			if alpnValue, ok := ep["alpn"].([]interface{}); ok && len(alpnValue) > 0 {
				alpn := make([]string, len(alpnValue))
				for i, a := range alpnValue {
					alpn[i] = a.(string)
				}
				newObj["alpn"] = strings.Join(alpn, ",")
			}
			if allowInsecureValue, ok := ep["allowInsecure"].(bool); ok && allowInsecureValue {
				newObj["allowInsecure"] = "1"
			}
			if fragmentValue, ok := ep["fragment"].(map[string]interface{}); ok {
				newObj["packets"], _ = fragmentValue["packets"].(string)
				newObj["length"], _ = fragmentValue["length"].(string)
				newObj["interval"], _ = fragmentValue["interval"].(string)
			}
			if index > 0 {
				links += "\n"
			}
			jsonStr, _ := json.MarshalIndent(newObj, "", "  ")
			links += "vmess://" + base64.StdEncoding.EncodeToString(jsonStr)
		}
		return links
	}

	obj["ps"] = s.genRemark(inbound, email, "")

	jsonStr, _ := json.MarshalIndent(obj, "", "  ")
	return "vmess://" + base64.StdEncoding.EncodeToString(jsonStr)
}

func (s *SubService) genVlessLink(inbound *model.Inbound, email string) string {
	address := s.address
	if inbound.Protocol != model.VLESS {
		return ""
	}
	var vlessSettings map[string]interface{}
	_ = json.Unmarshal([]byte(inbound.Settings), &vlessSettings)

	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	uuid := clients[clientIndex].ID
	port := inbound.Port
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	if vlessEncryption, ok := vlessSettings["encryption"].(string); ok && vlessEncryption != "" {
		params["encryption"] = vlessEncryption
	}
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			if len(headers) == 0 {
				if h, ok := searchKey(xhttp["extra"], "headers"); ok {
					headers, _ = h.(map[string]interface{})
				}
			}
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
		if xExtra := buildXhttpExtraForShare(xhttp); xExtra != nil {
			if xExtraJSON, err := json.Marshal(xExtra); err == nil {
				params["extra"] = string(xExtraJSON)
			}
		}
	}
	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
			if pcs := pinnedPeerCertSha256ToString(tlsSettings); pcs != "" {
				params["pcs"] = pcs
			}
			if vcn, ok := searchKey(tlsSettings, "verifyPeerCertByName"); ok {
				if vcnStr, _ := vcn.(string); vcnStr != "" {
					params["vcn"] = vcnStr
				}
			}
		}

		if streamNetwork == "tcp" && len(clients[clientIndex].Flow) > 0 {
			params["flow"] = clients[clientIndex].Flow
		}
	}

	if security == "reality" {
		params["security"] = "reality"
		realitySetting, _ := stream["realitySettings"].(map[string]interface{})
		realitySettings, _ := searchKey(realitySetting, "settings")
		if realitySetting != nil {
			if sniValue, ok := searchKey(realitySetting, "serverNames"); ok {
				sNames, _ := sniValue.([]interface{})
				params["sni"] = sNames[random.Num(len(sNames))].(string)
			}
			if pbkValue, ok := searchKey(realitySettings, "publicKey"); ok {
				params["pbk"], _ = pbkValue.(string)
			}
			if sidValue, ok := searchKey(realitySetting, "shortIds"); ok {
				shortIds, _ := sidValue.([]interface{})
				params["sid"] = shortIds[random.Num(len(shortIds))].(string)
			}
			if fpValue, ok := searchKey(realitySettings, "fingerprint"); ok {
				if fp, ok := fpValue.(string); ok && len(fp) > 0 {
					params["fp"] = fp
				}
			}
			if pqvValue, ok := searchKey(realitySettings, "mldsa65Verify"); ok {
				if pqv, ok := pqvValue.(string); ok && len(pqv) > 0 {
					params["pqv"] = pqv
				}
			}
			params["spx"] = "/" + random.Seq(15)
		}

		if streamNetwork == "tcp" && len(clients[clientIndex].Flow) > 0 {
			params["flow"] = clients[clientIndex].Flow
		}
	}

	if security != "tls" && security != "reality" {
		params["security"] = "none"
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			if utlsValue, ok := ep["utls"].(string); ok && len(utlsValue) > 0 {
				params["fp"] = utlsValue
			}
			if sniValue, ok := ep["sni"].(string); ok && len(sniValue) > 0 {
				params["sni"] = sniValue
			}
			if alpnValue, ok := ep["alpn"].([]interface{}); ok && len(alpnValue) > 0 {
				alpn := make([]string, len(alpnValue))
				for i, a := range alpnValue {
					alpn[i] = a.(string)
				}
				params["alpn"] = strings.Join(alpn, ",")
			}
			if allowInsecureValue, ok := ep["allowInsecure"].(bool); ok && allowInsecureValue {
				params["allowInsecure"] = "1"
			}
			if fragmentValue, ok := ep["fragment"].(map[string]interface{}); ok {
				params["packets"] = fragmentValue["packets"].(string)
				params["length"] = fragmentValue["length"].(string)
				params["interval"] = fragmentValue["interval"].(string)
			}
			link := fmt.Sprintf("vless://%s@%s:%d", uuid, dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("vless://%s@%s:%d", uuid, address, port)
	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genTrojanLink(inbound *model.Inbound, email string) string {
	address := s.address
	if inbound.Protocol != model.Trojan {
		return ""
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	password := clients[clientIndex].Password
	port := inbound.Port
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			if len(headers) == 0 {
				if h, ok := searchKey(xhttp["extra"], "headers"); ok {
					headers, _ = h.(map[string]interface{})
				}
			}
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
		if xExtra := buildXhttpExtraForShare(xhttp); xExtra != nil {
			if xExtraJSON, err := json.Marshal(xExtra); err == nil {
				params["extra"] = string(xExtraJSON)
			}
		}
	}
	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
			if pcs := pinnedPeerCertSha256ToString(tlsSettings); pcs != "" {
				params["pcs"] = pcs
			}
			if vcn, ok := searchKey(tlsSettings, "verifyPeerCertByName"); ok {
				if vcnStr, _ := vcn.(string); vcnStr != "" {
					params["vcn"] = vcnStr
				}
			}
		}
	}

	if security == "reality" {
		params["security"] = "reality"
		realitySetting, _ := stream["realitySettings"].(map[string]interface{})
		realitySettings, _ := searchKey(realitySetting, "settings")
		if realitySetting != nil {
			if sniValue, ok := searchKey(realitySetting, "serverNames"); ok {
				sNames, _ := sniValue.([]interface{})
				params["sni"] = sNames[random.Num(len(sNames))].(string)
			}
			if pbkValue, ok := searchKey(realitySettings, "publicKey"); ok {
				params["pbk"], _ = pbkValue.(string)
			}
			if sidValue, ok := searchKey(realitySetting, "shortIds"); ok {
				shortIds, _ := sidValue.([]interface{})
				params["sid"] = shortIds[random.Num(len(shortIds))].(string)
			}
			if fpValue, ok := searchKey(realitySettings, "fingerprint"); ok {
				if fp, ok := fpValue.(string); ok && len(fp) > 0 {
					params["fp"] = fp
				}
			}
			if pqvValue, ok := searchKey(realitySettings, "mldsa65Verify"); ok {
				if pqv, ok := pqvValue.(string); ok && len(pqv) > 0 {
					params["pqv"] = pqv
				}
			}
			params["spx"] = "/" + random.Seq(15)
		}
	}

	if security != "tls" && security != "reality" {
		params["security"] = "none"
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			if utlsValue, ok := ep["utls"].(string); ok && len(utlsValue) > 0 {
				params["fp"] = utlsValue
			}
			if sniValue, ok := ep["sni"].(string); ok && len(sniValue) > 0 {
				params["sni"] = sniValue
			}
			if alpnValue, ok := ep["alpn"].([]interface{}); ok && len(alpnValue) > 0 {
				alpn := make([]string, len(alpnValue))
				for i, a := range alpnValue {
					alpn[i] = a.(string)
				}
				params["alpn"] = strings.Join(alpn, ",")
			}
			if allowInsecureValue, ok := ep["allowInsecure"].(bool); ok && allowInsecureValue {
				params["allowInsecure"] = "1"
			}
			if fragmentValue, ok := ep["fragment"].(map[string]interface{}); ok {
				params["packets"] = fragmentValue["packets"].(string)
				params["length"] = fragmentValue["length"].(string)
				params["interval"] = fragmentValue["interval"].(string)
			}
			link := fmt.Sprintf("trojan://%s@%s:%d", password, dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("trojan://%s@%s:%d", password, address, port)

	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genShadowsocksLink(inbound *model.Inbound, email string) string {
	address := s.address
	if inbound.Protocol != model.Shadowsocks {
		return ""
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)

	var settings map[string]interface{}
	json.Unmarshal([]byte(inbound.Settings), &settings)
	inboundPassword := settings["password"].(string)
	method := settings["method"].(string)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			if len(headers) == 0 {
				if h, ok := searchKey(xhttp["extra"], "headers"); ok {
					headers, _ = h.(map[string]interface{})
				}
			}
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
		if xExtra := buildXhttpExtraForShare(xhttp); xExtra != nil {
			if xExtraJSON, err := json.Marshal(xExtra); err == nil {
				params["extra"] = string(xExtraJSON)
			}
		}
	}

	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
			if pcs := pinnedPeerCertSha256ToString(tlsSettings); pcs != "" {
				params["pcs"] = pcs
			}
			if vcn, ok := searchKey(tlsSettings, "verifyPeerCertByName"); ok {
				if vcnStr, _ := vcn.(string); vcnStr != "" {
					params["vcn"] = vcnStr
				}
			}
		}
	}

	encPart := fmt.Sprintf("%s:%s", method, clients[clientIndex].Password)
	if method[0] == '2' {
		encPart = fmt.Sprintf("%s:%s:%s", method, inboundPassword, clients[clientIndex].Password)
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			if utlsValue, ok := ep["utls"].(string); ok && len(utlsValue) > 0 {
				params["fp"] = utlsValue
			}
			if sniValue, ok := ep["sni"].(string); ok && len(sniValue) > 0 {
				params["sni"] = sniValue
			}
			if alpnValue, ok := ep["alpn"].([]interface{}); ok && len(alpnValue) > 0 {
				alpn := make([]string, len(alpnValue))
				for i, a := range alpnValue {
					alpn[i] = a.(string)
				}
				params["alpn"] = strings.Join(alpn, ",")
			}
			if allowInsecureValue, ok := ep["allowInsecure"].(bool); ok && allowInsecureValue {
				params["allowInsecure"] = "1"
			}
			if fragmentValue, ok := ep["fragment"].(map[string]interface{}); ok {
				params["packets"] = fragmentValue["packets"].(string)
				params["length"] = fragmentValue["length"].(string)
				params["interval"] = fragmentValue["interval"].(string)
			}
			link := fmt.Sprintf("ss://%s@%s:%d", base64.StdEncoding.EncodeToString([]byte(encPart)), dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("ss://%s@%s:%d", base64.StdEncoding.EncodeToString([]byte(encPart)), address, inbound.Port)
	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genHysteriaLink(inbound *model.Inbound, email string) string {
	address := s.address
	if inbound.Protocol != model.Hysteria {
		return ""
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	auth := clients[clientIndex].Auth
	port := inbound.Port
	params := make(map[string]string)

	params["security"] = "tls"
	tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
	alpns, _ := tlsSetting["alpn"].([]interface{})
	var alpn []string
	for _, a := range alpns {
		alpn = append(alpn, a.(string))
	}
	if len(alpn) > 0 {
		params["alpn"] = strings.Join(alpn, ",")
	}
	if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
		params["sni"], _ = sniValue.(string)
	}

	tlsSettings, _ := searchKey(tlsSetting, "settings")
	if tlsSetting != nil {
		if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
			params["fp"], _ = fpValue.(string)
		}
		if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
			if insecure.(bool) {
				params["insecure"] = "1"
			}
		}
	}

	var settings map[string]interface{}
	json.Unmarshal([]byte(inbound.Settings), &settings)
	version, _ := settings["version"].(float64)
	protocol := "hysteria2"
	if int(version) == 1 {
		protocol = "hysteria"
	}

	if fm, ok := stream["finalmask"].(map[string]interface{}); ok {
		if qp, ok := fm["quicParams"].(map[string]interface{}); ok {
			if v, ok := qp["congestion"].(string); ok && v != "" {
				params["congestion"] = v
			}
			if v, ok := qp["brutalUp"].(string); ok && v != "" {
				params["up"] = v
			}
			if v, ok := qp["brutalDown"].(string); ok && v != "" {
				params["down"] = v
			}
			if udpHop, ok := qp["udpHop"].(map[string]interface{}); ok {
				if v, ok := udpHop["ports"].(string); ok && v != "" {
					params["mport"] = v
				}
				switch iv := udpHop["interval"].(type) {
				case string:
					if iv != "" {
						params["udphopInterval"] = iv
					}
				case float64:
					params["udphopInterval"] = fmt.Sprintf("%d", int(iv))
				}
			}
			for jsonKey, paramKey := range map[string]string{
				"initStreamReceiveWindow":     "initStreamReceiveWindow",
				"maxStreamReceiveWindow":      "maxStreamReceiveWindow",
				"initConnectionReceiveWindow": "initConnectionReceiveWindow",
				"maxConnectionReceiveWindow":  "maxConnectionReceiveWindow",
				"maxIdleTimeout":              "maxIdleTimeout",
				"keepAlivePeriod":             "keepAlivePeriod",
			} {
				if v, ok := qp[jsonKey].(float64); ok && v != 0 {
					params[paramKey] = fmt.Sprintf("%d", int(v))
				}
			}
			if v, ok := qp["disablePathMTUDiscovery"].(bool); ok && v {
				params["disablePathMTUDiscovery"] = "true"
			}
		}
		if udpMasks, ok := fm["udp"].([]interface{}); ok {
			for _, m := range udpMasks {
				mask, _ := m.(map[string]interface{})
				settings, _ := mask["settings"].(map[string]interface{})
				password, _ := settings["password"].(string)
				maskType, _ := mask["type"].(string)
				if password != "" && maskType != "" {
					params["obfs"] = maskType
					params["obfs-password"] = password
					break
				}
			}
		}
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			if utlsValue, ok := ep["utls"].(string); ok && len(utlsValue) > 0 {
				params["fp"] = utlsValue
			}
			if sniValue, ok := ep["sni"].(string); ok && len(sniValue) > 0 {
				params["sni"] = sniValue
			}
			if alpnValue, ok := ep["alpn"].([]interface{}); ok && len(alpnValue) > 0 {
				alpn := make([]string, len(alpnValue))
				for i, a := range alpnValue {
					alpn[i] = a.(string)
				}
				params["alpn"] = strings.Join(alpn, ",")
			}
			if allowInsecureValue, ok := ep["allowInsecure"].(bool); ok && allowInsecureValue {
				params["allowInsecure"] = "1"
			}
			if fragmentValue, ok := ep["fragment"].(map[string]interface{}); ok {
				params["packets"] = fragmentValue["packets"].(string)
				params["length"] = fragmentValue["length"].(string)
				params["interval"] = fragmentValue["interval"].(string)
			}
			link := fmt.Sprintf("%s://%s@%s:%d", protocol, auth, dest, port)
			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = "tls"
			}
			url, _ := url.Parse(link)
			q := url.Query()
			for k, v := range params {
				q.Add(k, v)
			}
			url.RawQuery = q.Encode()
			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))
			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("%s://%s@%s:%d", protocol, auth, address, port)
	url, _ := url.Parse(link)
	q := url.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	url.RawQuery = q.Encode()
	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genRemark(inbound *model.Inbound, email string, extra string) string {
	separationChar := string(s.remarkModel[0])
	orderChars := s.remarkModel[1:]
	orders := map[byte]string{
		'i': "",
		'e': "",
		'o': "",
	}
	if len(email) > 0 {
		orders['e'] = email
	}
	if len(inbound.Remark) > 0 {
		orders['i'] = inbound.Remark
	}
	if len(extra) > 0 {
		orders['o'] = extra
	}

	var remark []string
	for i := 0; i < len(orderChars); i++ {
		char := orderChars[i]
		order, exists := orders[char]
		if exists && order != "" {
			remark = append(remark, order)
		}
	}

	if s.showInfo {
		statsExist := false
		var stats xray.ClientTraffic
		for _, clientStat := range inbound.ClientStats {
			if clientStat.Email == email {
				stats = clientStat
				statsExist = true
				break
			}
		}

		// Get remained days
		if statsExist {
			if !stats.Enable {
				return fmt.Sprintf("⛔️N/A%s%s", separationChar, strings.Join(remark, separationChar))
			}
			if vol := stats.Total - (stats.Up + stats.Down); vol > 0 {
				remark = append(remark, fmt.Sprintf("%s%s", common.FormatTraffic(vol), "📊"))
			}
			now := time.Now().Unix()
			switch exp := stats.ExpiryTime / 1000; {
			case exp > 0:
				remainingSeconds := exp - now
				days := remainingSeconds / 86400
				hours := (remainingSeconds % 86400) / 3600
				minutes := (remainingSeconds % 3600) / 60
				if days > 0 {
					if hours > 0 {
						remark = append(remark, fmt.Sprintf("%dD,%dH⏳", days, hours))
					} else {
						remark = append(remark, fmt.Sprintf("%dD⏳", days))
					}
				} else if hours > 0 {
					remark = append(remark, fmt.Sprintf("%dH⏳", hours))
				} else {
					remark = append(remark, fmt.Sprintf("%dM⏳", minutes))
				}
			case exp < 0:
				days := exp / -86400
				hours := (exp % -86400) / 3600
				minutes := (exp % -3600) / 60
				if days > 0 {
					if hours > 0 {
						remark = append(remark, fmt.Sprintf("%dD,%dH⏳", days, hours))
					} else {
						remark = append(remark, fmt.Sprintf("%dD⏳", days))
					}
				} else if hours > 0 {
					remark = append(remark, fmt.Sprintf("%dH⏳", hours))
				} else {
					remark = append(remark, fmt.Sprintf("%dM⏳", minutes))
				}
			}
		}
	}
	return strings.Join(remark, separationChar)
}

func buildXhttpExtraForShare(xhttp map[string]interface{}) map[string]interface{} {
	if xhttp == nil {
		return nil
	}
	extra, _ := xhttp["extra"].(map[string]interface{})
	if len(extra) == 0 {
		return nil
	}
	cleaned := map[string]interface{}{}
	for k, v := range extra {
		switch val := v.(type) {
		case nil:
			continue
		case string:
			if val == "" {
				continue
			}
		case bool:
			if !val {
				continue
			}
		case float64:
			if val == 0 {
				continue
			}
		case int:
			if val == 0 {
				continue
			}
		case int64:
			if val == 0 {
				continue
			}
		case []interface{}:
			if len(val) == 0 {
				continue
			}
		case map[string]interface{}:
			if len(val) == 0 {
				continue
			}
			if k == "xmux" {
				allEmpty := true
				for _, mv := range val {
					switch mvt := mv.(type) {
					case nil:
					case string:
						if mvt != "" {
							allEmpty = false
						}
					case bool:
						if mvt {
							allEmpty = false
						}
					case float64:
						if mvt != 0 {
							allEmpty = false
						}
					default:
						allEmpty = false
					}
					if !allEmpty {
						break
					}
				}
				if allEmpty {
					continue
				}
			}
		}
		cleaned[k] = v
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func searchKey(data interface{}, key string) (interface{}, bool) {
	switch val := data.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if k == key {
				return v, true
			}
			if result, ok := searchKey(v, key); ok {
				return result, true
			}
		}
	case []interface{}:
		for _, v := range val {
			if result, ok := searchKey(v, key); ok {
				return result, true
			}
		}
	}
	return nil, false
}

func pinnedPeerCertSha256ToString(tlsSettings interface{}) string {
	pcsValue, ok := searchKey(tlsSettings, "pinnedPeerCertSha256")
	if !ok {
		return ""
	}
	switch v := pcsValue.(type) {
	case []interface{}:
		var pcs []string
		for _, h := range v {
			if hs, ok := h.(string); ok && len(hs) > 0 {
				pcs = append(pcs, hs)
			}
		}
		return strings.Join(pcs, ",")
	case string:
		return v
	}
	return ""
}

func searchHost(headers interface{}) string {
	data, _ := headers.(map[string]interface{})
	for k, v := range data {
		if strings.EqualFold(k, "host") {
			switch v.(type) {
			case []interface{}:
				hosts, _ := v.([]interface{})
				if len(hosts) > 0 {
					return hosts[0].(string)
				} else {
					return ""
				}
			case interface{}:
				return v.(string)
			}
		}
	}

	return ""
}
