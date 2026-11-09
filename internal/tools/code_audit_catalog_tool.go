package tools

import (
	"context"
	"encoding/json"
	"strings"
)

// CodeAuditCatalogEntry is a payload-free secure code review checklist entry.
// It focuses the agent on framework-specific sources, sinks, and review paths
// without embedding exploit payloads in the system prompt.
type CodeAuditCatalogEntry struct {
	ID         string   `json:"id"`
	Languages  []string `json:"languages"`
	Category   string   `json:"category"`
	Name       string   `json:"name"`
	Sources    []string `json:"sources,omitempty"`
	Sinks      []string `json:"sinks,omitempty"`
	Checks     []string `json:"checks"`
	References []string `json:"references,omitempty"`
}

var codeAuditCatalog = []CodeAuditCatalogEntry{
	{
		ID: "AUD-GEN-DATAFLOW", Languages: []string{"all"}, Category: "methodology", Name: "Source-to-sink data-flow review",
		Sources: []string{"HTTP parameters", "headers and cookies", "request bodies", "file uploads", "environment variables", "message queues", "database records reused as code/config"},
		Sinks:   []string{"database queries", "command execution", "template rendering", "filesystem paths", "network requests", "deserialization", "HTML/JS output"},
		Checks: []string{
			"map trust boundaries before reading individual files",
			"trace attacker-controlled data through validation, normalization, authorization, and encoding",
			"record every finding with file, function, route, source, sink, proof path, impact, and remediation",
		},
		References: []string{"CWE Top 25", "OWASP ASVS", "OWASP Code Review Guide", "Semgrep source-sink rules", "CodeQL query suites"},
	},
	{
		ID: "AUD-GEN-SECRETS", Languages: []string{"all"}, Category: "secrets", Name: "Secrets and sensitive configuration",
		Sources: []string{".env files", "application config", "CI variables", "logs", "test fixtures", "container manifests"},
		Checks: []string{
			"search for hard-coded credentials, tokens, private keys, signing secrets, and connection strings",
			"verify secrets are not logged, committed, returned in errors, or exposed to client-side code",
			"check rotation, least privilege, environment separation, and redaction behavior",
		},
		References: []string{"CWE-798", "OWASP ASVS V6"},
	},
	{
		ID: "AUD-GEN-AUTHZ", Languages: []string{"all"}, Category: "authorization", Name: "Authorization and object-level access control",
		Sources: []string{"routes/controllers", "middleware/filters", "service methods", "ORM queries", "background jobs"},
		Checks: []string{
			"verify every object read/write is constrained by the authenticated principal, tenant, role, and workflow state",
			"look for direct object IDs, mass assignment, hidden admin parameters, and missing server-side policy checks",
			"check background jobs, exports, websockets, and async callbacks for the same authorization rules",
		},
		References: []string{"CWE-639", "OWASP API1", "OWASP ASVS V4"},
	},
	{
		ID: "AUD-PHP-SQL", Languages: []string{"php"}, Category: "injection", Name: "PHP SQL and query injection",
		Sources: []string{"$_GET", "$_POST", "$_REQUEST", "$_COOKIE", "$_SERVER", "JSON request bodies", "uploaded CSV/XML data"},
		Sinks:   []string{"mysqli_query", "mysql_query", "PDO::query", "PDO::exec", "DB::raw", "whereRaw", "orderByRaw", "Doctrine DQL strings"},
		Checks: []string{
			"prefer prepared statements and bound parameters for values and allowlists for identifiers/order clauses",
			"inspect dynamic table, column, sort, filter, and pagination fragments that cannot be parameterized",
			"check framework escape hatches in Laravel, Symfony, WordPress, Drupal, and custom DAO layers",
		},
		References: []string{"CWE-89", "OWASP ASVS V5"},
	},
	{
		ID: "AUD-PHP-FILE", Languages: []string{"php"}, Category: "file", Name: "PHP file inclusion, upload, and path traversal",
		Sources: []string{"route parameters", "multipart filenames", "archive members", "image metadata", "theme/plugin names"},
		Sinks:   []string{"include", "require", "file_get_contents", "fopen", "unlink", "move_uploaded_file", "ZipArchive::extractTo"},
		Checks: []string{
			"normalize paths and enforce workspace/storage roots after decoding and symlink resolution",
			"validate uploads by content, extension allowlist, size, storage location, and execution controls",
			"review LFI/RFI, PHAR metadata paths, archive extraction, image processing, and user-controlled template names",
		},
		References: []string{"CWE-22", "CWE-434", "CWE-98"},
	},
	{
		ID: "AUD-PHP-RCE", Languages: []string{"php"}, Category: "code-execution", Name: "PHP command, template, and dynamic code execution",
		Sources: []string{"request parameters", "admin settings", "template names", "webhook payloads", "database-backed rules"},
		Sinks:   []string{"eval", "assert", "preg_replace /e", "system", "exec", "shell_exec", "passthru", "proc_open", "Twig raw filters"},
		Checks: []string{
			"remove dynamic code execution or restrict it to signed, trusted, server-side definitions",
			"use argument arrays or strict allowlists for commands and never concatenate shell metacharacters",
			"check template sandbox escapes and dangerous extension/filter registration",
		},
		References: []string{"CWE-78", "CWE-94", "CWE-1336"},
	},
	{
		ID: "AUD-PHP-DESER", Languages: []string{"php"}, Category: "deserialization", Name: "PHP unsafe deserialization and magic methods",
		Sources: []string{"cookies", "signed-but-user-controlled blobs", "cache/session stores", "queue messages", "PHAR metadata"},
		Sinks:   []string{"unserialize", "__wakeup", "__destruct", "__toString", "phar:// streams"},
		Checks: []string{
			"avoid unserialize on untrusted data; prefer JSON and typed validation",
			"review gadget chains in dependencies and magic methods with file, command, network, or template side effects",
			"verify signing keys, algorithm choices, replay protection, and allowed_classes when legacy parsing remains",
		},
		References: []string{"CWE-502"},
	},
	{
		ID: "AUD-JAVA-SQL", Languages: []string{"java", "jsp"}, Category: "injection", Name: "Java/JSP SQL, ORM, and expression injection",
		Sources: []string{"ServletRequest parameters", "@RequestParam", "@PathVariable", "headers", "cookies", "JSON bodies", "JSP EL inputs"},
		Sinks:   []string{"Statement.execute", "createQuery string concatenation", "JdbcTemplate raw SQL", "MyBatis ${}", "SpEL", "JSP scriptlets"},
		Checks: []string{
			"prefer PreparedStatement, named parameters, criteria APIs, and MyBatis #{} for values",
			"allowlist identifiers, dynamic sort fields, mapper fragments, and expression names",
			"review JSP expression language, taglibs, and server-side template contexts for untrusted evaluation",
		},
		References: []string{"CWE-89", "CWE-917"},
	},
	{
		ID: "AUD-JAVA-XML-DESER", Languages: []string{"java", "jsp"}, Category: "parsing", Name: "Java XML, deserialization, and parser trust",
		Sources: []string{"XML uploads", "SOAP requests", "serialized blobs", "cookies", "queues", "cache entries"},
		Sinks:   []string{"DocumentBuilderFactory", "SAXParserFactory", "XMLInputFactory", "ObjectInputStream", "XStream", "Jackson default typing", "Fastjson"},
		Checks: []string{
			"disable external entities, DTDs, and external schemas on XML parsers",
			"ban or strictly filter native deserialization and polymorphic type binding from untrusted data",
			"review classpath gadget exposure, signed token parsing, and message queue consumers",
		},
		References: []string{"CWE-611", "CWE-502"},
	},
	{
		ID: "AUD-JAVA-SSRF-FILE", Languages: []string{"java", "jsp"}, Category: "server-side", Name: "Java SSRF, file access, and outbound trust",
		Sources: []string{"callback URLs", "import/export URLs", "avatar/webhook integrations", "path parameters", "zip entries"},
		Sinks:   []string{"URL.openConnection", "HttpClient", "RestTemplate", "WebClient", "Files.readAllBytes", "Paths.get", "ZipInputStream"},
		Checks: []string{
			"validate scheme, host, port, redirects, DNS resolution, and private/metadata ranges at connection time",
			"enforce canonical file roots after decoding, normalization, and symlink checks",
			"review archive extraction and server-side fetch features for confused-deputy behavior",
		},
		References: []string{"CWE-918", "CWE-22"},
	},
	{
		ID: "AUD-JAVA-AUTH", Languages: []string{"java", "jsp"}, Category: "authorization", Name: "Spring/Jakarta authorization coverage",
		Sources: []string{"controllers", "filters", "interceptors", "method security annotations", "repository methods"},
		Checks: []string{
			"verify deny-by-default route configuration and method-level checks for sensitive service methods",
			"look for mismatches between controller checks and repository tenant/owner filters",
			"review CSRF, CORS, session fixation, remember-me, and privilege changes",
		},
		References: []string{"OWASP ASVS V2", "OWASP ASVS V4"},
	},
	{
		ID: "AUD-DOTNET-INJECT", Languages: []string{"dotnet", "asp"}, Category: "injection", Name: ".NET and ASP injection sinks",
		Sources: []string{"Request.Query", "Request.Form", "RouteData", "cookies", "headers", "model binding", "classic ASP Request"},
		Sinks:   []string{"SqlCommand.CommandText", "FromSqlRaw", "ExecuteSqlRaw", "OleDbCommand", "DirectorySearcher.Filter", "XPathNavigator", "Response.Write"},
		Checks: []string{
			"use parameterized queries and allowlists for dynamic identifiers, filters, and sort clauses",
			"review LINQ/provider escape hatches, raw SQL helpers, LDAP filters, XPath, and classic ASP string-built SQL",
			"check output encoding across Razor, WebForms, MVC helpers, and classic ASP templates",
		},
		References: []string{"CWE-89", "CWE-90", "CWE-643", "CWE-79"},
	},
	{
		ID: "AUD-DOTNET-DESER", Languages: []string{"dotnet", "asp"}, Category: "deserialization", Name: ".NET deserialization and ViewState trust",
		Sources: []string{"hidden fields", "cookies", "TempData", "queues", "XML/JSON payloads", "ViewState"},
		Sinks:   []string{"BinaryFormatter", "LosFormatter", "NetDataContractSerializer", "SoapFormatter", "JavaScriptSerializer", "Json.NET TypeNameHandling", "ViewState"},
		Checks: []string{
			"remove dangerous formatters and polymorphic type loading from untrusted data",
			"verify ViewState MAC, encryption where needed, machineKey secrecy, and event validation",
			"review model binders, custom converters, and background consumers for type confusion",
		},
		References: []string{"CWE-502", "OWASP ASVS V5"},
	},
	{
		ID: "AUD-DOTNET-AUTH", Languages: []string{"dotnet", "asp"}, Category: "authorization", Name: ".NET auth, antiforgery, and middleware order",
		Sources: []string{"controllers/pages", "minimal APIs", "filters", "middleware", "SignalR hubs", "classic ASP includes"},
		Checks: []string{
			"verify [Authorize] coverage, policy names, fallback policies, and middleware order",
			"check antiforgery on state-changing browser endpoints and CORS/credentials interactions",
			"review model binding over-posting, tenant filters, direct object IDs, and hub method authorization",
		},
		References: []string{"OWASP ASVS V2", "OWASP ASVS V4"},
	},
	{
		ID: "AUD-JS-XSS", Languages: []string{"javascript", "nodejs"}, Category: "client-side", Name: "JavaScript DOM XSS and unsafe rendering",
		Sources: []string{"location", "hash", "postMessage", "localStorage", "URLSearchParams", "server-rendered JSON", "third-party widgets"},
		Sinks:   []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval", "Function", "setTimeout string", "dangerouslySetInnerHTML"},
		Checks: []string{
			"trace browser-controlled data into DOM, markdown, URL, template, and script execution contexts",
			"verify framework escaping assumptions for React, Vue, Angular, Svelte, SSR hydration, and markdown renderers",
			"review postMessage origins, CSP compatibility, URL sanitization, and trusted-types readiness",
		},
		References: []string{"CWE-79", "OWASP ASVS V5"},
	},
	{
		ID: "AUD-NODE-SERVER", Languages: []string{"nodejs", "javascript"}, Category: "server-side", Name: "Node.js server-side injection, SSRF, and RCE",
		Sources: []string{"Express/Koa/Fastify req params", "headers", "cookies", "JSON bodies", "webhooks", "queues", "GraphQL args"},
		Sinks:   []string{"child_process.exec", "spawn shell:true", "vm.runIn*", "eval", "template engines", "fetch", "axios", "fs", "path.join"},
		Checks: []string{
			"avoid shell execution or use fixed commands with argument arrays and allowlists",
			"validate outbound URLs across redirects, DNS changes, private ranges, and protocol confusion",
			"normalize paths under storage roots and review template helpers/filters for code execution",
		},
		References: []string{"CWE-78", "CWE-918", "CWE-22"},
	},
	{
		ID: "AUD-NODE-PROTO", Languages: []string{"nodejs", "javascript"}, Category: "supply-chain", Name: "Node.js prototype pollution and dependency trust",
		Sources: []string{"JSON merge inputs", "query parsers", "YAML/TOML config", "dependency install scripts", "plugin metadata"},
		Sinks:   []string{"lodash merge", "deepmerge", "Object.assign on nested input", "set-by-path helpers", "npm scripts", "dynamic require/import"},
		Checks: []string{
			"block __proto__, prototype, and constructor keys in recursive merge/set helpers",
			"review lockfiles, lifecycle scripts, dependency confusion risk, and typosquatting exposure",
			"check plugin loading, dynamic imports, sandbox boundaries, and transitive vulnerable packages",
		},
		References: []string{"CWE-1321", "OWASP ASVS V14"},
	},
	{
		ID: "AUD-RUBY-RAILS", Languages: []string{"ruby"}, Category: "rails", Name: "Ruby/Rails MVC security review",
		Sources: []string{"params", "cookies", "headers", "ActiveJob args", "ActionCable messages", "file uploads"},
		Sinks:   []string{"find_by_sql", "where string fragments", "Arel.sql", "render inline", "ERB", "send_file", "redirect_to", "constantize"},
		Checks: []string{
			"review strong parameters, policy scopes, tenant filters, and object ownership in controllers/services",
			"prefer parameterized ActiveRecord APIs and allowlists for order/select/group fragments",
			"check open redirects, file send paths, template rendering, ActionCable auth, and background jobs",
		},
		References: []string{"CWE-89", "CWE-601", "OWASP ASVS V4"},
	},
	{
		ID: "AUD-RUBY-DESER", Languages: []string{"ruby"}, Category: "deserialization", Name: "Ruby unsafe deserialization and dynamic execution",
		Sources: []string{"cookies", "signed blobs", "YAML config", "queue messages", "cache values"},
		Sinks:   []string{"YAML.load", "Marshal.load", "Oj.load object mode", "constantize", "send", "eval", "system", "Open3"},
		Checks: []string{
			"use safe_load or typed parsing for untrusted YAML and avoid Marshal from user-controlled data",
			"constrain dynamic method/class resolution with explicit allowlists",
			"review shell invocation, environment handling, and argument escaping in maintenance tasks",
		},
		References: []string{"CWE-502", "CWE-94", "CWE-78"},
	},
	{
		ID: "AUD-RUST-UNSAFE", Languages: []string{"rust"}, Category: "memory-safety", Name: "Rust unsafe, FFI, and boundary review",
		Sources: []string{"FFI inputs", "raw bytes", "network parsers", "path/URL params", "environment variables", "unsafe trait implementations"},
		Sinks:   []string{"unsafe blocks", "from_raw_parts", "transmute", "CStr::from_ptr", "CString", "libc calls", "FFI callbacks"},
		Checks: []string{
			"justify every unsafe block with invariants and check lifetime, aliasing, bounds, encoding, and null assumptions",
			"validate FFI ownership, panic boundaries, integer conversions, and error handling across trust boundaries",
			"run or recommend cargo clippy, cargo audit, cargo deny, Miri, sanitizers, or fuzzing where applicable",
		},
		References: []string{"Rustonomicon", "CWE-119", "CWE-190"},
	},
	{
		ID: "AUD-RUST-WEB", Languages: []string{"rust"}, Category: "web", Name: "Rust web/API and data-access review",
		Sources: []string{"actix-web extractors", "axum extractors", "rocket forms", "serde payloads", "headers", "cookies"},
		Sinks:   []string{"sqlx::query raw SQL", "diesel sql_query", "Command", "tera/handlebars templates", "reqwest", "tokio fs"},
		Checks: []string{
			"verify auth extractors and middleware protect every route and tenant/object query",
			"use typed query builders or bind parameters and allowlists for identifiers/order fragments",
			"review SSRF, path traversal, template rendering, panic-to-500 leakage, and secret logging",
		},
		References: []string{"OWASP ASVS", "CWE-89", "CWE-918"},
	},
}

type CodeAuditCatalogTool struct{}

func (t *CodeAuditCatalogTool) Name() string { return "CodeAuditCatalog" }

func (t *CodeAuditCatalogTool) Description() string {
	return "List, search, or show payload-free secure code audit checklists for PHP, Java/JSP, .NET/ASP, JavaScript/Node.js, Ruby, Rust, and cross-language risks."
}

func (t *CodeAuditCatalogTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"action":   map[string]interface{}{"type": "string", "enum": []string{"list", "search", "show"}},
		"language": map[string]interface{}{"type": "string", "description": "Language or platform alias: php, java, jsp, dotnet, .net, asp, js, javascript, nodejs, ruby, rust, or all."},
		"query":    map[string]interface{}{"type": "string"},
		"id":       map[string]interface{}{"type": "string"},
	}, "required": []string{"action"}}
}

func (t *CodeAuditCatalogTool) Execute(_ context.Context, input map[string]interface{}) (*ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(toString(input["action"])))
	language := normalizeAuditLanguage(toString(input["language"]))
	query := strings.ToLower(strings.TrimSpace(toString(input["query"])))
	id := strings.TrimSpace(toString(input["id"]))

	if action != "list" && action != "search" && action != "show" {
		return &ToolResult{Content: "unknown code audit catalog action: " + action, IsError: true}, nil
	}

	var matches []CodeAuditCatalogEntry
	for _, entry := range codeAuditCatalog {
		if action == "show" && !strings.EqualFold(entry.ID, id) {
			continue
		}
		if language != "" && language != "all" && !entryMatchesLanguage(entry, language) {
			continue
		}
		if action == "search" && query != "" {
			data, _ := json.Marshal(entry)
			if !strings.Contains(strings.ToLower(string(data)), query) {
				continue
			}
		}
		matches = append(matches, entry)
	}

	if action == "show" && len(matches) == 0 {
		return &ToolResult{Content: "code audit catalog entry not found: " + id, IsError: true}, nil
	}
	return marshalTool(matches)
}

func entryMatchesLanguage(entry CodeAuditCatalogEntry, language string) bool {
	for _, lang := range entry.Languages {
		normalized := normalizeAuditLanguage(lang)
		if normalized == "all" || normalized == language {
			return true
		}
	}
	return false
}

func normalizeAuditLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "all", "*", "any":
		return strings.ToLower(strings.TrimSpace(language))
	case "js", "javascript", "ecmascript", "typescript", "ts", "frontend":
		return "javascript"
	case "node", "node.js", "nodejs", "express", "koa", "fastify":
		return "nodejs"
	case "java", "spring", "springboot", "spring-boot", "jakarta", "servlet":
		return "java"
	case "jsp", "jspx":
		return "jsp"
	case ".net", "dotnet", "csharp", "c#", "asp.net", "aspx", "mvc", "webforms":
		return "dotnet"
	case "asp", "classic-asp", "vbscript":
		return "asp"
	case "php", "laravel", "symfony", "wordpress", "drupal":
		return "php"
	case "ruby", "rails", "ruby-on-rails":
		return "ruby"
	case "rust", "actix", "axum", "rocket":
		return "rust"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}
