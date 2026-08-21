package sub

import "github.com/alireza0/x-ui/util/random"

// The link builders read Xray configuration that was stored as JSON and decoded
// into interface{} trees. Fields are routinely absent - a WebSocket inbound with
// no explicit path is perfectly valid, and so is an empty streamSettings - so
// reading them with a bare type assertion panics on configurations Xray itself
// accepts. Because a subscription is built from every inbound a client belongs
// to, one such inbound would fail the whole subscription rather than just its
// own link. These helpers read the same values and fall back to the zero value.
//
// On a well-formed configuration they return exactly what the assertion did.

func asMap(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	return m
}

func asSlice(value interface{}) []interface{} {
	s, _ := value.([]interface{})
	return s
}

func asString(value interface{}) string {
	s, _ := value.(string)
	return s
}

func asBool(value interface{}) bool {
	b, _ := value.(bool)
	return b
}

func asFloat(value interface{}) float64 {
	f, _ := value.(float64)
	return f
}

// valueAt looks a key up in a container that may not be a map at all.
func valueAt(container interface{}, key string) interface{} {
	m, ok := container.(map[string]interface{})
	if !ok {
		return nil
	}
	return m[key]
}

func stringAt(container interface{}, key string) string {
	return asString(valueAt(container, key))
}

func boolAt(container interface{}, key string) bool {
	return asBool(valueAt(container, key))
}

func floatAt(container interface{}, key string) float64 {
	return asFloat(valueAt(container, key))
}

func mapAt(container interface{}, key string) map[string]interface{} {
	return asMap(valueAt(container, key))
}

func sliceAt(container interface{}, key string) []interface{} {
	return asSlice(valueAt(container, key))
}

// firstString returns the first entry of a list as a string, or "" for a list
// that is empty or holds something else.
func firstString(list []interface{}) string {
	if len(list) == 0 {
		return ""
	}
	return asString(list[0])
}

// randomString picks one entry at random, the way REALITY server names and
// short IDs are chosen. An empty list yields "" instead of an index panic.
func randomString(list []interface{}) string {
	if len(list) == 0 {
		return ""
	}
	return asString(list[random.Num(len(list))])
}
