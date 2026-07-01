package usagereporter

import (
	"fmt"
	"strings"
)

func CollectUsage(source string, root string, state *State, acceptWindowDays int) (*CollectResult, error) {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case SourceCodex:
		return CollectCodexUsageWithWindow(root, state, acceptWindowDays)
	case "zcode", "minimax_code":
		return nil, fmt.Errorf("usage collector for source %q is not implemented yet", source)
	default:
		return nil, fmt.Errorf("unsupported usage source %q", source)
	}
}
