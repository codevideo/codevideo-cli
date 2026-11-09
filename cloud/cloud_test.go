package cloud

import (
	"context"
	"strings"
	"testing"
)

func TestConfigFromEnvDefaultsToCodeVideosBucket(t *testing.T) {
	t.Setenv("CODEVIDEO_S3_KEY_ID", "key")
	t.Setenv("CODEVIDEO_S3_SECRET", "secret")
	t.Setenv("CODEVIDEO_S3_BUCKET", "")
	t.Setenv("CODEVIDEO_S3_REGION", "")

	c := ConfigFromEnv()
	if c.Bucket != "fullstackcraft" || c.Region != "us-east-1" {
		t.Fatalf("bucket, region = %q, %q; want fullstackcraft, us-east-1", c.Bucket, c.Region)
	}
	// The hosted worker only sets the keys, so its URLs must stay exactly as before.
	want := "https://fullstackcraft.s3.us-east-1.amazonaws.com/codevideo/v3/audio/a.mp3"
	if got := c.URL(Key("v3/audio", "a.mp3")); got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestConfigFromEnvUsesYourOwnBucket(t *testing.T) {
	t.Setenv("CODEVIDEO_S3_KEY_ID", "key")
	t.Setenv("CODEVIDEO_S3_SECRET", "secret")
	t.Setenv("CODEVIDEO_S3_BUCKET", " my-course-audio ")
	t.Setenv("CODEVIDEO_S3_REGION", "eu-central-1")

	c := ConfigFromEnv()
	want := "https://my-course-audio.s3.eu-central-1.amazonaws.com/codevideo/v3/audio/a.mp3"
	if got := c.URL(Key("v3/audio", "a.mp3")); got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestDottedBucketGetsAPathStyleURL(t *testing.T) {
	c := Config{Bucket: "audio.example.com", Region: "us-west-2"}
	want := "https://s3.us-west-2.amazonaws.com/audio.example.com/codevideo/v3/audio/a.mp3"
	if got := c.URL(Key("v3/audio", "a.mp3")); got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestEnabledNeedsBothKeys(t *testing.T) {
	for _, c := range []Config{{}, {KeyID: "key"}, {Secret: "secret"}} {
		if c.Enabled() {
			t.Fatalf("%+v reports enabled", c)
		}
	}
	if !(Config{KeyID: "key", Secret: "secret"}).Enabled() {
		t.Fatal("both keys set but not enabled")
	}
}

func TestUploadWithoutKeysFailsBeforeAnyRequest(t *testing.T) {
	_, err := Config{Bucket: "b", Region: "r"}.Upload(context.Background(), []byte("x"), "v3/audio", "a.mp3")
	if err == nil || !strings.Contains(err.Error(), "credentials are not set") {
		t.Fatalf("err = %v, want missing credentials", err)
	}
}
