package routes

import (
	"bufio"
	"bytes"
	"iter"
	"strings"
)

const (
	TokenTypeText    = "text"
	TokenTypeEmote   = "emote"
	TokenTypeColour  = "colour"
	TokenTypeEffect  = "effect"
	TokenTypePattern = "pattern"
	TokenTypeCommand = "command"
)

var TextEffects = map[string]struct{}{
	"wave":    {},
	"wave2":   {},
	"shake":   {},
	"slide":   {},
	"scroll":  {},
	"cheddar": {},
}

var TextColours = map[string]struct{}{
	"yellow":  {},
	"red":     {},
	"green":   {},
	"cyan":    {},
	"purple":  {},
	"white":   {},
	"flash1":  {},
	"flash2":  {},
	"flash3":  {},
	"glow1":   {},
	"glow2":   {},
	"glow3":   {},
	"rainbow": {},
}

type Token struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emote Emote  `json:"emote"`
}

type Tokenizer struct {
	EmoteCache        map[string]Emote
	TextEffectSep     byte
	TextCommandPrefix byte
}

// ScanSeparator returns a split function for a [Scanner] that returns text separated
// by a separator byte as tokens (including surrounding separators).
func ScanSeparator(sep byte) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		// :: -> : (If double sep, jump to next sep)
		if len(data) >= 2 && data[1] == sep {
			return 1, data[:1], nil
		}
		// If we start with a sep, also include the final sep
		offset := 0
		if data[0] == sep {
			offset = 1
		}
		end := bytes.Index(data[1:], []byte{sep}) + 1
		// ending sep not found, just return the rest
		if end <= 0 {
			return len(data), data, nil
		}
		return end + offset, data[:end+offset], nil
	}
}

// Recursively check for the presence of colors and effects
//
// Formats:
//
//	color:text
//	effect:text
//	color:effect:text
//	effect:colour:text
//
// Returns: (yield stop, last word (text only))
func (p Tokenizer) iterWordEffect(yield func(Token) bool, word string, depth int) (bool, string) {

	// Base Case: Empty string
	if word == "" {
		return false, ""
	}

	tok := Token{
		Type: TokenTypeText,
		Text: word,
		Emote: Emote{
			Locations: []string{},
			Images:    []Image{},
		},
	}

	// Base Case: Emote
	if emote, ok := p.EmoteCache[word]; ok {
		tok.Type = TokenTypeEmote
		tok.Emote = emote
		return !yield(tok), ""
	}

	// Base Case: Depth limit
	if depth == 2 {
		return false, word
	}

	prefix, postfix, sepFound := strings.Cut(word, string(p.TextEffectSep))

	// No effects found, return word
	if !sepFound || prefix == "" {
		return false, word
	}

	// Look for color, effect, or pattern
	if _, ok := TextColours[prefix]; ok {
		tok.Type = TokenTypeColour
		tok.Text = prefix
	} else if _, ok := TextEffects[prefix]; ok {
		tok.Type = TokenTypeEffect
		tok.Text = prefix
	} else if 7 <= len(prefix) && len(prefix) <= 15 && prefix[:7] == "pattern" {
		// Note: Len("pattern...ops") >= 8 and pattern opcode max length is 8.
		if len(prefix) == 7 {
			return false, word[8:]
		}
		tok.Type = TokenTypePattern
		tok.Text = prefix[7:]
	} else {
		return false, word
	}

	if !yield(tok) {
		return true, ""
	}

	// Recursively tokenize next effect
	return p.iterWordEffect(yield, postfix, depth+1)
}

// Helper to iterate over YouTube style emotes
func (p Tokenizer) iterYoutube(yield func(Token) bool, word string, sb strings.Builder) strings.Builder {
	scanner := bufio.NewScanner(strings.NewReader(word))
	scanner.Split(ScanSeparator(':'))

	// Iterate over potential emotes [:emote:] (scanning over colons)
	tok := Token{
		Type: TokenTypeText,
		Emote: Emote{
			Locations: []string{},
			Images:    []Image{},
		},
	}
	for scanner.Scan() {
		text := scanner.Text()
		// YouTube emote found
		if emote, ok := p.EmoteCache[text]; ok && text[0] == ':' {
			// yield text before emote
			tok.Text = strings.TrimSpace(sb.String())
			if tok.Text != "" {
				if !yield(tok) {
					return sb
				}
			}
			tok.Type = TokenTypeEmote
			tok.Text = text
			tok.Emote = emote
			if !yield(tok) {
				return sb
			}
			sb.Reset()
		} else {
			tok.Type = TokenTypeText
			tok.Emote = Emote{
				Locations: []string{},
				Images:    []Image{},
			}
			sb.WriteString(text)
		}
	}
	sb.WriteByte(' ')

	return sb
}

// Returns an iterator over the string which yields tokens.
func (p Tokenizer) Iter(s string) iter.Seq[Token] {
	return func(yield func(Token) bool) {
		var sb strings.Builder
		scanner := bufio.NewScanner(strings.NewReader(s))
		scanner.Split(bufio.ScanWords)

		tok := Token{
			Type: TokenTypeText,
			Emote: Emote{
				Locations: []string{},
				Images:    []Image{},
			},
		}

		ok := scanner.Scan()
		if !ok {
			return
		}

		word := scanner.Text()

		// Check for command
		// Exits early if command prefix is detected at start of string
		if len(word) > 1 && word[0] == p.TextCommandPrefix {
			command := word[1:]
			if officialCommand, isAlias := TextCommandAlias[command]; isAlias {
				command = officialCommand
			}
			if _, ok := TextCommand[command]; ok {
				tok.Type = TokenTypeCommand
				tok.Text = strings.TrimSpace(command + s[len(word):])
			} else {
				tok.Text = strings.TrimSpace(s)
			}
			yield(tok)
			return
		}

		// Recursively tokenize text effects
		stop, lastWord := p.iterWordEffect(yield, word, 0)
		if stop {
			return
		}

		// Scan last word for youtube emotes
		sb = p.iterYoutube(yield, lastWord, sb)

		// Scan the rest of the message for emotes
		for scanner.Scan() {
			word = scanner.Text()

			// Check for emote
			if emote, ok := p.EmoteCache[word]; ok {
				// yield text before emote
				tok.Text = strings.TrimSpace(sb.String())
				if tok.Text != "" {
					if !yield(tok) {
						return
					}
				}
				tok.Type = TokenTypeEmote
				tok.Text = word
				tok.Emote = emote
				if !yield(tok) {
					return
				}
				sb.Reset()
			} else {
				tok.Type = TokenTypeText
				tok.Emote = Emote{
					Locations: []string{},
					Images:    []Image{},
				}
				sb = p.iterYoutube(yield, word, sb)
			}
		}

		tok.Text = strings.TrimSpace(sb.String())

		// yield remaining text at end of message scan
		if tok.Text != "" {
			if !yield(tok) {
				return
			}
		}
	}
}
