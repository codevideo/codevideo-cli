package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestCLISendsOnlyWithAWebhookFlag(t *testing.T) {
	var mu sync.Mutex
	var got []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Text string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		got = append(got, body.Text)
		mu.Unlock()
	}))
	defer hook.Close()
	t.Cleanup(func() { Configure(false, "") })

	// A source checkout's .env sets this; without the flag the CLI ignores it.
	t.Setenv("CODEVIDEO_SLACK_WEBHOOK_URL", hook.URL)
	Configure(false, "")
	if Enabled() {
		t.Fatal("enabled without --slack-webhook-url")
	}
	Send("dropped")
	Queue("dropped too")

	Configure(false, hook.URL)
	for _, message := range []string{"[  0%] Narration", "[ 25%] Recording", "[100%] Done"} {
		Queue(message)
	}
	Flush(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || got[0] != "[  0%] Narration" || got[2] != "[100%] Done" {
		t.Fatalf("webhook got %q", got)
	}
}

func TestFlushGivesUpOnASlowWebhook(t *testing.T) {
	release := make(chan struct{})
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer hook.Close()
	defer close(release)
	t.Cleanup(func() { Configure(false, "") })

	Configure(false, hook.URL)
	Queue("slow")
	started := time.Now()
	Flush(100 * time.Millisecond)
	if waited := time.Since(started); waited > time.Second {
		t.Fatalf("Flush waited %v", waited)
	}
}
