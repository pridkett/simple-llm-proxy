package router

import (
	"net/http"
	"strings"
)

const (
	HeaderProviderUsed    = "X-Provider-Used"
	HeaderProviderURLUsed = "X-Provider-URL-Used"
	HeaderProvidersTried  = "X-Providers-Tried"
	HeaderFailoverReason  = "X-Failover-Reason"
)

// SetRouteHeaders sets response headers from RouteResult metadata.
// Must be called BEFORE writing any response body (before first SSE chunk for streaming).
func SetRouteHeaders(w http.ResponseWriter, result *RouteResult) {
	if result == nil || result.DeploymentUsed == nil {
		return
	}

	d := result.DeploymentUsed

	// X-Provider-Used: "provider/model" format (D-13)
	w.Header().Set(HeaderProviderUsed, d.ProviderName+"/"+d.ActualModel)

	// X-Provider-URL-Used: resolved base URL (D-14)
	url := d.APIBase
	if url == "" {
		url = defaultBaseURL(d.ProviderName)
	}
	w.Header().Set(HeaderProviderURLUsed, url)

	// X-Providers-Tried: all deployments attempted including successful one (D-15)
	// DeploymentsTried always includes the successful deployment, so this is always present on success.
	if len(result.DeploymentsTried) > 0 {
		tried := make([]string, len(result.DeploymentsTried))
		for i, t := range result.DeploymentsTried {
			tried[i] = t.ProviderName + "/" + t.ActualModel
		}
		w.Header().Set(HeaderProvidersTried, strings.Join(tried, ", "))
	}

	// X-Failover-Reason: only present when failover occurred (D-16)
	if len(result.FailoverReasons) > 0 {
		reasons := make([]string, len(result.FailoverReasons))
		for i, r := range result.FailoverReasons {
			reasons[i] = string(r)
		}
		w.Header().Set(HeaderFailoverReason, strings.Join(reasons, ", "))
	}
}

func defaultBaseURL(providerName string) string {
	switch providerName {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com"
	default:
		return ""
	}
}
