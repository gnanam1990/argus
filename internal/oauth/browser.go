package oauth

import (
	"os/exec"
	"runtime"
)

// OpenURL opens url in the default browser. The loopback flow does not call it;
// the CLI does, falling back to printing the URL when it fails.
func OpenURL(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	return exec.Command(cmd, args...).Start()
}
