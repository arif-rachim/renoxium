package monitor

import (
	"net/http"
	"net/http/httptest"
)

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
func newRequest(url string) *http.Request     { return httptest.NewRequest("GET", url, nil) }
