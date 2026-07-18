//go:build webpreview && windows

package main

import "testing"

func TestWebPreviewEventBridgeDoesNotRequireWailsContext(t *testing.T) {
	app := &App{}
	emitPlatformEvent(app, AgentEvent{Name: "server:status"})
}
