package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SecurityCatalogEntry is intentionally payload-free. It tells the agent what
// to validate while keeping attack material in user-approved evidence files.
type SecurityCatalogEntry struct {
	ID                   string   `json:"id"`
	Domain               string   `json:"domain"`
	Category             string   `json:"category"`
	Name                 string   `json:"name"`
	Checks               []string `json:"checks"`
	EvidenceRequirements []string `json:"evidenceRequirements,omitempty"`
	AppliesTo            []string `json:"appliesTo,omitempty"`
	References           []string `json:"references,omitempty"`
}

var securityCatalog = []SecurityCatalogEntry{
	{ID: "RECON-DNS", Domain: "infra", Category: "recon", Name: "DNS and domain exposure", Checks: []string{"authoritative DNS inventory", "zone transfer exposure", "orphaned CNAME and dangling third-party records", "subdomain lifecycle hygiene", "DNS record drift and stale assets"}},
	{ID: "RECON-EDGE", Domain: "infra", Category: "edge", Name: "CDN, WAF, and origin exposure", Checks: []string{"CDN and WAF coverage mapping", "origin exposure through historical records and certificates", "mail and non-web asset source disclosure", "development or test subdomain origin mismatch", "edge-to-origin access control assumptions"}},
	{ID: "RECON-PORTS", Domain: "infra", Category: "services", Name: "Public service and management port exposure", Checks: []string{"internet-exposed management services", "database and cache service exposure", "anonymous or unauthenticated access indicators", "default credential risk", "service owner and business justification"}},
	{ID: "RECON-LEAKS", Domain: "web-api", Category: "exposure", Name: "Source, backup, and sensitive file exposure", Checks: []string{"version-control directory exposure", "temporary backup and archive files", "platform metadata files", "Java WEB-INF and META-INF access", "directory listing and source artifact disclosure"}},
	{ID: "RECON-DOCS", Domain: "web-api", Category: "exposure", Name: "Monitoring and API documentation exposure", Checks: []string{"Spring Boot Actuator exposure", "heap dump and environment endpoint access", "Swagger ReDoc OpenAPI and Postman collection exposure", "hidden endpoint and parameter discovery", "documentation access control and environment separation"}},
	{ID: "WEB-AUTH", Domain: "web-api", Category: "identity", Name: "Authentication weaknesses", Checks: []string{"login and recovery enumeration", "MFA bypass and downgrade", "default or weak credentials", "rate-limit and lockout behavior"}, References: []string{"OWASP ASVS V2"}},
	{ID: "AUTH-SSO", Domain: "web-api", Category: "identity", Name: "OAuth OIDC and SAML authentication flows", Checks: []string{"redirect URI validation", "state and nonce binding", "authorization code and token handling", "SAML signature verification", "XML signature wrapping resilience"}},
	{ID: "AUTH-RESET", Domain: "web-api", Category: "identity", Name: "Password reset and account recovery", Checks: []string{"Host header trust in reset links", "reset token entropy and one-time use", "verification code exposure in responses", "email and phone rebinding controls", "user enumeration through messages or timing"}},
	{ID: "AUTH-MFA", Domain: "web-api", Category: "identity", Name: "Multi-factor authentication enforcement", Checks: []string{"MFA step binding to authenticated session", "direct URL access before MFA completion", "verification attempt rate limits", "successful verification token reuse", "remember-device and recovery-code controls"}},
	{ID: "WEB-SESS", Domain: "web-api", Category: "identity", Name: "Session management", Checks: []string{"cookie attributes", "rotation after auth and privilege change", "logout invalidation", "concurrent session policy", "token replay and fixation"}, References: []string{"OWASP ASVS V3"}},
	{ID: "WEB-AC", Domain: "web-api", Category: "authorization", Name: "Access control and IDOR/BOLA", Checks: []string{"horizontal access", "vertical access", "object and function authorization", "tenant isolation", "mass assignment"}, References: []string{"OWASP API1", "OWASP API5", "CWE-639"}},
	{ID: "WEB-AC-STATE", Domain: "web-api", Category: "authorization", Name: "Stateful and distributed authorization", Checks: []string{"context and state transition authorization", "client-side role or permission trust", "gateway and microservice authorization consistency", "internal API secondary checks", "SSRF-to-internal-service authorization assumptions"}},
	{ID: "WEB-INJECT", Domain: "web-api", Category: "injection", Name: "Injection", Checks: []string{"SQL and NoSQL injection", "OS command injection", "LDAP and XPath injection", "template and expression injection", "CRLF and header injection"}, References: []string{"CWE-74", "CWE-89", "CWE-78"}},
	{ID: "WEB-XSS-CSRF", Domain: "web-api", Category: "browser", Name: "XSS, CSRF, CSP, and clickjacking", Checks: []string{"reflected stored and DOM XSS", "CSP coverage and bypass-prone trust choices", "mutation XSS risk in rich HTML parsing", "CSRF protection on state-changing actions", "frame embedding and clickjacking controls"}},
	{ID: "WEB-RCE-TEMPLATE", Domain: "web-api", Category: "injection", Name: "RCE, code, expression, and template injection", Checks: []string{"command execution sinks", "dynamic code evaluation sinks", "Java expression language injection surfaces", "server-side template injection surfaces", "safe validation and sandbox boundaries"}},
	{ID: "WEB-FILE", Domain: "web-api", Category: "file", Name: "File handling", Checks: []string{"upload validation and storage", "path traversal", "arbitrary file read or write", "archive extraction", "content-type confusion"}, References: []string{"CWE-22", "CWE-434"}},
	{ID: "WEB-FILE-PROCESS", Domain: "web-api", Category: "file", Name: "Advanced file and media processing", Checks: []string{"archive extraction path safety", "image document and media parser exposure", "server-side file conversion trust boundaries", "stored file execution controls", "safe handling of untrusted metadata"}},
	{ID: "WEB-DESER", Domain: "web-api", Category: "server-side", Name: "Deserialization and unsafe object processing", Checks: []string{"language-native deserialization inputs", "signed or encrypted object trust", "gadget-capable dependency exposure", "session and cache object handling", "post-exploitation persistence indicators without performing persistence"}},
	{ID: "WEB-SSRF", Domain: "web-api", Category: "server-side", Name: "SSRF and callback abuse", Checks: []string{"URL parser confusion", "redirect handling", "private and metadata ranges", "DNS rebinding controls", "blind callback evidence"}, References: []string{"CWE-918"}},
	{ID: "WEB-XML", Domain: "web-api", Category: "server-side", Name: "XML and unsafe parsing", Checks: []string{"XXE", "XML entity expansion", "unsafe deserialization", "prototype pollution", "parser differential"}, References: []string{"CWE-611", "CWE-502"}},
	{ID: "WEB-REQ", Domain: "web-api", Category: "protocol", Name: "HTTP protocol attacks", Checks: []string{"request smuggling indicators", "host header trust", "cache poisoning", "HTTP parameter pollution", "response splitting"}},
	{ID: "WEB-PROTO-H2", Domain: "web-api", Category: "protocol", Name: "HTTP parsing, HTTP/2 downgrade, and transport semantics", Checks: []string{"front-end and back-end parser consistency", "HTTP/2 to HTTP/1.1 downgrade assumptions", "CRLF handling in headers and redirects", "safe handling of unusual transfer semantics", "edge security controls and origin behavior alignment"}},
	{ID: "WEB-BROWSER", Domain: "web-api", Category: "browser", Name: "Browser-side security", Checks: []string{"stored reflected and DOM XSS", "CSRF", "CORS trust", "clickjacking", "postMessage origin checks"}, References: []string{"CWE-79", "CWE-352"}},
	{ID: "WEB-LOGIC", Domain: "web-api", Category: "business-logic", Name: "Business logic abuse", Checks: []string{"workflow bypass", "race conditions", "price and quantity manipulation", "coupon and credit abuse", "state transition integrity"}},
	{ID: "WEB-BIZ-FINANCE", Domain: "web-api", Category: "business-logic", Name: "Transaction and financial logic", Checks: []string{"amount quantity and sign manipulation", "integer overflow and rounding behavior", "coupon credit and reward abuse", "refund settlement and fulfillment state integrity", "open redirect in login and checkout flows"}},
	{ID: "WEB-BIZ-RACE", Domain: "web-api", Category: "business-logic", Name: "Race conditions and resource abuse", Checks: []string{"concurrency-sensitive balance and inventory operations", "idempotency and replay controls", "SMS and email sending frequency controls", "multi-dimensional rate limits", "safe low-noise race validation planning"}},
	{ID: "WEB-DATA", Domain: "web-api", Category: "data", Name: "Sensitive data exposure", Checks: []string{"secrets and PII in responses", "logs and error messages", "backup and source files", "weak transport", "client-side embedded secrets"}},
	{ID: "API-GQL", Domain: "web-api", Category: "api", Name: "GraphQL and realtime APIs", Checks: []string{"introspection exposure", "field authorization", "query depth and batching", "subscription authorization", "WebSocket origin and message validation"}},
	{ID: "API-WS", Domain: "web-api", Category: "api", Name: "WebSocket and realtime channel security", Checks: []string{"Origin validation during handshake", "authorization continuity after upgrade", "message-level input validation", "tenant and role isolation in subscriptions", "server-side injection through realtime messages"}},
	{ID: "API-WEBHOOK", Domain: "web-api", Category: "api", Name: "Webhook and async processing", Checks: []string{"signature verification", "replay protection", "callback authorization", "queue message trust", "idempotency"}},
	{ID: "INFRA-SVC", Domain: "infra", Category: "services", Name: "Exposed services and management planes", Checks: []string{"service inventory", "anonymous access", "default credentials", "unsafe methods", "management interface exposure"}},
	{ID: "INFRA-CONFIG", Domain: "infra", Category: "configuration", Name: "Security misconfiguration", Checks: []string{"debug mode", "directory listing", "verbose errors", "unnecessary features", "insecure defaults"}},
	{ID: "INFRA-TLS", Domain: "infra", Category: "crypto", Name: "TLS and cryptography", Checks: []string{"protocols and ciphers", "certificate validation", "hard-coded keys", "weak randomness", "unsafe password storage"}},
	{ID: "INFRA-DEPS", Domain: "infra", Category: "supply-chain", Name: "Dependencies and supply chain", Checks: []string{"known vulnerable components", "dependency confusion", "untrusted update path", "artifact integrity", "CI/CD secret exposure"}},
	{ID: "MODERN-COMPONENTS", Domain: "infra", Category: "supply-chain", Name: "Framework, middleware, and plugin CVE exposure", Checks: []string{"framework and middleware version inventory", "historically critical component exposure", "plugin and extension inventory", "reachable vulnerable feature assessment", "patch and compensating-control evidence"}},
	{ID: "CLOUD-IAM", Domain: "cloud", Category: "identity", Name: "Cloud IAM", Checks: []string{"excessive permissions", "role assumption trust", "service account exposure", "cross-account access", "stale credentials"}},
	{ID: "CLOUD-DATA", Domain: "cloud", Category: "storage", Name: "Cloud data exposure", Checks: []string{"public object storage", "snapshot and backup exposure", "database network access", "signed URL scope", "encryption and key policy"}},
	{ID: "CLOUD-META", Domain: "cloud", Category: "workload", Name: "Cloud workload boundaries", Checks: []string{"metadata service exposure", "container escape indicators", "orchestrator RBAC", "secret mounts", "serverless event trust"}},
	{ID: "CLOUD-K8S", Domain: "cloud", Category: "workload", Name: "Container and Kubernetes posture", Checks: []string{"container privilege and mount posture", "service account token exposure", "Kubernetes RBAC and namespace boundaries", "in-cluster metadata and secret access", "escape preconditions documented without attempting escape"}},
	{ID: "MOBILE-CLIENT", Domain: "client", Category: "mobile-desktop", Name: "Client application security", Checks: []string{"local secret storage", "certificate validation", "deep link handling", "IPC authorization", "update integrity"}},
	{ID: "LLM-WEB-INTEGRATION", Domain: "ai-agent", Category: "web-integration", Name: "AI plugin and web backend integration", Checks: []string{"AI-accessible backend API authorization", "user intent and approval propagation", "indirect prompt injection through web content", "tool action scoping and auditability", "sensitive operation confirmation gates"}},
	{ID: "LLM-DIRECT", Domain: "ai-agent", Category: "prompt-injection", Name: "Direct prompt injection and instruction override", Checks: []string{"role and authority spoofing", "system rule replacement", "developer or debug impersonation", "conditional and coercive instructions", "multi-turn escalation"}, References: []string{"OWASP LLM01"}},
	{ID: "LLM-JAILBREAK", Domain: "ai-agent", Category: "jailbreak", Name: "Jailbreak resilience", Checks: []string{"role-play and persona attacks", "fictional-world reframing", "translation and multilingual attacks", "policy inversion", "dual-response and forced-prefix techniques"}, References: []string{"OWASP LLM01"}},
	{ID: "LLM-OBFUSCATE", Domain: "ai-agent", Category: "evasion", Name: "Encoding and obfuscation bypass", Checks: []string{"Base64 hex ROT13 and Unicode", "homoglyph and zero-width text", "reversal and token splitting", "pinyin and multilingual transformation", "nested JSON XML code and markdown"}},
	{ID: "LLM-INDIRECT", Domain: "ai-agent", Category: "indirect-injection", Name: "Indirect prompt injection", Checks: []string{"web and document instructions", "email and issue content", "image OCR and metadata", "tool result injection", "retrieved content authority separation"}, References: []string{"OWASP LLM01"}},
	{ID: "LLM-LEAK", Domain: "ai-agent", Category: "confidentiality", Name: "System prompt and context leakage", Checks: []string{"direct extraction", "debug and serialization framing", "partial and multi-turn reconstruction", "RAG context leakage", "hidden chain and tool schema disclosure"}, References: []string{"OWASP LLM07"}},
	{ID: "LLM-RAG", Domain: "ai-agent", Category: "rag", Name: "RAG poisoning and retrieval security", Checks: []string{"source trust and provenance", "poisoned chunk influence", "tenant isolation", "ACL-aware retrieval", "citation faithfulness and stale content"}, References: []string{"OWASP LLM08"}},
	{ID: "AGENT-TOOLS", Domain: "ai-agent", Category: "tool-security", Name: "Tool invocation and excessive agency", Checks: []string{"argument injection", "tool allowlist and approval", "read/write boundary enforcement", "dangerous action confirmation", "result validation and least privilege"}, References: []string{"OWASP LLM06", "OWASP LLM08"}},
	{ID: "AGENT-SCOPE", Domain: "ai-agent", Category: "authorization", Name: "Agent scope and authorization", Checks: []string{"cross-user and cross-tenant access", "confused deputy behavior", "identity propagation", "out-of-scope target expansion", "privilege escalation through chained tools"}},
	{ID: "AGENT-MEM", Domain: "ai-agent", Category: "memory", Name: "Memory and context poisoning", Checks: []string{"untrusted memory persistence", "cross-session contamination", "instruction stored as fact", "memory deletion and correction", "sensitive data retention"}},
	{ID: "AGENT-CHAIN", Domain: "ai-agent", Category: "multi-agent", Name: "Multi-agent and MCP trust", Checks: []string{"agent-to-agent instruction trust", "MCP server spoofing", "tool description poisoning", "delegation scope", "provenance across handoffs"}},
	{ID: "LLM-OUTPUT", Domain: "ai-agent", Category: "output", Name: "Insecure output handling", Checks: []string{"HTML and script rendering", "shell SQL and template sinks", "unsafe markdown links", "downstream parser injection", "output encoding"}, References: []string{"OWASP LLM05"}},
	{ID: "LLM-DATA", Domain: "ai-agent", Category: "privacy", Name: "Sensitive information disclosure", Checks: []string{"training and context memorization", "secret and PII exfiltration", "cross-tenant leakage", "log and telemetry exposure", "redaction consistency"}, References: []string{"OWASP LLM02"}},
	{ID: "LLM-DOS", Domain: "ai-agent", Category: "availability", Name: "Model and agent denial of service", Checks: []string{"unbounded output", "recursive tool loops", "context flooding", "expensive retrieval or tool fan-out", "rate token and budget limits"}, References: []string{"OWASP LLM10"}},
	{ID: "LLM-INTEGRITY", Domain: "ai-agent", Category: "integrity", Name: "Hallucination and decision integrity", Checks: []string{"fabricated facts and citations", "unsupported high-impact decisions", "confidence calibration", "source conflict handling", "human review gates"}, References: []string{"OWASP LLM09"}},
	{ID: "LLM-MODEL", Domain: "ai-agent", Category: "model-supply-chain", Name: "Model and adapter supply chain", Checks: []string{"model provenance and hashes", "unsafe serialization", "adapter and plugin trust", "dependency vulnerabilities", "rollback and version controls"}, References: []string{"OWASP LLM03", "OWASP LLM04"}},
}

type SecurityCatalogTool struct{}

func (t *SecurityCatalogTool) Name() string { return "SecurityCatalog" }
func (t *SecurityCatalogTool) Description() string {
	return "List, search, show, or target-filter a payload-free security test catalog with checks, evidence requirements, and applicability tags."
}
func (t *SecurityCatalogTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action":          map[string]interface{}{"type": "string", "enum": []string{"list", "search", "show"}},
		"domain":          map[string]interface{}{"type": "string", "enum": []string{"web-api", "infra", "cloud", "client", "ai-agent"}},
		"query":           map[string]interface{}{"type": "string"},
		"id":              map[string]interface{}{"type": "string"},
		"feature":         map[string]interface{}{"type": "string"},
		"target_features": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
	}, "required": []string{"action"}}
}
func (t *SecurityCatalogTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	domain := strings.ToLower(strings.TrimSpace(toString(input["domain"])))
	query := strings.ToLower(strings.TrimSpace(toString(input["query"])))
	id := strings.TrimSpace(toString(input["id"]))
	features := append(stringListInput(input["target_features"]), strings.TrimSpace(toString(input["feature"])))
	var matches []SecurityCatalogEntry
	for _, entry := range securityCatalog {
		entry = enrichSecurityCatalogEntry(entry)
		if action == "show" && !strings.EqualFold(entry.ID, id) {
			continue
		}
		if domain != "" && entry.Domain != domain {
			continue
		}
		if len(nonEmptyStrings(features)) > 0 && !securityCatalogApplies(entry, features) {
			continue
		}
		data, _ := json.Marshal(entry)
		if action == "search" && query != "" && !strings.Contains(strings.ToLower(string(data)), query) {
			continue
		}
		matches = append(matches, entry)
	}
	if action != "list" && action != "search" && action != "show" {
		return &ToolResult{Content: "unknown catalog action: " + action, IsError: true}, nil
	}
	if action == "show" && len(matches) == 0 {
		return &ToolResult{Content: "catalog entry not found: " + id, IsError: true}, nil
	}
	return marshalTool(matches)
}

func enrichSecurityCatalogEntry(entry SecurityCatalogEntry) SecurityCatalogEntry {
	if len(entry.EvidenceRequirements) == 0 {
		entry.EvidenceRequirements = defaultEvidenceRequirements(entry)
	}
	if len(entry.AppliesTo) == 0 {
		entry.AppliesTo = defaultAppliesTo(entry)
	}
	return entry
}

func defaultEvidenceRequirements(entry SecurityCatalogEntry) []string {
	switch entry.Domain {
	case "web-api":
		requirements := []string{"endpoint inventory and auth context", "Burp Request and Burp Response for confirmed findings", "role, tenant, and precondition notes"}
		switch entry.Category {
		case "exposure":
			return append(requirements, "non-invasive URL or metadata evidence", "business owner and environment classification")
		case "identity":
			return append(requirements, "account role matrix and session state")
		case "authorization":
			return append(requirements, "authorized baseline and unauthorized comparison across roles or tenants")
		case "business-logic":
			return append(requirements, "business preconditions, sample size, and state transition evidence")
		case "injection", "server-side", "file", "protocol", "browser", "api":
			return append(requirements, "minimal-impact validation evidence and safe negative control")
		default:
			return requirements
		}
	case "infra":
		return []string{"asset and service inventory", "tool output or configuration evidence", "version and exposure notes", "risk rationale"}
	case "cloud":
		return []string{"cloud account/project scope", "resource identifier", "IAM or policy evidence", "public exposure or trust-path evidence"}
	case "client":
		return []string{"client platform and version", "local configuration or traffic evidence", "reproduction preconditions", "privacy-safe proof"}
	case "ai-agent":
		return []string{"test case record with expected and actual behavior", "redacted transcript or tool-call trace", "model/app version and trust boundary", "linked evidence and finding for failures"}
	default:
		return []string{"tested/not-tested status", "evidence reference", "residual risk notes"}
	}
}

func defaultAppliesTo(entry SecurityCatalogEntry) []string {
	switch entry.ID {
	case "RECON-DNS":
		return []string{"infra", "dns", "domain", "subdomains", "cname", "takeover", "axfr"}
	case "RECON-EDGE":
		return []string{"infra", "cdn", "waf", "edge", "origin", "cloudflare", "certificates"}
	case "RECON-PORTS":
		return []string{"infra", "network", "ports", "ssh", "rdp", "mysql", "redis", "elasticsearch", "management-plane"}
	case "RECON-LEAKS":
		return []string{"web", "api", "source-leak", "git", "svn", "backup", "ds-store", "web-inf", "meta-inf"}
	case "RECON-DOCS":
		return []string{"web", "api", "actuator", "swagger", "redoc", "openapi", "postman", "monitoring"}
	case "WEB-AUTH":
		return []string{"web", "api", "login", "accounts", "mfa", "password-recovery"}
	case "AUTH-SSO":
		return []string{"web", "api", "oauth", "oidc", "saml", "sso", "login", "federation"}
	case "AUTH-RESET":
		return []string{"web", "api", "password-reset", "account-recovery", "host-header", "verification-code"}
	case "AUTH-MFA":
		return []string{"web", "api", "mfa", "2fa", "otp", "recovery-code", "remember-device"}
	case "WEB-SESS":
		return []string{"web", "api", "authenticated", "cookies", "tokens", "sessions"}
	case "WEB-AC":
		return []string{"web", "api", "authenticated", "multi-role", "multi-tenant", "object-ids"}
	case "WEB-AC-STATE":
		return []string{"web", "api", "workflow", "state", "microservices", "gateway", "internal-api", "ssrf"}
	case "WEB-INJECT":
		return []string{"web", "api", "forms", "parameters", "search", "database", "commands", "templates"}
	case "WEB-XSS-CSRF":
		return []string{"web", "browser", "frontend", "xss", "csrf", "csp", "mxss", "clickjacking", "iframe"}
	case "WEB-RCE-TEMPLATE":
		return []string{"web", "api", "rce", "command", "eval", "spel", "ognl", "ssti", "template"}
	case "WEB-FILE":
		return []string{"web", "api", "uploads", "downloads", "files", "archives"}
	case "WEB-FILE-PROCESS":
		return []string{"web", "api", "uploads", "archives", "zip", "images", "documents", "ffmpeg", "imagemagick", "pdf"}
	case "WEB-DESER":
		return []string{"web", "api", "deserialization", "serialized", "java", "php", "python", "nodejs", "session"}
	case "WEB-SSRF":
		return []string{"web", "api", "url-fetch", "webhooks", "imports", "callbacks", "cloud"}
	case "WEB-XML":
		return []string{"web", "api", "xml", "soap", "deserialization", "parsers"}
	case "WEB-REQ":
		return []string{"web", "api", "reverse-proxy", "cache", "host-header", "http"}
	case "WEB-PROTO-H2":
		return []string{"web", "api", "http2", "h2c", "proxy", "gateway", "cdn", "waf", "crlf", "transport"}
	case "WEB-BROWSER":
		return []string{"web", "browser", "frontend", "javascript", "cors", "csrf"}
	case "WEB-LOGIC":
		return []string{"web", "api", "authenticated", "workflow", "payments", "state-changing"}
	case "WEB-BIZ-FINANCE":
		return []string{"web", "api", "payments", "checkout", "refunds", "coupons", "credits", "orders", "open-redirect"}
	case "WEB-BIZ-RACE":
		return []string{"web", "api", "race", "concurrency", "idempotency", "sms", "email", "rate-limit", "inventory"}
	case "WEB-DATA":
		return []string{"web", "api", "pii", "secrets", "logs", "errors", "backups"}
	case "API-GQL":
		return []string{"api", "graphql", "websocket", "realtime", "subscriptions"}
	case "API-WS":
		return []string{"api", "websocket", "realtime", "subscriptions", "origin", "cswsh"}
	case "API-WEBHOOK":
		return []string{"api", "webhooks", "async", "queues", "callbacks"}
	case "INFRA-SVC":
		return []string{"infra", "network", "services", "management-plane"}
	case "INFRA-CONFIG":
		return []string{"infra", "web", "api", "configuration", "debug"}
	case "INFRA-TLS":
		return []string{"infra", "web", "api", "tls", "crypto", "certificates"}
	case "INFRA-DEPS":
		return []string{"infra", "repository", "dependencies", "ci-cd", "supply-chain"}
	case "MODERN-COMPONENTS":
		return []string{"infra", "web", "api", "components", "frameworks", "middleware", "plugins", "cve", "struts", "spring", "log4j", "shiro", "fastjson", "wordpress"}
	case "CLOUD-IAM":
		return []string{"cloud", "iam", "service-accounts", "roles"}
	case "CLOUD-DATA":
		return []string{"cloud", "storage", "database", "backups", "signed-urls"}
	case "CLOUD-META":
		return []string{"cloud", "containers", "serverless", "metadata", "orchestrator"}
	case "CLOUD-K8S":
		return []string{"cloud", "containers", "docker", "kubernetes", "k8s", "rbac", "service-account", "namespace"}
	case "MOBILE-CLIENT":
		return []string{"mobile", "desktop", "client", "deep-links", "local-storage"}
	case "LLM-WEB-INTEGRATION":
		return []string{"ai", "llm", "agent", "plugin", "web", "api", "tools", "indirect-injection", "approval"}
	}
	if entry.Domain == "ai-agent" {
		return []string{"ai", "llm", "agent", "rag", "tools", "mcp", "plugins"}
	}
	return []string{entry.Domain, entry.Category}
}

func securityCatalogApplies(entry SecurityCatalogEntry, features []string) bool {
	for _, feature := range nonEmptyStrings(features) {
		feature = strings.ToLower(feature)
		for _, applies := range entry.AppliesTo {
			applies = strings.ToLower(strings.TrimSpace(applies))
			if applies == "all" || applies == feature || strings.Contains(applies, feature) || strings.Contains(feature, applies) {
				return true
			}
		}
	}
	return false
}

func nonEmptyStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			out = append(out, text)
		}
	}
	return out
}

type SecurityTestRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Vector     string `json:"vector,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Turn       string `json:"turn,omitempty"`
	Objective  string `json:"objective,omitempty"`
	PayloadRef string `json:"payloadRef,omitempty"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Severity   string `json:"severity,omitempty"`
	EvidenceID string `json:"evidenceId,omitempty"`
	FindingID  string `json:"findingId,omitempty"`
	Notes      string `json:"notes,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}

type SecurityTestTool struct{}

func (t *SecurityTestTool) Name() string { return "SecurityTest" }
func (t *SecurityTestTool) Description() string {
	return "Record AI and agent security test cases with vector, channel, expected behavior, actual behavior, outcome, and evidence linkage."
}
func (t *SecurityTestTool) InputSchema() map[string]interface{} {
	fields := map[string]interface{}{}
	for _, key := range []string{"id", "title", "vector", "channel", "turn", "objective", "payload_ref", "expected", "actual", "outcome", "severity", "evidence_id", "finding_id", "notes"} {
		fields[key] = map[string]interface{}{"type": "string"}
	}
	fields["action"] = map[string]interface{}{"type": "string", "enum": []string{"add", "update", "list", "show", "clear"}}
	return map[string]interface{}{"type": "object", "properties": fields, "required": []string{"action"}}
}
func (t *SecurityTestTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	agentState.Lock()
	defer agentState.Unlock()
	switch action {
	case "add":
		record := securityTestFromInput(input)
		if record.Title == "" {
			return &ToolResult{Content: "missing title", IsError: true}, nil
		}
		if record.ID == "" {
			record.ID = "T-" + strings.ToUpper(uuid.New().String()[:8])
		}
		record.UpdatedAt = time.Now().Format(time.RFC3339)
		agentState.SecurityTests = append(agentState.SecurityTests, record)
	case "update":
		id := strings.TrimSpace(toString(input["id"]))
		for i := range agentState.SecurityTests {
			if strings.EqualFold(agentState.SecurityTests[i].ID, id) {
				mergeSecurityTest(&agentState.SecurityTests[i], input)
				agentState.SecurityTests[i].UpdatedAt = time.Now().Format(time.RFC3339)
				return marshalTool(agentState.SecurityTests[i])
			}
		}
		return &ToolResult{Content: "security test not found: " + id, IsError: true}, nil
	case "show":
		id := strings.TrimSpace(toString(input["id"]))
		for _, record := range agentState.SecurityTests {
			if strings.EqualFold(record.ID, id) {
				return marshalTool(record)
			}
		}
		return &ToolResult{Content: "security test not found: " + id, IsError: true}, nil
	case "clear":
		agentState.SecurityTests = nil
	case "list", "":
	default:
		return &ToolResult{Content: "unknown security test action: " + action, IsError: true}, nil
	}
	return marshalTool(agentState.SecurityTests)
}

func securityTestFromInput(input map[string]interface{}) SecurityTestRecord {
	record := SecurityTestRecord{ID: strings.TrimSpace(toString(input["id"]))}
	mergeSecurityTest(&record, input)
	return record
}

func mergeSecurityTest(record *SecurityTestRecord, input map[string]interface{}) {
	fields := map[string]*string{
		"title": &record.Title, "vector": &record.Vector, "channel": &record.Channel,
		"turn": &record.Turn, "objective": &record.Objective, "payload_ref": &record.PayloadRef,
		"expected": &record.Expected, "actual": &record.Actual, "outcome": &record.Outcome,
		"severity": &record.Severity, "evidence_id": &record.EvidenceID,
		"finding_id": &record.FindingID, "notes": &record.Notes,
	}
	for key, dst := range fields {
		if value := strings.TrimSpace(toString(input[key])); value != "" {
			*dst = value
		}
	}
	if record.Outcome == "" {
		record.Outcome = "not-run"
	}
}
