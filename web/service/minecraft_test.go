package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeMojang(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/Notch":
			w.Write([]byte(`{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch"}`))
		case r.URL.Path == "/session/069a79f444e94726a5befca90e38aaf5" && r.URL.Query().Get("unsigned") == "false":
			w.Write([]byte(`{"name":"Notch","properties":[{"name":"textures","value":"dmFsdWU=","signature":"c2ln"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	oldProfile, oldSession := mojangProfileURL, mojangSessionURL
	mojangProfileURL, mojangSessionURL = server.URL+"/users/", server.URL+"/session/"
	t.Cleanup(func() { mojangProfileURL, mojangSessionURL = oldProfile, oldSession })
}

func TestGetMinecraftProfile(t *testing.T) {
	fakeMojang(t)
	profile, err := (&ServerService{}).GetMinecraftProfile("Notch")
	if err != nil {
		t.Fatal(err)
	}
	want := MinecraftProfile{
		Username: "Notch", UUID: "069a79f4-44e9-4726-a5be-fca90e38aaf5",
		TexturesValue: "dmFsdWU=", TexturesSignature: "c2ln",
	}
	if *profile != want {
		t.Fatalf("got %+v, want %+v", *profile, want)
	}
}

func TestGetMinecraftProfileRejects(t *testing.T) {
	fakeMojang(t)
	for username, wantErr := range map[string]string{
		"ab":           "invalid Minecraft username",
		"bad name":     "invalid Minecraft username",
		"nobody_there": "no such Minecraft account",
	} {
		if _, err := (&ServerService{}).GetMinecraftProfile(username); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: error %v, want %q", username, err, wantErr)
		}
	}
}
