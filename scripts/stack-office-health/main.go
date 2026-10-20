// Static discovery probe for Collabora's distroless runtime image.
package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://127.0.0.1:9980/api/v1/collabora/hosting/discovery")
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("<wopi-discovery")) {
		os.Exit(1)
	}
}
