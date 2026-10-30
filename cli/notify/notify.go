// Package notify sends Slack messages.
//
// The serve-mode worker keeps reporting through CODEVIDEO_SLACK_WEBHOOK_URL as
// before. The CLI sends nothing unless --slack-webhook-url is passed, so a
// source checkout's .env (loaded automatically) can't make local renders post.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	slack "github.com/codevideo/go-utils/slack"
	log "github.com/sirupsen/logrus"
)

var (
	mu         sync.Mutex
	serve      bool
	webhookURL string
	client     = &http.Client{Timeout: 10 * time.Second}

	queue      = make(chan string, 64)
	startQueue sync.Once
	queued     sync.WaitGroup
)

// Configure sets the mode and, for the CLI, the webhook from --slack-webhook-url.
func Configure(serveMode bool, url string) {
	mu.Lock()
	defer mu.Unlock()
	serve, webhookURL = serveMode, url
}

// Enabled reports whether messages go anywhere.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return serve || webhookURL != ""
}

// Send posts message to Slack. Failures are logged at debug level and never
// interrupt a render.
func Send(message string) {
	mu.Lock()
	serveMode, url := serve, webhookURL
	mu.Unlock()
	var err error
	switch {
	case serveMode:
		err = slack.SendSlackMessage(message)
	case url != "":
		err = post(url, message)
	default:
		return
	}
	if err != nil {
		log.Debugf("Slack message not sent: %v", err)
	}
}

// Queue sends message in the background, in order, so a slow webhook never
// holds up the progress bars. Flush waits for what's queued.
func Queue(message string) {
	if !Enabled() {
		return
	}
	startQueue.Do(func() {
		go func() {
			for m := range queue {
				Send(m)
				queued.Done()
			}
		}()
	})
	queued.Add(1)
	select {
	case queue <- message:
	default:
		queued.Done()
		log.Debugf("Slack queue is full; dropped: %s", message)
	}
}

// Flush waits up to timeout for queued messages to go out.
func Flush(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		queued.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func post(url, message string) error {
	payload, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return err
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s", resp.Status)
	}
	return nil
}
