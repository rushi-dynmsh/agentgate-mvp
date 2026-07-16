package approval

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/agentgate/agentgate/internal/slack"
)

// HTTPServer exposes the human-facing side of the approval flow:
//
//	GET  /                  — tiny approval UI (list pending, Approve/Deny)
//	GET  /pending           — pending calls as JSON
//	GET  /status/{txID}     — state/result of one transaction as JSON
//	POST /decide            — local approver path (form: transaction_id, decision)
//	POST /slack/interactive — Slack interactivity callback (signed)
//
// Slack is the "real" write path when configured; the local UI exists so the
// whole flow works on a laptop with no public endpoint.
type HTTPServer struct {
	store    *Store
	executor *Executor
	slack    *slack.Client
}

func NewHTTPServer(store *Store, executor *Executor, slackClient *slack.Client) *HTTPServer {
	return &HTTPServer{store: store, executor: executor, slack: slackClient}
}

func (h *HTTPServer) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.ui)
	mux.HandleFunc("GET /pending", h.pendingJSON)
	mux.HandleFunc("GET /status/{tx}", h.status)
	mux.HandleFunc("POST /decide", h.decide)
	mux.HandleFunc("POST /slack/interactive", h.slackInteractive)
	log.Printf("approval UI listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (h *HTTPServer) pendingJSON(w http.ResponseWriter, r *http.Request) {
	pending, err := h.store.ListPending(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pending)
}

func (h *HTTPServer) status(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.Context(), r.PathValue("tx"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// decide is the local (non-Slack) approver path.
func (h *HTTPServer) decide(w http.ResponseWriter, r *http.Request) {
	txID := r.FormValue("transaction_id")
	decision := r.FormValue("decision")
	decidedBy := r.FormValue("decided_by")
	if decidedBy == "" {
		decidedBy = "local-approver"
	}
	var outcome string
	switch decision {
	case "approve":
		outcome = h.executor.Approve(r.Context(), txID, decidedBy)
	case "deny":
		outcome = h.executor.Deny(r.Context(), txID, decidedBy)
	default:
		http.Error(w, "decision must be approve or deny", 400)
		return
	}
	// Browser form → bounce back to the UI; API caller → plain text.
	if r.Header.Get("Accept") == "application/json" {
		json.NewEncoder(w).Encode(map[string]string{"outcome": outcome})
		return
	}
	http.Redirect(w, r, "/?msg="+url.QueryEscape(outcome), http.StatusSeeOther)
}

// slackInteractive receives button clicks from Slack. Slack sends the
// payload as a form field, signed with the app's signing secret — the
// signature covers the RAW body bytes, so buffer them before any parsing.
func (h *HTTPServer) slackInteractive(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", 400)
		return
	}
	if h.slack.SigningSecret != "" {
		if err := h.slack.VerifySignature(
			r.Header.Get("X-Slack-Request-Timestamp"),
			r.Header.Get("X-Slack-Signature"),
			raw,
		); err != nil {
			log.Printf("slack callback: signature rejected: %v", err)
			http.Error(w, "signature", 401)
			return
		}
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	var payload slack.InteractionPayload
	if err := json.Unmarshal([]byte(form.Get("payload")), &payload); err != nil {
		http.Error(w, "bad payload", 400)
		return
	}
	if len(payload.Actions) == 0 {
		w.WriteHeader(200)
		return
	}
	action := payload.Actions[0]
	who := "slack:" + payload.User.Username
	var outcome string
	switch action.ActionID {
	case "approve":
		outcome = h.executor.Approve(r.Context(), action.Value, who)
	case "deny":
		outcome = h.executor.Deny(r.Context(), action.Value, who)
	default:
		w.WriteHeader(200)
		return
	}
	log.Printf("slack decision: tx=%s action=%s by=%s → %s", action.Value, action.ActionID, who, outcome)
	if payload.ResponseURL != "" {
		go slack.Respond(payload.ResponseURL, fmt.Sprintf("Transaction `%s`: %s", action.Value, outcome))
	}
	w.WriteHeader(200)
}

// ui renders a minimal HTML approval queue — enough to demo the whole flow
// without Slack.
func (h *HTTPServer) ui(w http.ResponseWriter, r *http.Request) {
	pending, err := h.store.ListPending(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>AgentGate approvals</title>
<style>body{font-family:system-ui;margin:2rem;max-width:52rem}
table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:.5rem;text-align:left}
button{padding:.3rem .8rem;margin-right:.4rem}.msg{background:#eef;padding:.6rem;border-radius:4px}</style>
<h1>AgentGate — pending approvals</h1>`)
	if msg := r.URL.Query().Get("msg"); msg != "" {
		fmt.Fprintf(w, `<p class="msg">%s</p>`, html.EscapeString(msg))
	}
	if len(pending) == 0 {
		fmt.Fprint(w, "<p>Nothing waiting. ✨</p>")
		return
	}
	fmt.Fprint(w, "<table><tr><th>When</th><th>Who</th><th>Tool</th><th>Args</th><th></th></tr>")
	for _, p := range pending {
		args, _ := json.Marshal(p.Args)
		fmt.Fprintf(w, `<tr><td>%s</td><td>%s<br><small>%s</small></td><td><code>%s</code></td><td><code>%s</code></td>
<td><form method="post" action="/decide" style="display:inline">
<input type="hidden" name="transaction_id" value="%s">
<button name="decision" value="approve">Approve</button>
<button name="decision" value="deny">Deny</button></form></td></tr>`,
			p.CreatedAt.Format("15:04:05"),
			html.EscapeString(p.OnBehalfOf), html.EscapeString(p.AgentID),
			html.EscapeString(p.Tool), html.EscapeString(string(args)),
			html.EscapeString(p.TransactionID))
	}
	fmt.Fprint(w, "</table>")
}
