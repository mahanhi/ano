package via

import "testing"

func TestStreamingLeavesLeakNamesAlone(t *testing.T) {
	if !Streaming("video.googlevideo.com") || !Streaming("foo.aiv-cdn.net") || !Streaming("GOOGLEVIDEO.COM") {
		t.Fatal("streaming names were missed")
	}
	for _, host := range []string{
		"device-metrics-us.amazon.com",
		"unagi-na.amazon.com",
		"amazon-adsystem.com",
		"api.amazon.com",
		"amazon.com",
	} {
		if Streaming(host) {
			t.Fatalf("%s was treated as streaming", host)
		}
	}
}

func TestNormalizeStripsPort(t *testing.T) {
	host, ok := Normalize("Device-Metrics-US.amazon.com:443")
	if !ok || host != "device-metrics-us.amazon.com" {
		t.Fatalf("host %q ok %v", host, ok)
	}
	if _, ok := Normalize("not a host"); ok {
		t.Fatal("accepted a broken host")
	}
}
