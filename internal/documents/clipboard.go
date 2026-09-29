package documents

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"tango/internal/mjson"
	"tango/internal/state"
)

// CopyToClipboard copies the content of the detail view (the expanded
// document, in re-parseable shell syntax) to the system clipboard.
func CopyToClipboard() {
	s := state.S
	if len(s.DetailTokens) == 0 {
		return
	}
	text := mjson.Join(s.DetailTokens)
	state.Async("", func(ctx context.Context) func() {
		err := copyText(ctx, text)
		return func() {
			if err != nil {
				state.FlashError("Clipboard error: " + err.Error())
				return
			}
			state.FlashStatus(" [green]Copied to clipboard")
		}
	})
}

func clipboardCommands() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	}
	var cmds [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cmds = append(cmds, []string{"wl-copy"})
	}
	return append(cmds,
		[]string{"xclip", "-selection", "clipboard"},
		[]string{"xsel", "--clipboard", "--input"},
		[]string{"clip.exe"},
	)
}

func copyText(ctx context.Context, text string) error {
	for _, c := range clipboardCommands() {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v %s", c[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Fall back to the OSC 52 terminal escape (works over SSH in most terminals).
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("no clipboard tool found (install pbcopy, wl-copy, xclip or xsel)")
	}
	defer func() { _ = tty.Close() }()
	_, err = fmt.Fprintf(tty, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}
