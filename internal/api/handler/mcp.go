package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// RegisterManagementMCP serves the proxy's own tools alongside the upstream MCP gateway.
func RegisterManagementMCP(r chi.Router, store storage.Storage, getCfg func() *config.Config, provider *auth.OIDCProvider) {
	reports, ok := store.(storage.UsageStorage)
	if !ok {
		return
	}
	cfg := getCfg()
	management, local := cfg.MCP.Management, cfg.LocalAdmin.Enabled
	var oauth *auth.MCPOAuth
	if provider != nil && management.OAuth {
		oauth = provider.MCP
	}
	if !local && oauth == nil && management.ReadTokenHash == [sha256.Size]byte{} && management.WriteTokenHash == [sha256.Size]byte{} {
		return
	}
	origin := ""
	challenge := "Bearer"
	if oauth != nil {
		origin = oauth.Origin
		challenge = fmt.Sprintf("Bearer resource_metadata=%q, scope=%q", origin+auth.MCPMetadataPath, auth.MCPReadScope)
		r.Get(auth.MCPMetadataPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(oauth.Metadata())
		})
	} else if callback, err := url.Parse(cfg.OIDCSettings.RedirectURL); err == nil && callback.Host != "" {
		origin = callback.Scheme + "://" + callback.Host
	}
	reader := managementMCP(store, reports, getCfg, false, oauth != nil)
	writer := managementMCP(store, reports, getCfg, true, oauth != nil)
	r.HandleFunc(auth.MCPManagementPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origins := r.Header.Values("Origin")
		if local {
			if !middleware.IsLocalAdminRequest(r) {
				model.WriteError(w, model.ErrForbidden("invalid loopback peer, host, or origin"))
				return
			}
			if len(r.Header.Values("Authorization")) == 0 {
				writer.ServeHTTP(w, r)
				return
			}
		} else if len(origins) > 0 && (len(origins) != 1 || origin == "" || origins[0] != origin) {
			model.WriteError(w, model.ErrForbidden("invalid origin"))
			return
		}

		headers := r.Header.Values("Authorization")
		if len(headers) == 1 {
			scheme, token, _ := strings.Cut(headers[0], " ")
			if strings.EqualFold(scheme, "Bearer") && token != "" {
				digest := sha256.Sum256([]byte(token))
				if management.WriteTokenHash != [sha256.Size]byte{} && subtle.ConstantTimeCompare(digest[:], management.WriteTokenHash[:]) == 1 {
					writer.ServeHTTP(w, r)
					return
				}
				if management.ReadTokenHash != [sha256.Size]byte{} && subtle.ConstantTimeCompare(digest[:], management.ReadTokenHash[:]) == 1 {
					reader.ServeHTTP(w, r)
					return
				}
				if oauth != nil {
					if scopes, valid := oauth.Scopes(r.Context(), token); valid {
						switch {
						case !slices.Contains(scopes, auth.MCPReadScope):
							w.Header().Set("WWW-Authenticate", challenge+`, error="insufficient_scope"`)
							model.WriteError(w, model.ErrForbidden("OAuth access token lacks "+auth.MCPReadScope))
						case slices.Contains(scopes, auth.MCPWriteScope):
							writer.ServeHTTP(w, r)
						default:
							reader.ServeHTTP(w, r)
						}
						return
					}
				}
			}
		}
		if len(headers) > 0 {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
		} else {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		model.WriteError(w, model.ErrUnauthorized("MCP bearer token required"))
	})
}

type mcpUsageInput struct {
	From         string `json:"from,omitempty" jsonschema:"Inclusive YYYY-MM-DD date. Defaults to six days before today."`
	To           string `json:"to,omitempty" jsonschema:"Inclusive YYYY-MM-DD date. Defaults to today."`
	Since        string `json:"since,omitempty" jsonschema:"Inclusive RFC3339 timestamp. Use with until to override from and to."`
	Until        string `json:"until,omitempty" jsonschema:"Exclusive RFC3339 timestamp. Use with since."`
	Timezone     string `json:"timezone,omitempty" jsonschema:"IANA timezone for date bounds and buckets. Defaults to UTC."`
	Interval     string `json:"interval,omitempty" jsonschema:"Bucket size: hour, day (default), or week."`
	GroupBy      string `json:"group_by,omitempty" jsonschema:"Dimension: model (default), team, app, key, or status."`
	Model        string `json:"model,omitempty" jsonschema:"Exact proxy model alias."`
	TeamID       *int64 `json:"team_id,omitempty" jsonschema:"Filter by positive team ID from list_teams."`
	AppID        *int64 `json:"app_id,omitempty" jsonschema:"Filter by positive application ID from list_applications."`
	KeyID        *int64 `json:"key_id,omitempty" jsonschema:"Filter by positive key ID from list_keys."`
	StatusCode   *int   `json:"status_code,omitempty" jsonschema:"Exact HTTP status code, 100–599."`
	Unattributed string `json:"unattributed,omitempty" jsonschema:"Match traffic missing team, app, or key attribution."`
}

type mcpCreateKeyInput struct {
	AppID          int64    `json:"app_id" jsonschema:"Application ID from list_applications."`
	Name           string   `json:"name" jsonschema:"Name to identify the key."`
	AllowedModels  []string `json:"allowed_models,omitempty" jsonschema:"Allowed proxy model aliases. Empty allows all aliases."`
	MaxRPM         *int     `json:"max_rpm,omitempty" jsonschema:"Maximum requests per minute."`
	MaxRPD         *int     `json:"max_rpd,omitempty" jsonschema:"Maximum requests per day."`
	MaxBudget      *float64 `json:"max_budget,omitempty" jsonschema:"Hard budget in USD."`
	SoftBudget     *float64 `json:"soft_budget,omitempty" jsonschema:"Budget alert threshold in USD."`
	ExpiresAt      string   `json:"expires_at,omitempty" jsonschema:"Optional future RFC3339 expiration timestamp."`
	AllowDecisions *bool    `json:"allow_decisions,omitempty" jsonschema:"Whether this key may use decision models."`
}

func managementMCP(store storage.Storage, reports storage.UsageStorage, getCfg func() *config.Config, write, oauth bool) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "simple-llm-proxy", Version: "1.0.0"}, nil)
	addManagementTool(server, oauth, true, "query_usage",
		"Query retained calls, failures, tokens and spend over time. Filter by model, team, application, key or HTTP status. Group by one dimension. Each subtotal includes its measurement coverage.",
		func(ctx context.Context, input mcpUsageInput) (usageReport, error) {
			params := url.Values{
				"from": {input.From}, "to": {input.To}, "since": {input.Since}, "until": {input.Until},
				"timezone": {input.Timezone}, "interval": {input.Interval}, "group_by": {input.GroupBy},
				"model": {input.Model}, "unattributed": {input.Unattributed},
			}
			for _, filter := range []struct {
				name string
				id   *int64
			}{{"team_id", input.TeamID}, {"app_id", input.AppID}, {"key_id", input.KeyID}} {
				if filter.id != nil {
					params.Set(filter.name, strconv.FormatInt(*filter.id, 10))
				}
			}
			if input.StatusCode != nil {
				params.Set("status_code", strconv.Itoa(*input.StatusCode))
			}
			query, tz, interval, err := parseUsageQuery(params, time.Now())
			if err != nil {
				return usageReport{}, err
			}
			query.MaxRows = 501
			report, err := loadUsageReport(ctx, reports, query, tz, interval, getCfg)
			if err != nil && err != errUsageTooManyRows {
				return usageReport{}, fmt.Errorf("failed to load usage report")
			}
			return report, err
		})
	addManagementTool(server, oauth, true, "list_teams", "List teams and their IDs.",
		func(ctx context.Context, _ struct{}) (map[string][]*storage.Team, error) {
			teams, err := store.ListTeams(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list teams")
			}
			return map[string][]*storage.Team{"teams": teams}, nil
		})
	addManagementTool(server, oauth, true, "list_applications", "List applications for a team.",
		func(ctx context.Context, input struct {
			TeamID int64 `json:"team_id" jsonschema:"Team ID from list_teams."`
		}) (map[string][]*storage.Application, error) {
			if input.TeamID <= 0 {
				return nil, fmt.Errorf("team_id must be positive")
			}
			apps, err := store.ListApplications(ctx, input.TeamID)
			if err != nil {
				return nil, fmt.Errorf("failed to list applications")
			}
			return map[string][]*storage.Application{"applications": apps}, nil
		})
	addManagementTool(server, oauth, true, "list_keys", "List app key IDs, prefixes, limits, expirations and allowed models.",
		func(ctx context.Context, input struct {
			AppID int64 `json:"app_id" jsonschema:"Application ID from list_applications."`
		}) (map[string][]keyListItem, error) {
			if input.AppID <= 0 {
				return nil, fmt.Errorf("app_id must be positive")
			}
			keys, err := listAppKeys(ctx, store, input.AppID)
			if err != nil {
				return nil, err
			}
			return map[string][]keyListItem{"keys": keys}, nil
		})
	if write {
		addManagementTool(server, oauth, false, "create_key",
			"Create an application key with optional model, rate, budget and expiration limits. The result contains the full secret once.",
			func(ctx context.Context, input mcpCreateKeyInput) (createdAppKey, error) {
				return createMCPKey(ctx, store, input)
			})
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 64 << 10,
		// The route checks local peers and Hosts and accepts authenticated requests through a reverse proxy.
		DisableLocalhostProtection: true,
	})
}

func addManagementTool[In, Out any](server *mcp.Server, oauth, readOnly bool, name, description string, run func(context.Context, In) (Out, error)) {
	closedWorld := false
	tool := &mcp.Tool{
		Name: name, Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: readOnly, OpenWorldHint: &closedWorld},
	}
	if !readOnly {
		tool.Annotations.DestructiveHint = &closedWorld
	}
	if oauth {
		scopes := []string{auth.MCPReadScope}
		if !readOnly {
			scopes = append(scopes, auth.MCPWriteScope)
		}
		tool.Meta = mcp.Meta{"securitySchemes": []map[string]any{{"type": "oauth2", "scopes": scopes}}}
	}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result, err := run(ctx, input)
		if ctx.Err() == context.DeadlineExceeded && err != nil {
			return nil, nil, fmt.Errorf("tool timed out")
		}
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode tool result")
		}
		if readOnly && len(raw) > 256<<10 {
			return nil, nil, fmt.Errorf("tool result exceeds size limit")
		}
		return &mcp.CallToolResult{
			StructuredContent: json.RawMessage(raw),
			Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		}, nil, nil
	})
}

func createMCPKey(ctx context.Context, store storage.Storage, input mcpCreateKeyInput) (createdAppKey, error) {
	if input.AppID <= 0 || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 {
		return createdAppKey{}, fmt.Errorf("a positive app_id and a nonblank name of at most 512 bytes are required")
	}
	if input.MaxRPM != nil && *input.MaxRPM <= 0 || input.MaxRPD != nil && *input.MaxRPD <= 0 ||
		input.MaxBudget != nil && *input.MaxBudget <= 0 || input.SoftBudget != nil && *input.SoftBudget <= 0 {
		return createdAppKey{}, fmt.Errorf("rate and budget limits must be positive")
	}
	body := keySettings{
		Name: input.Name, AllowDecisions: input.AllowDecisions, AllowedModels: input.AllowedModels,
		MaxRPM: input.MaxRPM, MaxRPD: input.MaxRPD, MaxBudget: input.MaxBudget, SoftBudget: input.SoftBudget,
	}
	if input.ExpiresAt != "" {
		body.ExpiresAt, _ = json.Marshal(input.ExpiresAt)
	}
	opts, validationError := body.options()
	if validationError != nil {
		return createdAppKey{}, validationError
	}
	key, token, err := createAppKey(ctx, store, input.AppID, body, opts)
	if err != nil {
		return createdAppKey{}, fmt.Errorf("failed to create key, check the application ID and key settings")
	}
	return createdAppKeyResponse(key, token, input.AllowedModels), nil
}
