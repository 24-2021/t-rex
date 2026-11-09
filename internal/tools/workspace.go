package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/t-rex-ai/t-rex/internal/config"
)

var windowsAbsPathPattern = regexp.MustCompile(`(?i)^[a-z]:[\\/]`)
var shellSegmentPattern = regexp.MustCompile(`[;&|]+`)

func workspaceRoot(cfg *config.Config) (string, error) {
	root := ""
	if cfg != nil {
		root = strings.TrimSpace(cfg.Project.Path)
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(root)
}

func resolveWorkspacePath(cfg *config.Config, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is empty")
	}
	root, err := workspaceRoot(cfg)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	candidate := path
	if !filepath.IsAbs(candidate) && !windowsAbsPathPattern.MatchString(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	absCandidate, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	if !pathWithinRoot(absCandidate, root) {
		return "", fmt.Errorf("path %q is outside workspace %q", absCandidate, root)
	}
	if err := validateProjectPath(cfg, absCandidate, root); err != nil {
		return "", err
	}
	return absCandidate, nil
}

func pathWithinRoot(path string, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateProjectPath(cfg *config.Config, candidate string, root string) error {
	allowed, err := projectPathAllowed(cfg, candidate, root)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("path %q is outside configured project paths", candidate)
	}
	return nil
}

func projectPathAllowed(cfg *config.Config, candidate string, root string) (bool, error) {
	if !pathWithinRoot(candidate, root) {
		return false, nil
	}
	if cfg == nil {
		return true, nil
	}
	for _, rule := range cfg.Project.DeniedPaths {
		if pathMatchesRule(candidate, root, rule) {
			return false, fmt.Errorf("path %q is denied by project policy", candidate)
		}
	}
	if len(cfg.Project.AllowedPaths) == 0 {
		return true, nil
	}
	for _, rule := range cfg.Project.AllowedPaths {
		if pathMatchesRule(candidate, root, rule) {
			return true, nil
		}
	}
	return false, nil
}

func pathMatchesRule(candidate string, root string, rule string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}
	pattern := rule
	if !filepath.IsAbs(pattern) && !windowsAbsPathPattern.MatchString(pattern) {
		pattern = filepath.Join(root, pattern)
	}
	pattern = filepath.ToSlash(pattern)
	candidate = filepath.ToSlash(candidate)
	if !strings.ContainsAny(pattern, "*?[") {
		resolved, err := filepath.Abs(filepath.FromSlash(pattern))
		if err != nil {
			return false
		}
		resolved = filepath.ToSlash(filepath.Clean(resolved))
		return strings.EqualFold(candidate, resolved) || pathWithinRoot(candidate, resolved)
	}
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*`, `.*`)
	expression = strings.ReplaceAll(expression, `\?`, `.`)
	matched, err := regexp.MatchString(`(?i)^`+expression+`(?:/.*)?$`, candidate)
	return err == nil && matched
}

func validateShellWorkspace(cfg *config.Config, cwd string, command string) (string, error) {
	root, err := workspaceRoot(cfg)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = root
	}
	resolvedCwd, err := resolveWorkspacePath(cfg, cwd)
	if err != nil {
		return "", fmt.Errorf("cwd is outside workspace: %w", err)
	}
	if err := validateShellSegments(cfg, root, resolvedCwd, command); err != nil {
		return "", err
	}
	for _, token := range commandPathTokens(command) {
		resolved, err := resolveCommandPathToken(resolvedCwd, token)
		if err != nil {
			return "", err
		}
		if resolved != "" && !pathWithinRoot(resolved, root) {
			return "", fmt.Errorf("command references path outside workspace: %s", token)
		}
		if resolved != "" {
			if err := validateProjectPath(cfg, resolved, root); err != nil {
				return "", fmt.Errorf("command path rejected: %w", err)
			}
		}
	}
	return resolvedCwd, nil
}

func validateShellSegments(cfg *config.Config, root string, cwd string, command string) error {
	for _, segment := range shellSegmentPattern.Split(command, -1) {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		lower := strings.ToLower(segment)
		fields := strings.Fields(segment)
		if len(fields) >= 2 && isChangeDirCommand(fields[0]) {
			target := strings.Trim(fields[1], "'\"`")
			resolved, err := resolveCommandPathToken(cwd, target)
			if err != nil {
				return err
			}
			if !pathWithinRoot(resolved, root) {
				return fmt.Errorf("cd target is outside workspace: %s", target)
			}
			if err := validateProjectPath(cfg, resolved, root); err != nil {
				return fmt.Errorf("cd target rejected: %w", err)
			}
		}
		if strings.Contains(lower, "remove-item") || strings.Contains(lower, "del ") || strings.Contains(lower, "erase ") || strings.Contains(lower, "move-item") || strings.Contains(lower, "copy-item") || strings.HasPrefix(lower, "rm ") || strings.Contains(lower, " rm ") || strings.HasPrefix(lower, "mv ") || strings.Contains(lower, " mv ") || strings.HasPrefix(lower, "cp ") || strings.Contains(lower, " cp ") {
			if err := validateFileOperationTargets(cfg, root, cwd, segment); err != nil {
				return err
			}
			for _, token := range commandPathTokens(segment) {
				resolved, err := resolveCommandPathToken(cwd, token)
				if err != nil {
					return err
				}
				if !pathWithinRoot(resolved, root) {
					return fmt.Errorf("file operation references path outside workspace: %s", token)
				}
			}
		}
		if err := validateRedirectTargets(cfg, root, cwd, segment); err != nil {
			return err
		}
	}
	return nil
}

func validateFileOperationTargets(cfg *config.Config, root string, cwd string, segment string) error {
	fields := strings.Fields(segment)
	for _, field := range fields[1:] {
		token := strings.Trim(field, "'\"`(),;")
		if token == "" || strings.HasPrefix(token, "-") || strings.HasPrefix(token, "$") || strings.Contains(token, "=") {
			continue
		}
		resolved, err := resolveCommandPathToken(cwd, token)
		if err != nil {
			return err
		}
		if !pathWithinRoot(resolved, root) {
			return fmt.Errorf("file operation references path outside workspace: %s", token)
		}
		if err := validateProjectPath(cfg, resolved, root); err != nil {
			return fmt.Errorf("file operation target rejected: %w", err)
		}
	}
	return nil
}

func isChangeDirCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	return command == "cd" || command == "chdir" || command == "set-location" || command == "sl"
}

func validateRedirectTargets(cfg *config.Config, root string, cwd string, segment string) error {
	fields := strings.Fields(segment)
	for i, field := range fields {
		if field == ">" || field == ">>" || field == "1>" || field == "2>" {
			if i+1 >= len(fields) {
				continue
			}
			target := strings.Trim(fields[i+1], "'\"`")
			resolved, err := resolveCommandPathToken(cwd, target)
			if err != nil {
				return err
			}
			if !pathWithinRoot(resolved, root) {
				return fmt.Errorf("redirect target is outside workspace: %s", target)
			}
			if err := validateProjectPath(cfg, resolved, root); err != nil {
				return fmt.Errorf("redirect target rejected: %w", err)
			}
		}
		for _, prefix := range []string{">", ">>", "1>", "2>"} {
			if strings.HasPrefix(field, prefix) && len(field) > len(prefix) {
				target := strings.Trim(field[len(prefix):], "'\"`")
				resolved, err := resolveCommandPathToken(cwd, target)
				if err != nil {
					return err
				}
				if !pathWithinRoot(resolved, root) {
					return fmt.Errorf("redirect target is outside workspace: %s", target)
				}
				if err := validateProjectPath(cfg, resolved, root); err != nil {
					return fmt.Errorf("redirect target rejected: %w", err)
				}
			}
		}
	}
	return nil
}

func commandPathTokens(command string) []string {
	fields := strings.Fields(command)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "'\"`(),;<>")
		if field == "" || strings.Contains(field, "://") || strings.HasPrefix(field, "-") {
			continue
		}
		if i := strings.Index(field, "="); i >= 0 && i+1 < len(field) {
			field = strings.Trim(field[i+1:], "'\"`")
		}
		if looksLikePath(field) {
			tokens = append(tokens, field)
		}
	}
	return tokens
}

func looksLikePath(token string) bool {
	return filepath.IsAbs(token) ||
		windowsAbsPathPattern.MatchString(token) ||
		strings.HasPrefix(token, "~") ||
		token == ".." ||
		strings.HasPrefix(token, "../") ||
		strings.HasPrefix(token, `..\`) ||
		strings.Contains(token, "/../") ||
		strings.Contains(token, `\..\`)
}

func resolveCommandPathToken(cwd string, token string) (string, error) {
	if strings.HasPrefix(token, "~") {
		return "", fmt.Errorf("command references home-relative path outside workspace: %s", token)
	}
	if windowsAbsPathPattern.MatchString(token) || filepath.IsAbs(token) {
		return filepath.Abs(filepath.Clean(token))
	}
	return filepath.Abs(filepath.Clean(filepath.Join(cwd, token)))
}
