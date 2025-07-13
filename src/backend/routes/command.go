package routes

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ----------------------------------------------------------------------------
// CUSTOM ERRORS
// ----------------------------------------------------------------------------

type ErrEmptyMessage struct {
	Author string
}

func (e *ErrEmptyMessage) Error() string {
	return fmt.Sprintf("message is empty. author: %s", e.Author)
}

type ErrNotACommand struct {
	Author  string
	Message string
}

func (e *ErrNotACommand) Error() string {
	return fmt.Sprintf("message not a command. author: %s; message: %s", e.Author, e.Message)
}

type CommandParser struct {
	HelpTimer         *time.Timer
	HelpResetDuration time.Duration
}

// ----------------------------------------------------------------------------
// COMMANDS
// ----------------------------------------------------------------------------
//
// When creating a new command, be sure to create a help message function.

// Names for all valid text commands
var TextCommand = map[string]struct{}{
	"color": {},
	"help":  {},
}

// Aliases are alternative names for commands which get converted by the tokenizer
var TextCommandAlias = map[string]string{
	"colour": "color",
	"h":      "help",
}

// ----------------------------------------------------------------------------
// HELP MESSAGES
// ----------------------------------------------------------------------------
//
// Every command must have a corresponding help message function detailing
// the Usage and Options.
//
// Help messages are only sent when the parser's help timer is finished.

func ColorHelp() string {
	sb := strings.Builder{}

	sb.WriteString("Usage: !color [color]. Colors: ")
	for color := range NameColors {
		sb.WriteString(color)
		sb.WriteByte(' ')
	}

	return sb.String()
}

func HelpHelp() string {
	sb := strings.Builder{}

	sb.WriteString("Usage: !help [command]. Commands: ")
	for cmd := range TextCommand {
		sb.WriteString(cmd)
		sb.WriteByte(' ')
	}

	return sb.String()
}

// Maps each command to a help function.
var CommandHelp = map[string]func() string{
	"color": ColorHelp,
	"help":  HelpHelp,
}

// ----------------------------------------------------------------------------
// CONSTANTS
// ----------------------------------------------------------------------------

// Names of colors defined by the frontend.
var NameColors = map[string]struct{}{
	"red":       {},
	"orange":    {},
	"yellow":    {},
	"green":     {},
	"lightblue": {},
	"blue":      {},
	"violet":    {},
	"pink":      {},
	"tan":       {},
	"olive":     {},
	"lime":      {},
	"sky":       {},
	"purple":    {},
}

// ----------------------------------------------------------------------------
// PARSER METHODS
// ----------------------------------------------------------------------------

// Creates a response message directly from EloraChat for all to see.
func (cp CommandParser) CreateResponse(r string) Message {
	var m Message

	m.Author = "EloraChat"
	m.Colour = "#FFCD05"
	m.Message = r
	m.Tokens = []Token{{
		Type: TokenTypeText,
		Text: r,
	}}
	// TODO: Create custom elora badge and link
	m.Badges = []Badge{}
	m.Emotes = []Emote{}

	return m
}

// Parse commands from message, potentially transforming the message.
// TODO: Replace userSetting with database ptr and update table instead of map.
func (cp CommandParser) Parse(m Message, userSetting map[string]string) (Message, error) {
	if m.Tokens == nil || len(m.Tokens) < 1 {
		return m, &ErrEmptyMessage{m.Author}
	} else if m.Tokens[0].Type != TokenTypeCommand {
		return m, &ErrNotACommand{m.Author, m.Message}
	} else if userSetting == nil {
		return m, errors.New("userSetting map is nil")
	}

	cmd, optsStr, _ := strings.Cut(m.Tokens[0].Text, " ")
	opts := strings.Split(optsStr, " ")

	switch cmd {
	case "color":
		if len(opts) == 0 || (len(opts) == 1 && opts[0] == "") {
			select {
			case <-cp.HelpTimer.C:
				cp.HelpTimer.Reset(cp.HelpResetDuration)
				return cp.CreateResponse(ColorHelp()), nil
			default:
				return m, nil
			}
		}
		color := opts[0]
		if _, ok := NameColors[color]; ok {
			userSetting[m.Author] = color
		}
	case "help":
		select {
		case <-cp.HelpTimer.C:
			cp.HelpTimer.Reset(cp.HelpResetDuration)
			if len(opts) == 0 || (len(opts) == 1 && opts[0] == "") {
				return cp.CreateResponse(HelpHelp()), nil
			}
			helpCmd := opts[0]
			if _, ok := CommandHelp[helpCmd]; ok {
				return cp.CreateResponse(CommandHelp[helpCmd]()), nil
			}
		default:
			return m, nil
		}
	}

	return m, nil
}
