package provider

import (
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
)

// Do retains accounting headers even when the provider response is an error.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if resp != nil {
		accounting.ObserveHeaders(req.Context(), resp.Header)
	}
	return resp, err
}
