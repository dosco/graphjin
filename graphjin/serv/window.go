package serv

import (
	"fmt"
	"strings"
	"time"
)

func parseClampedWindow(text string, minWindow, maxWindow time.Duration) (time.Duration, error) {
	window, err := time.ParseDuration(strings.TrimSpace(text))
	if err != nil {
		return 0, err
	}
	if minWindow > 0 && window < minWindow {
		window = minWindow
	}
	if maxWindow > 0 && window > maxWindow {
		window = maxWindow
	}
	if minWindow > 0 && maxWindow > 0 && minWindow > maxWindow {
		return 0, fmt.Errorf("minimum window exceeds maximum window")
	}
	return window, nil
}
