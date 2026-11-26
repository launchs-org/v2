package exportlog

import (
	"strings"
	"testing"
	"time"
)

// TestEncodeNDJSON は、1 行に 1 つの JSON になり、& が読める形のままで、行が無ければ空になることを確かめる。
func TestEncodeNDJSON(testingT *testing.T) {
	lines := []Line{
		{ID: 1, IngressID: "ingress-1", Host: "a.example.com", Time: FormatTime(time.Date(2026, 11, 26, 1, 2, 3, 456000000, time.UTC)), Method: "GET", URL: "/a?b=1&c=2", Status: 200, DurationMs: 1.5, ClientIP: "203.0.113.9", Bytes: 10},
		{ID: 2, IngressID: "ingress-1", Host: "a.example.com", Time: "2026-11-26T01:02:04.000Z", Method: "POST", URL: "/b", Status: 500},
	}
	encoded := string(EncodeNDJSON(lines))
	rows := strings.Split(strings.TrimSuffix(encoded, "\n"), "\n")
	if len(rows) != 2 || !strings.HasSuffix(encoded, "\n") {
		testingT.Fatalf("2 行で、最後に改行があるはず: %q", encoded)
	}
	if !strings.Contains(rows[0], `"time":"2026-11-26T01:02:03.456Z"`) || !strings.Contains(rows[0], `"url":"/a?b=1&c=2"`) {
		testingT.Errorf("時刻と URL が読める形のはず: %s", rows[0])
	}
	if len(EncodeNDJSON(nil)) != 0 {
		testingT.Errorf("行が無ければ空のはず")
	}
}
