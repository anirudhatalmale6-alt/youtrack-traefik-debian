// Minimal lego "exec" DNS provider hook: sets/clears the DNS-01 TXT record
// in pebble-challtestsrv via its management API. Statically compiled so it
// runs inside Traefik's scratch-based image (no shell needed).
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
)

func main() {
	// argv: <present|cleanup> <fqdn> <txtvalue>
	if len(os.Args) < 3 {
		os.Exit(2)
	}
	action, fqdn := os.Args[1], os.Args[2]
	base := os.Getenv("CHALLTESTSRV_URL") // e.g. http://10.89.0.2:8055
	var url string
	var body []byte
	switch action {
	case "present":
		if len(os.Args) < 4 {
			os.Exit(2)
		}
		url = base + "/set-txt"
		body, _ = json.Marshal(map[string]string{"host": fqdn, "value": os.Args[3]})
	case "cleanup":
		url = base + "/clear-txt"
		body, _ = json.Marshal(map[string]string{"host": fqdn})
	default:
		os.Exit(0)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		os.Stderr.WriteString("challtestsrv status " + resp.Status)
		os.Exit(1)
	}
}
