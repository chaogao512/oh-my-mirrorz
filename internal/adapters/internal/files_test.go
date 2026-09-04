package adapterinternal

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestVerifyEndpointFallsBackFromForbiddenHead(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != "oh-my-mirrorz/0.2" {
			t.Errorf("unexpected user agent %q", r.Header.Get("User-Agent"))
		}
		if r.Method == http.MethodHead {
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden")), Request: r}, nil
		}
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Errorf("missing range header")
		}
		return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("x")), Request: r}, nil
	})}
	if err := VerifyEndpoint(client, "https://example.com/resource"); err != nil {
		t.Fatal(err)
	}
}
