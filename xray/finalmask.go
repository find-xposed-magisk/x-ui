package xray

// Xray-core v26.9.30 dropped "finalmask.quicParams.udpHop" in favour of a
// client-only "udphop" UDP mask. The panel still keeps the port range in the
// inbound's quicParams, because share links and subscriptions are generated
// from it, but the core must only ever see the mask, and only on the client.

// TakeUDPHop removes quicParams.udpHop from a stream settings map and returns
// it, dropping quicParams entirely once nothing else is left in it.
func TakeUDPHop(stream map[string]any) map[string]any {
	finalmask, _ := stream["finalmask"].(map[string]any)
	quicParams, _ := finalmask["quicParams"].(map[string]any)
	udpHop, ok := quicParams["udpHop"].(map[string]any)
	if !ok {
		return nil
	}
	delete(quicParams, "udpHop")
	if len(quicParams) == 0 {
		delete(finalmask, "quicParams")
	}
	if len(finalmask) == 0 {
		delete(stream, "finalmask")
	}
	return udpHop
}

// MoveUDPHopToMask rewrites quicParams.udpHop of a client stream into the
// "udphop" UDP mask, which reproduces the old hopping: a fresh local socket to
// another server port on every interval. The mask goes first, since the core
// rejects a mask that opens its own sockets anywhere else.
func MoveUDPHopToMask(stream map[string]any) bool {
	udpHop := TakeUDPHop(stream)
	ports, ok := udpHop["ports"]
	if !ok || ports == nil || ports == "" {
		return udpHop != nil
	}

	settings := map[string]any{
		"mode":        "intervalLocal,intervalRemote",
		"remotePorts": ports,
	}
	if interval, ok := udpHop["interval"]; ok && interval != nil && interval != "" {
		settings["interval"] = interval
	}

	finalmask, _ := stream["finalmask"].(map[string]any)
	if finalmask == nil {
		finalmask = map[string]any{}
		stream["finalmask"] = finalmask
	}
	udp, _ := finalmask["udp"].([]any)
	for _, mask := range udp {
		if m, _ := mask.(map[string]any); m["type"] == "udphop" {
			return true
		}
	}
	finalmask["udp"] = append([]any{map[string]any{"type": "udphop", "settings": settings}}, udp...)
	return true
}
