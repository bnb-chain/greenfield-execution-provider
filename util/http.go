package util

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const DefaultHttpTimeOut int = 5 // s

var (
	dialer = &net.Dialer{
		Timeout:   1 * time.Second,
		KeepAlive: 60 * time.Second,
	}
	transport = &http.Transport{
		DialContext:         dialer.DialContext,
		MaxConnsPerHost:     1000,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     10 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	}

	HttpClient = &http.Client{
		Timeout:   time.Second * time.Duration(DefaultHttpTimeOut),
		Transport: transport,
	}
)
