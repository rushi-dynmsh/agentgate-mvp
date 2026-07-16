// Package slack posts approval requests to a Slack channel (interactive
// Approve/Deny buttons) and verifies the signatures on Slack's callbacks.
//
// Configured entirely by environment variables; when SLACK_BOT_TOKEN is
// unset, AgentGate falls back to the local approval UI only.
package slack

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Client posts messages to one channel with one bot token.
type Client struct {
	BotToken      string
	Channel       string
	SigningSecret string
}

// Enabled reports whether Slack notifications are configured.
func (c *Client) Enabled() bool { return c != nil && c.BotToken != "" && c.Channel != "" }

// PostApprovalRequest sends an interactive message describing the parked
// call, with Approve / Deny buttons whose value carries the transaction id.
func (c *Client) PostApprovalRequest(txID, tool, onBehalfOf, agentID string, args map[string]any) error {
	argsJSON, _ := json.Marshal(args)
	payload := map[string]any{
		"channel": c.Channel,
		"text":    fmt.Sprintf("Approval needed: %s wants to run %s", onBehalfOf, tool),
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf(
						":lock: *Approval needed*\n*Tool:* `%s`\n*Arguments:* `%s`\n*On behalf of:* %s\n*Agent:* `%s`\n*Transaction:* `%s`",
						tool, argsJSON, onBehalfOf, agentID, txID),
				},
			},
			{
				"type": "actions",
				"elements": []map[string]any{
					{
						"type":      "button",
						"style":     "primary",
						"action_id": "approve",
						"text":      map[string]any{"type": "plain_text", "text": "Approve"},
						"value":     txID,
					},
					{
						"type":      "button",
						"style":     "danger",
						"action_id": "deny",
						"text":      map[string]any{"type": "plain_text", "text": "Deny"},
						"value":     txID,
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.BotToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("slack API: %s", out.Error)
	}
	return nil
}

// VerifySignature checks Slack's request signature (v0 HMAC-SHA256 scheme)
// so nobody can forge approval clicks by POSTing to our callback endpoint.
func (c *Client) VerifySignature(timestamp, signature string, body []byte) error {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("bad timestamp")
	}
	// Reject replays of old captured requests.
	if d := time.Since(time.Unix(ts, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return fmt.Errorf("timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(c.SigningSecret))
	fmt.Fprintf(mac, "v0:%s:%s", timestamp, body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// InteractionPayload is the subset of Slack's interactivity callback we use.
type InteractionPayload struct {
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	ResponseURL string `json:"response_url"`
}

// Respond replaces the original Slack message (via response_url) with the
// outcome text, so the channel shows what happened to the request.
func Respond(responseURL, text string) {
	body, _ := json.Marshal(map[string]any{
		"replace_original": true,
		"text":             text,
	})
	http.Post(responseURL, "application/json", bytes.NewReader(body)) //nolint:errcheck
}
