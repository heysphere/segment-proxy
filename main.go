package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gorilla/handlers"
)

const mirrorHostEnv string = "SEGMENT_MIRROR_HOST_ENV"
const segmentCDNHost string = "http://cdn.segment.com"
const segmentTrackingAPIHost string = "http://api.segment.io"

func parseURL(hostName string) *url.URL {
	targetURL, err := url.Parse(hostName)
	if err != nil {
		log.Fatalf("Failed to parse url %s: %v", hostName, err)
	}

	return targetURL
}

// singleJoiningSlash is copied from httputil.singleJoiningSlash method.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func isCDNURL(url *url.URL) bool {
	urlStr := url.String()
	return strings.HasPrefix(urlStr, "/v1/projects") ||
		strings.HasPrefix(urlStr, "/analytics.js/v1")
}

func isAttributionURL(url *url.URL) bool {
	return strings.HasPrefix(url.String(), "/v1/attribution")
}

func copyRequestTo(client *http.Client, url *url.URL, req *http.Request) error {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return err
	}

	req.Body = ioutil.NopCloser(bytes.NewReader(body))
	destURI := fmt.Sprintf("%s%s", url.String(), req.RequestURI)

	proxyReq, err := http.NewRequest(req.Method, destURI, bytes.NewReader(body))
	if err != nil {
		return err
	}

	proxyReq.Header = req.Header
	resp, err := client.Do(proxyReq)
	if err != nil {
		return err
	}

	resp.Body.Close()
	return nil
}

// NewSegmentReverseProxy is adapted from the httputil.NewSingleHostReverseProxy
// method, modified to dynamically redirect to different servers (CDN or Tracking API)
// based on the incoming request, and sets the host of the request to the host of of
// the destination URL.
func NewSegmentReverseProxy(
	client *http.Client,
	cdn *url.URL,
	trackingAPI *url.URL,
	mirrorHostURL *url.URL,
) http.Handler {
	director := func(req *http.Request) {
		// Figure out which server to redirect to based on the incoming request.
		var target *url.URL
		if isCDNURL(req.URL) {
			target = cdn
		} else {
			target = trackingAPI
			if isAttributionURL(req.URL) {
				log.Printf(
					"[INFO]: Got an attribution request: [%s]\n",
					req.URL.String(),
				)
			}
		}

		targetQuery := target.RawQuery
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}

		// Set the host of the request to the host of of the destination URL.
		// See http://blog.semanticart.com/blog/2013/11/11/a-proper-api-proxy-written-in-go/.
		req.Host = req.URL.Host
		if mirrorHostURL != nil {
			err := copyRequestTo(client, mirrorHostURL, req)
			if err != nil {
				log.Printf(
					"ERROR: Failed to mirror request to %s: %v\n",
					mirrorHostURL.String(), err,
				)
			}
		}
	}

	return &httputil.ReverseProxy{Director: director}
}

var port = flag.String("port", "8080", "bind address")
var debug = flag.Bool("debug", false, "debug mode")

func main() {
	flag.Parse()

	mirrorHost := os.Getenv(mirrorHostEnv)
	var mirrorHostURL *url.URL
	if mirrorHost == "" {
		log.Printf("[WARNING]: %s ENV is not set!\n", mirrorHostEnv)
		mirrorHostURL = nil
	} else {
		mirrorHostURL = parseURL(mirrorHost)
	}

	proxy := NewSegmentReverseProxy(
		&http.Client{},
		parseURL(segmentCDNHost),
		parseURL(segmentTrackingAPIHost),
		mirrorHostURL,
	)
	if *debug {
		proxy = handlers.LoggingHandler(os.Stdout, proxy)
		log.Printf("serving proxy at port %v\n", *port)
	}

	// TODO: https?
	log.Fatal(http.ListenAndServe(":"+*port, proxy))
}
