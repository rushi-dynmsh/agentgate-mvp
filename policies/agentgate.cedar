// AgentGate authorization policies, written in Cedar.
//
// Shape of every question we ask Cedar:
//   principal — the human the agent acts on behalf of, e.g. AgentGate::User::"bob",
//               with their roles attached as parent groups (AgentGate::Role::"reader")
//   action    — always AgentGate::Action::"call_tool" for now
//   resource  — the tool being invoked, e.g. AgentGate::Tool::"delete_record",
//               carrying a `risk` attribute ("read" or "destructive")
//
// Cedar's answer is binary (allow/deny). The "needs human approval" step for
// destructive tools is layered on top by the agentgate service: Cedar says
// "this principal MAY use this tool", and agentgate then parks destructive
// calls for sign-off instead of forwarding immediately.

// Readers may call read-only tools.
permit (
  principal in AgentGate::Role::"reader",
  action == AgentGate::Action::"call_tool",
  resource
) when { resource.risk == "read" };

// Admins may call any tool — destructive ones still get parked for human
// approval by the service before they reach the backend.
permit (
  principal in AgentGate::Role::"admin",
  action == AgentGate::Action::"call_tool",
  resource
);

// Everything not explicitly permitted is denied — that's Cedar's default,
// stated here for readers new to the language.
