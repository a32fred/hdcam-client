package diagnostic

import "os/exec"

type ToolResult struct {
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
}

func CheckTools(names []string) []ToolResult {
	results := make([]ToolResult, len(names))
	for i, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			results[i] = ToolResult{Name: name, Path: path, Available: true}
		} else {
			results[i] = ToolResult{Name: name, Available: false}
		}
	}
	return results
}
