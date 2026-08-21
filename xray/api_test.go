package xray

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// A payload built from a model.Client keeps its Go types; one read back out of
// an inbound's stored settings arrives as generic JSON values. Both reach
// AddUser, so wireguardPeerConfig has to understand both.
func wgKey(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed + byte(i)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestWireguardPeerConfigTypedPayload(t *testing.T) {
	peer, err := wireguardPeerConfig(map[string]interface{}{
		"email":        "typed@example.com",
		"publicKey":    wgKey(1),
		"preSharedKey": wgKey(2),
		"allowedIPs":   []string{"10.0.0.2/32"},
		"keepAlive":    uint32(25),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := peer.GetAllowedIps(); len(got) != 1 || got[0] != "10.0.0.2/32" {
		t.Fatalf("allowed IPs = %v, want [10.0.0.2/32]", got)
	}
	if got := peer.GetKeepAlive(); got != "25" {
		t.Fatalf("keepAlive = %q, want \"25\"", got)
	}
	if len(peer.GetPublicKey()) != 64 {
		t.Fatalf("public key %q was not converted to hex", peer.GetPublicKey())
	}
	if len(peer.GetPreSharedKey()) != 64 {
		t.Fatalf("pre-shared key %q was not converted to hex", peer.GetPreSharedKey())
	}
}

func TestWireguardPeerConfigJSONPayload(t *testing.T) {
	// Exactly what json.Unmarshal produces for a stored client entry.
	var decoded map[string]interface{}
	raw := `{"email":"json@example.com","publicKey":"` + wgKey(3) + `",
	         "allowedIPs":["10.0.0.3/32","fd00::3/128"],"keepAlive":30}`
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}

	peer, err := wireguardPeerConfig(decoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := peer.GetAllowedIps(); len(got) != 2 || got[0] != "10.0.0.3/32" || got[1] != "fd00::3/128" {
		t.Fatalf("allowed IPs = %v, want both entries preserved", got)
	}
	if got := peer.GetKeepAlive(); got != "30" {
		t.Fatalf("keepAlive = %q, want \"30\" (float64 30 must not read as 0)", got)
	}
}

func TestWireguardPeerConfigRejectsIncompletePeers(t *testing.T) {
	tests := []struct {
		name string
		user map[string]interface{}
	}{
		{"no public key", map[string]interface{}{"allowedIPs": []string{"10.0.0.2/32"}}},
		{"empty public key", map[string]interface{}{"publicKey": "", "allowedIPs": []string{"10.0.0.2/32"}}},
		{"no allowed IPs", map[string]interface{}{"publicKey": wgKey(4)}},
		{"empty allowed IPs", map[string]interface{}{"publicKey": wgKey(4), "allowedIPs": []interface{}{}}},
		{"blank allowed IPs", map[string]interface{}{"publicKey": wgKey(4), "allowedIPs": []interface{}{""}}},
		{"invalid public key", map[string]interface{}{"publicKey": "not a key", "allowedIPs": []string{"10.0.0.2/32"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := wireguardPeerConfig(test.user); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}
}

func TestWireguardPeerConfigOptionalFields(t *testing.T) {
	peer, err := wireguardPeerConfig(map[string]interface{}{
		"publicKey":  wgKey(5),
		"allowedIPs": []string{"10.0.0.5/32"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if peer.GetPreSharedKey() != "" {
		t.Fatalf("pre-shared key = %q, want empty", peer.GetPreSharedKey())
	}
	if peer.GetKeepAlive() != "" {
		t.Fatalf("keepAlive = %q, want empty", peer.GetKeepAlive())
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  []string
	}{
		{"typed slice", []string{"a", "b"}, []string{"a", "b"}},
		{"json slice", []interface{}{"a", "b"}, []string{"a", "b"}},
		{"json slice with blanks", []interface{}{"a", "", "b"}, []string{"a", "b"}},
		{"json slice with non-strings", []interface{}{"a", 7, nil}, []string{"a"}},
		{"nil", nil, nil},
		{"wrong type", "a,b", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := toStringSlice(test.value)
			if len(got) != len(test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("got %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestToUint32(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  uint32
	}{
		{"uint32", uint32(25), 25},
		{"int", 25, 25},
		{"int64", int64(25), 25},
		{"float64 from JSON", float64(25), 25},
		{"json.Number", json.Number("25"), 25},
		{"string", "25", 25},
		{"zero", float64(0), 0},
		{"negative", -5, 0},
		{"nil", nil, 0},
		{"unparsable string", "soon", 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := toUint32(test.value); got != test.want {
				t.Fatalf("toUint32(%v) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
