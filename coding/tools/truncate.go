package tools

import (
	"fmt"
	"strings"
)

type truncationResult struct {
	Content               string
	Truncated             bool
	TruncatedBy           string
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

func (t truncationResult) toMap() map[string]any {
	return map[string]any{
		"truncated":             t.Truncated,
		"truncatedBy":           t.TruncatedBy,
		"totalLines":            t.TotalLines,
		"totalBytes":            t.TotalBytes,
		"outputLines":           t.OutputLines,
		"outputBytes":           t.OutputBytes,
		"lastLinePartial":       t.LastLinePartial,
		"firstLineExceedsLimit": t.FirstLineExceedsLimit,
		"maxLines":              t.MaxLines,
		"maxBytes":              t.MaxBytes,
	}
}

func truncateHead(content string, maxLines, maxBytes int) truncationResult {
	totalBytes := byteLen(content)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: "",
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	if totalLines > 0 && byteLen(lines[0]) > maxBytes {
		return truncationResult{
			Content:               "",
			Truncated:             true,
			TruncatedBy:           "bytes",
			TotalLines:            totalLines,
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			FirstLineExceedsLimit: true,
			MaxLines:              maxLines,
			MaxBytes:              maxBytes,
		}
	}

	out := []string{}
	outBytes := 0
	truncatedBy := "lines"
	for i := 0; i < len(lines) && i < maxLines; i++ {
		line := lines[i]
		lineBytes := byteLen(line)
		if i > 0 {
			lineBytes++
		}
		if outBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		out = append(out, line)
		outBytes += lineBytes
	}
	if len(out) >= maxLines && outBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := strings.Join(out, "\n")
	return truncationResult{
		Content:     outContent,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		OutputLines: len(out),
		OutputBytes: byteLen(outContent),
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

func truncateTail(content string, maxLines, maxBytes int) truncationResult {
	totalBytes := byteLen(content)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: "",
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	out := []string{}
	outBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		line := lines[i]
		lineBytes := byteLen(line)
		if len(out) > 0 {
			lineBytes++
		}
		if outBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(out) == 0 {
				truncated := truncateStringToBytesFromEnd(line, maxBytes)
				out = append([]string{truncated}, out...)
				outBytes = byteLen(truncated)
				lastLinePartial = true
			}
			break
		}
		out = append([]string{line}, out...)
		outBytes += lineBytes
	}
	if len(out) >= maxLines && outBytes <= maxBytes {
		truncatedBy = "lines"
	}

	outContent := strings.Join(out, "\n")
	return truncationResult{
		Content:         outContent,
		Truncated:       true,
		TruncatedBy:     truncatedBy,
		TotalLines:      totalLines,
		TotalBytes:      totalBytes,
		OutputLines:     len(out),
		OutputBytes:     byteLen(outContent),
		LastLinePartial: lastLinePartial,
		MaxLines:        maxLines,
		MaxBytes:        maxBytes,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	raw := []byte(s)
	if len(raw) <= maxBytes {
		return s
	}
	start := len(raw) - maxBytes
	for start < len(raw) && (raw[start]&0xC0) == 0x80 {
		start++
	}
	return string(raw[start:])
}

func byteLen(s string) int {
	return len([]byte(s))
}

func formatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024.0)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024.0*1024.0))
}
