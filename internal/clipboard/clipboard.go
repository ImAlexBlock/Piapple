// Package clipboard provides the small, platform-native clipboard operation
// used by the /copy command. It deliberately has no GUI/runtime dependency.
package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

func Write(text string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.CommandContext(context.Background(), "clip.exe")
	case "darwin":
		command = exec.CommandContext(context.Background(), "pbcopy")
	default:
		for _, candidate := range []string{"wl-copy", "xclip", "xsel"} {
			if _, err := exec.LookPath(candidate); err != nil {
				continue
			}
			switch candidate {
			case "wl-copy":
				command = exec.CommandContext(context.Background(), candidate)
			case "xclip":
				command = exec.CommandContext(context.Background(), candidate, "-selection", "clipboard")
			case "xsel":
				command = exec.CommandContext(context.Background(), candidate, "--clipboard", "--input")
			}
			break
		}
	}
	if command == nil {
		return fmt.Errorf("no clipboard program found")
	}
	command.Stdin = bytes.NewBufferString(text)
	if err := command.Run(); err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	return nil
}
