package server

import (
	"bytes"
	"log"
	"net/http"
	"time"
)

// notifier sends push notifications to an ntfy topic URL.
// When url is empty all operations are no-ops.
type notifier struct {
	url    string
	client *http.Client
}

func newNotifier(url string) *notifier {
	return &notifier{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// send fires a notification asynchronously. Safe to call even when url is empty.
func (n *notifier) send(title, message string) {
	if n.url == "" {
		return
	}
	go func() {
		req, err := http.NewRequest(http.MethodPost, n.url, bytes.NewBufferString(message))
		if err != nil {
			log.Printf("notify: build request: %v", err)
			return
		}
		req.Header.Set("Title", title)
		req.Header.Set("Priority", "high")
		req.Header.Set("Tags", "warning")
		resp, err := n.client.Do(req)
		if err != nil {
			log.Printf("notify: send: %v", err)
			return
		}
		resp.Body.Close()
	}()
}
