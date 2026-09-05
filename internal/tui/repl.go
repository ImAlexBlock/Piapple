package tui

import (
	"bufio"
	"context"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"io"
	"strings"
)

type Runner struct {
	Loop       *agent.Loop
	In         io.Reader
	Out        io.Writer
	Transcript []agent.Message
	Persist    func([]agent.Message) error
}

func (r *Runner) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(r.In)
	fmt.Fprintln(r.Out, "Piapple interactive mode. Type /help for commands.")
	for {
		fmt.Fprint(r.Out, "piapple> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case "/exit", "/quit":
			return nil
		case "/help":
			fmt.Fprintln(r.Out, "/help  /clear  /exit\nAny other text is sent to the agent.")
			continue
		case "/clear":
			r.Transcript = nil
			fmt.Fprintln(r.Out, "Conversation cleared.")
			continue
		}
		before := len(r.Transcript)
		r.Transcript = append(r.Transcript, agent.Message{Role: "user", Content: line})
		next, answer, err := r.Loop.Run(ctx, r.Transcript)
		r.Transcript = next
		if r.Persist != nil {
			if persistErr := r.Persist(r.Transcript[before:]); persistErr != nil {
				return persistErr
			}
		}
		if err != nil {
			fmt.Fprintln(r.Out, "error:", err)
			continue
		}
		fmt.Fprintln(r.Out, answer)
	}
}
