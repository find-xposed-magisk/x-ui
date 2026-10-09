package xray

import (
	"encoding/json"
	"reflect"
	"testing"
)

func streamFrom(t *testing.T, raw string) map[string]any {
	t.Helper()
	stream := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &stream); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return stream
}

func TestTakeUDPHopDropsEmptyQuicParams(t *testing.T) {
	stream := streamFrom(t, `{"network":"hysteria","finalmask":{"quicParams":{"udpHop":{"ports":"20000-50000"}}}}`)
	if hop := TakeUDPHop(stream); hop["ports"] != "20000-50000" {
		t.Fatalf("returned %v", hop)
	}
	if _, ok := stream["finalmask"]; ok {
		t.Fatalf("an empty finalmask was left behind: %v", stream)
	}
}

func TestTakeUDPHopKeepsOtherQuicParams(t *testing.T) {
	stream := streamFrom(t, `{"finalmask":{"quicParams":{"congestion":"bbr","udpHop":{"ports":"443"}}}}`)
	TakeUDPHop(stream)
	want := streamFrom(t, `{"finalmask":{"quicParams":{"congestion":"bbr"}}}`)
	if !reflect.DeepEqual(stream, want) {
		t.Fatalf("got %v, want %v", stream, want)
	}
}

func TestTakeUDPHopWithoutHop(t *testing.T) {
	stream := streamFrom(t, `{"network":"tcp"}`)
	if TakeUDPHop(stream) != nil {
		t.Fatal("found a hop that is not there")
	}
}

func TestMoveUDPHopToMaskPutsMaskFirst(t *testing.T) {
	stream := streamFrom(t, `{"finalmask":{
		"udp":[{"type":"salamander","settings":{"password":"x"}}],
		"quicParams":{"congestion":"bbr","udpHop":{"ports":"20000-50000","interval":"5-10"}}}}`)
	if !MoveUDPHopToMask(stream) {
		t.Fatal("reported no change")
	}
	want := streamFrom(t, `{"finalmask":{
		"udp":[
			{"type":"udphop","settings":{"mode":"intervalLocal,intervalRemote","remotePorts":"20000-50000","interval":"5-10"}},
			{"type":"salamander","settings":{"password":"x"}}],
		"quicParams":{"congestion":"bbr"}}}`)
	if !reflect.DeepEqual(stream, want) {
		t.Fatalf("got %v, want %v", stream, want)
	}
}

func TestMoveUDPHopToMaskKeepsExistingMask(t *testing.T) {
	stream := streamFrom(t, `{"finalmask":{
		"udp":[{"type":"udphop","settings":{"mode":"perConnRemote","remotePorts":"443"}}],
		"quicParams":{"udpHop":{"ports":"20000-50000"}}}}`)
	MoveUDPHopToMask(stream)
	want := streamFrom(t, `{"finalmask":{"udp":[{"type":"udphop","settings":{"mode":"perConnRemote","remotePorts":"443"}}]}}`)
	if !reflect.DeepEqual(stream, want) {
		t.Fatalf("got %v, want %v", stream, want)
	}
}

func TestMoveUDPHopToMaskWithoutPorts(t *testing.T) {
	stream := streamFrom(t, `{"finalmask":{"quicParams":{"udpHop":{"interval":"30"}}}}`)
	if !MoveUDPHopToMask(stream) {
		t.Fatal("the stale udpHop was not reported as removed")
	}
	if len(stream) != 0 {
		t.Fatalf("expected nothing left, got %v", stream)
	}
}
