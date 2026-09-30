package client

import (
	"encoding/json"
	"net/http"
	"net/url"
)

type ExecutionResolution struct {
	Fence         int64  `json:"fence"`
	Key           string `json:"idempotencyKey"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	EffectStopped bool   `json:"effectStopped"`
}
type EffectResolution struct {
	Generation            int    `json:"generation"`
	Key                   string `json:"idempotencyKey"`
	Decision              string `json:"decision"`
	Reason                string `json:"reason"`
	Classification        string `json:"classification"`
	EffectStopped         bool   `json:"effectStopped"`
	DuplicateRiskAccepted bool   `json:"duplicateRiskAccepted"`
}

func (c *Client) Executions(instance string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(http.MethodGet, "/api/instances/"+url.PathEscape(instance)+"/executions", nil, &out)
	return out, err
}
func (c *Client) ResolveExecution(id string, b ExecutionResolution) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(http.MethodPost, "/api/executions/"+url.PathEscape(id)+"/resolve", b, &out)
	return out, err
}
func (c *Client) ResolveEffect(id string, b EffectResolution) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(http.MethodPost, "/api/execution-effects/"+url.PathEscape(id)+"/resolve", b, &out)
	return out, err
}
