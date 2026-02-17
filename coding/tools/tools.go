package tools

import "github.com/zahlmann/phi/agent"

const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024
)

func NewCodingTools(cwd string) []agent.Tool {
	return []agent.Tool{
		NewWriteFileTool(cwd),
		NewReadFileTool(cwd),
		NewEditTool(cwd),
		NewBashTool(cwd, 0),
	}
}
