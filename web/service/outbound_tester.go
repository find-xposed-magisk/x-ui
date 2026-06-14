package service

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/xray"
)

// OutboundTestResult holds the result of testing a single outbound.
type OutboundTestResult struct {
	Id      int    `json:"id"`
	Tag     string `json:"tag"`
	Success bool   `json:"success"`
	Delay   int64  `json:"delay"`
	Message string `json:"message"`
}

func (s *OutboundService) TestOutbound(id int) (*OutboundTestResult, error) {
	outbound, err := s.GetOutbound(id)
	if err != nil {
		return nil, err
	}
	testURL, err := s.settingService.GetOutboundTestUrl()
	if err != nil || testURL == "" {
		testURL = "https://www.gstatic.com/generate_204"
	}
	return s.testSingleOutbound(outbound, testURL), nil
}

func (s *OutboundService) testSingleOutbound(outbound *model.Outbound, testURL string) *OutboundTestResult {
	result := &OutboundTestResult{
		Id:  outbound.Id,
		Tag: outbound.Tag,
	}

	port, err := getFreePort()
	if err != nil {
		result.Message = err.Error()
		return result
	}

	outboundConfig := outbound.GenXrayOutboundConfig()
	outboundConfig.Tag = "proxy"
	outboundJson, err := json.MarshalIndent(outboundConfig, "    ", "  ")
	if err != nil {
		result.Message = err.Error()
		return result
	}

	configContent := config.GetTestXrayTemplate()
	configContent = strings.Replace(configContent, "__INBOUND_PORT__", strconv.Itoa(port), 1)
	configContent = strings.Replace(configContent, "__OUTBOUND__", string(outboundJson), 1)

	binFolder := config.GetBinFolderPath()
	configPath := filepath.Join(binFolder, fmt.Sprintf("test_outbound_%d.json", port))
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		result.Message = err.Error()
		return result
	}
	defer os.Remove(configPath)

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		absConfigPath = configPath
	}
	absBinFolder, err := filepath.Abs(binFolder)
	if err != nil {
		absBinFolder = binFolder
	}
	binaryPath, err := filepath.Abs(xray.GetBinaryPath())
	if err != nil {
		binaryPath = xray.GetBinaryPath()
	}

	cmd := exec.Command(binaryPath, "-c", absConfigPath)
	cmd.Dir = absBinFolder
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+absBinFolder)
	if err := cmd.Start(); err != nil {
		result.Message = err.Error()
		return result
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	if err := waitForPort(port, 5*time.Second); err != nil {
		result.Message = "xray failed to start: " + err.Error()
		return result
	}

	delay, err := measureDelay(port, testURL)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	result.Success = true
	result.Delay = delay
	logger.Debug("Outbound test succeeded:", outbound.Tag, delay, "ms")
	return result
}

func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, common.NewErrorf("unable to find a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return common.NewError("timed out waiting for proxy port")
}

func measureDelay(port int, testURL string) (int64, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return 0, err
	}

	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
	defer transport.CloseIdleConnections()

	start := time.Now()
	resp, err := client.Get(testURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return 0, common.NewErrorf("unexpected status code: %d", resp.StatusCode)
	}
	return time.Since(start).Milliseconds(), nil
}
