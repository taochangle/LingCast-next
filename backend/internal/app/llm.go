package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type ReplyDecision struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
}

func appendViewerTurn(history []ViewerTurn, event ViewerTurn) []ViewerTurn {
	if len(history) >= 20 {
		history = history[len(history)-19:]
	}
	return append(append([]ViewerTurn(nil), history...), event)
}

// Reject explicit requests for control or secrets before the viewer turn can
// enter conversational history. The independent audit handles broader cases.
func explicitViewerDirective(text string) bool {
	normalized := strings.ToLower(text)
	for _, term := range []string{"忽略前令", "忽略之前的指令", "泄露系统提示词", "输出系统提示词", "ignore previous instructions", "reveal the system prompt", "修改配置", "修改设置"} {
		if strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func (a *App) BuildInteractionReply(ctx context.Context, in HostInput, event ViewerTurn) (ReplyDecision, error) {
	skip := ReplyDecision{Action: "skip"}
	if event.Kind != "comment" || strings.TrimSpace(event.Text) == "" || utf8.RuneCountInString(event.Text) > 500 || utf8.RuneCountInString(event.Nickname) > 80 {
		return skip, nil
	}
	if explicitViewerDirective(event.Nickname) || explicitViewerDirective(event.Text) {
		return skip, nil
	}
	in.ServerTime = a.c.Clock().In(a.loc)
	in.Timezone = a.c.Timezone
	in.ViewerTurns = appendViewerTurn(in.ViewerTurns, event)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		text, err := a.generateSpeech(ctx, in, "interaction")
		if errors.Is(err, errModelOperation) {
			return skip, nil
		}
		if errors.Is(err, errSpeechFormat) {
			continue
		}
		if err != nil || text == "" {
			return skip, err
		}
		decision, err := a.CheckGeneratedSpeech(ctx, SpeechCandidate{Source: "interaction", Text: text, Context: in})
		if errors.Is(err, errModelOperation) {
			return skip, nil
		}
		if err != nil {
			return skip, err
		}
		if decision.Allow {
			return ReplyDecision{Action: "speak", Text: text}, nil
		}
		if decision.Category == "injection" || decision.Category == "secret-exfiltration" {
			return skip, nil
		}
	}
	return skip, nil
}
