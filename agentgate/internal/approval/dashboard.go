package approval

import "net/http"

// dashboard serves a single self-contained, read-only page (Phase 7). It
// polls the JSON endpoints (/audit, /pending, /relations) and renders three
// live panels. Deliberately has NO approve/deny controls — decisions happen
// on the approval UI ("/") or in Slack; the dashboard is for visibility only.
func (h *HTTPServer) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>AgentGate Dashboard</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body { font-family: system-ui, sans-serif; margin: 0; background: #0f1117; color: #e6e6e6; }
  header { padding: 1rem 1.5rem; background: #161a23; border-bottom: 1px solid #262b36;
           display: flex; align-items: center; gap: 1rem; position: sticky; top: 0; z-index: 10; }
  header h1 { font-size: 1.1rem; margin: 0; }
  .sub { color: #8b93a7; font-size: .85rem; }
  .live { margin-left: auto; font-size: .8rem; color: #8b93a7; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #3fb950; margin-right: .35rem; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: .3; } }
  main { padding: 1.25rem; display: grid; gap: 1.25rem; grid-template-columns: 1fr; max-width: 1200px; margin: 0 auto; }
  section { background: #161a23; border: 1px solid #262b36; border-radius: 10px; overflow: hidden; }
  section > h2 { font-size: .95rem; margin: 0; padding: .75rem 1rem; background: #1b2029;
                 border-bottom: 1px solid #262b36; display: flex; justify-content: space-between; }
  .count { color: #8b93a7; font-weight: normal; }
  table { width: 100%; border-collapse: collapse; font-size: .82rem; }
  th, td { text-align: left; padding: .5rem .75rem; border-bottom: 1px solid #21262f; vertical-align: top; }
  th { color: #8b93a7; font-weight: 600; position: sticky; }
  tbody tr:hover { background: #1b2029; }
  code { font-family: ui-monospace, monospace; background: #0f1117; padding: .05rem .3rem; border-radius: 4px; font-size: .95em; }
  .pill { display: inline-block; padding: .1rem .5rem; border-radius: 999px; font-size: .72rem; font-weight: 600; white-space: nowrap; }
  .allow          { background: #10331d; color: #4ade80; }
  .deny           { background: #3a1618; color: #f87171; }
  .needs_approval, .pending { background: #3a2e10; color: #fbbf24; }
  .executed_after_approval, .executed { background: #10331d; color: #4ade80; }
  .denied_by_human, .execute_failed, .failed, .denied { background: #3a1618; color: #f87171; }
  .muted { color: #8b93a7; }
  .scroll { max-height: 60vh; overflow: auto; }
  .empty { padding: 1.25rem; color: #8b93a7; }
  .args { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
<header>
  <h1>🛡️ AgentGate <span class="sub">governance dashboard · read-only</span></h1>
  <span class="live"><span class="dot"></span>live · refreshes every 3s</span>
</header>
<main>
  <section>
    <h2>Pending approvals <span class="count" id="pending-count"></span></h2>
    <div class="scroll"><table>
      <thead><tr><th>When</th><th>On behalf of</th><th>Tool</th><th>Args</th><th>State</th></tr></thead>
      <tbody id="pending-body"></tbody>
    </table><div class="empty" id="pending-empty" hidden>Nothing waiting. ✨</div></div>
  </section>

  <section id="relations-section">
    <h2>Ownership graph <span class="count" id="relations-count"></span></h2>
    <div class="scroll"><table>
      <thead><tr><th>User</th><th>owns →</th><th>Record</th></tr></thead>
      <tbody id="relations-body"></tbody>
    </table><div class="empty" id="relations-empty" hidden>No relationships.</div></div>
  </section>

  <section>
    <h2>Audit log <span class="count" id="audit-count"></span></h2>
    <div class="scroll"><table>
      <thead><tr><th>When</th><th>On behalf of</th><th>Method</th><th>Tool</th><th>Decision</th><th>Reason</th></tr></thead>
      <tbody id="audit-body"></tbody>
    </table><div class="empty" id="audit-empty" hidden>No decisions yet.</div></div>
  </section>
</main>

<script>
const esc = s => String(s ?? "").replace(/[&<>"]/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[c]));
const time = s => s ? new Date(s).toLocaleTimeString() : "";
const pill = s => '<span class="pill ' + esc(s) + '">' + esc(s) + '</span>';
const argstr = a => a ? JSON.stringify(a) : "";

async function getJSON(url) {
  try { const r = await fetch(url); if (!r.ok) return null; return await r.json(); }
  catch { return null; }
}

function fill(bodyId, emptyId, countId, rows, render) {
  const body = document.getElementById(bodyId);
  const empty = document.getElementById(emptyId);
  const count = document.getElementById(countId);
  rows = rows || [];
  count.textContent = rows.length ? rows.length : "";
  empty.hidden = rows.length > 0;
  body.innerHTML = rows.map(render).join("");
}

async function refresh() {
  const [pending, audit, relations] = await Promise.all([
    getJSON("/pending"), getJSON("/audit?limit=100"), getJSON("/relations"),
  ]);

  fill("pending-body", "pending-empty", "pending-count", pending, p =>
    "<tr><td class='muted'>" + time(p.created_at) + "</td><td>" + esc(p.on_behalf_of) +
    "</td><td><code>" + esc(p.tool) + "</code></td><td class='args'><code>" + esc(argstr(p.args)) +
    "</code></td><td>" + pill(p.state) + "</td></tr>");

  fill("audit-body", "audit-empty", "audit-count", audit, a =>
    "<tr><td class='muted'>" + time(a.created_at) + "</td><td>" + esc(a.on_behalf_of) +
    "</td><td class='muted'>" + esc(a.method) + "</td><td><code>" + esc(a.tool) +
    "</code></td><td>" + pill(a.decision) + "</td><td class='muted'>" + esc(a.reason) + "</td></tr>");

  // Relationship graph is optional; hide the panel entirely if not configured.
  const relSection = document.getElementById("relations-section");
  if (relations === null) { relSection.hidden = true; }
  else {
    relSection.hidden = false;
    fill("relations-body", "relations-empty", "relations-count", relations, r =>
      "<tr><td>" + esc(r.user) + "</td><td class='muted'>owns</td><td><code>record " +
      esc(r.record_id) + "</code></td></tr>");
  }
}

refresh();
setInterval(refresh, 3000);
</script>
</html>`
