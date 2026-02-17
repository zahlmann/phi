package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultCWD(cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		return cwd
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func resolveSafePath(cwd, input string) (string, error) {
	base, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}

	target := input
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}

	sep := string(os.PathSeparator)
	if target != base && !strings.HasPrefix(target, base+sep) {
		return "", fmt.Errorf("path escapes working directory: %s", input)
	}
	return target, nil
}

func toStringArg(args map[string]any, key string) (string, bool) {
	raw, ok := args[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	default:
		return fmt.Sprintf("%v", raw), true
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
