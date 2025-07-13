package routes

import (
	"bufio"
	"iter"
	"strings"
	"testing"
)

func TestTokenizer(t *testing.T) {
	tokenizer := Tokenizer{
		EmoteCache: map[string]Emote{
			"Clap": {
				ID:     "1",
				Name:   "Clap",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/1/1x.webp"}},
			},
			"Clap2": {
				ID:     "2",
				Name:   "Clap2",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/2/1x.webp"}},
			},
			"KEKW": {
				ID:     "3",
				Name:   "KEKW",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/3/1x.webp"}},
			},
			"FeelsGoodMan": {
				ID:     "4",
				Name:   "FeelsGoodMan",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/4/1x.webp"}},
			},
			":goat-turqouise-white-horns:": {
				ID:     "5",
				Name:   ":goat-turqouise-white-horns:",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/5/1x.webp"}},
			},
			":_DayoHog:": {
				ID:     "6",
				Name:   ":_DayoHog:",
				Images: []Image{{URL: "https://cdn.test.net/v1/emotes/6/1x.webp"}},
			},
		},
		TextEffectSep:     ':',
		TextCommandPrefix: '!',
	}
	type Test struct {
		Name     string
		Message  string
		Expected []Token
	}
	// Test effect parsing regardless of whitespace conditions
	effectTests := []Test{
		{
			Name:    "colourEmpty",
			Message: "white:",
			Expected: []Token{
				{TokenTypeColour, "white", Emote{}},
			},
		},
		{
			Name:    "<WS>colourEmpty",
			Message: "    white:",
			Expected: []Token{
				{TokenTypeColour, "white", Emote{}},
			},
		},
		{
			Name:    "effectEmpty",
			Message: "wave2:",
			Expected: []Token{
				{TokenTypeEffect, "wave2", Emote{}},
			},
		},
		{
			Name:    "colour:text",
			Message: "white:text",
			Expected: []Token{
				{TokenTypeColour, "white", Emote{}},
				{TokenTypeText, "text", Emote{}},
			},
		},
		{
			Name:    "color:<WS>text",
			Message: "white:  text",
			Expected: []Token{
				{TokenTypeColour, "white", Emote{}},
				{TokenTypeText, "text", Emote{}},
			},
		},
		{
			Name:    "color:<WS>emote",
			Message: "white:  Clap2",
			Expected: []Token{
				{TokenTypeColour, "white", Emote{}},
				{TokenTypeEmote, "Clap2", tokenizer.EmoteCache["Clap2"]},
			},
		},
		{
			Name:    "color:effect:emote",
			Message: "rainbow:wave2:KEKW",
			Expected: []Token{
				{TokenTypeColour, "rainbow", Emote{}},
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeEmote, "KEKW", tokenizer.EmoteCache["KEKW"]},
			},
		},
		{
			Name:    "effect:color:emote",
			Message: "wave2:rainbow:KEKW",
			Expected: []Token{
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeColour, "rainbow", Emote{}},
				{TokenTypeEmote, "KEKW", tokenizer.EmoteCache["KEKW"]},
			},
		},
		{
			Name:    "effect:color:<WS>emote",
			Message: "wave2:rainbow:   KEKW",
			Expected: []Token{
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeColour, "rainbow", Emote{}},
				{TokenTypeEmote, "KEKW", tokenizer.EmoteCache["KEKW"]},
			},
		},
		{
			Name:    "effect:emote:emote",
			Message: "wave2:KEKW:KEKW",
			Expected: []Token{
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeText, "KEKW:KEKW", Emote{}},
			},
		},
		{
			Name:    "leadingSep",
			Message: ":cyan:text",
			Expected: []Token{
				{TokenTypeText, ":cyan:text", Emote{}},
			},
		},
		{
			Name:    "manySep",
			Message: ":::::::::",
			Expected: []Token{
				{TokenTypeText, ":::::::::", Emote{}},
			},
		},
		{
			Name:    "effect:manySep",
			Message: "wave2:::::::::",
			Expected: []Token{
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeText, "::::::::", Emote{}},
			},
		},
		{
			Name:    "semicolons",
			Message: ":jaja::#:!@#:",
			Expected: []Token{
				{TokenTypeText, ":jaja::#:!@#:", Emote{}},
			},
		},
		{
			Name:    "semicolonEmpty",
			Message: ":   ",
			Expected: []Token{
				{TokenTypeText, ":", Emote{}},
			},
		},
		{
			Name:    "patternNoOps",
			Message: "pattern:I am a bumblebee!!!",
			Expected: []Token{
				{TokenTypeText, "I am a bumblebee!!!", Emote{}},
			},
		},
		{
			Name:     "patternEmpty",
			Message:  "pattern:",
			Expected: []Token{},
		},
		{
			Name:    "patternMax",
			Message: "patternq3q3q3q3:I am a bumblebee!!!",
			Expected: []Token{
				{TokenTypePattern, "q3q3q3q3", Emote{}},
				{TokenTypeText, "I am a bumblebee!!!", Emote{}},
			},
		},
		{
			Name:    "patternOverMax",
			Message: "patternq3q3q3q3q:I am a bumblebee!!!",
			Expected: []Token{
				{TokenTypeText, "patternq3q3q3q3q:I am a bumblebee!!!", Emote{}},
			},
		},
	}
	iterTests := []Test{
		{
			Name:     "empty",
			Message:  "",
			Expected: []Token{},
		},
		{
			Name:     "ws",
			Message:  "  \n\t\r",
			Expected: []Token{},
		},
		{
			Name:    "randomWS",
			Message: "  2[qrp]3-4t[    #(YT$ jd  ",
			Expected: []Token{
				{TokenTypeText, "2[qrp]3-4t[ #(YT$ jd", Emote{}},
			},
		},
		{
			Name:    "emotesWS",
			Message: "KEKW KEKW    FeelsGoodMan  !!!",
			Expected: []Token{
				{TokenTypeEmote, "KEKW", tokenizer.EmoteCache["KEKW"]},
				{TokenTypeEmote, "KEKW", tokenizer.EmoteCache["KEKW"]},
				{TokenTypeEmote, "FeelsGoodMan", tokenizer.EmoteCache["FeelsGoodMan"]},
				{TokenTypeText, "!!!", Emote{}},
			},
		},
		{
			Name:    "emotesSmashed",
			Message: "KEKWKEKWFeelsGoodMan!",
			Expected: []Token{
				{TokenTypeText, "KEKWKEKWFeelsGoodMan!", Emote{}},
			},
		},
		{
			Name:    "colonNoEffect",
			Message: "Hey, you guys know about Gunz: The Duel?",
			Expected: []Token{
				{TokenTypeText, "Hey, you guys know about Gunz: The Duel?", Emote{}},
			},
		},
		{
			Name:    "colonEffectTypo",
			Message: "gren:This is green!",
			Expected: []Token{
				{TokenTypeText, "gren:This is green!", Emote{}},
			},
		},
		{
			Name:    "emoteSolo",
			Message: "Clap",
			Expected: []Token{
				{TokenTypeEmote, "Clap", tokenizer.EmoteCache["Clap"]},
			},
		},
	}

	iterYouTubeTests := []Test{
		{
			Name:    "emote",
			Message: ":goat-turqouise-white-horns:",
			Expected: []Token{
				{TokenTypeEmote, ":goat-turqouise-white-horns:", tokenizer.EmoteCache[":goat-turqouise-white-horns:"]},
			},
		},
		{
			Name:    "emoteExtraColon",
			Message: "::goat-turqouise-white-horns:",
			Expected: []Token{
				{TokenTypeText, ":", Emote{}},
				{TokenTypeEmote, ":goat-turqouise-white-horns:", tokenizer.EmoteCache[":goat-turqouise-white-horns:"]},
			},
		},
		{
			Name:    "emoteManyColon",
			Message: ":::slk:j::goat-turqouise-white-horns::fj::fd:::",
			Expected: []Token{
				{TokenTypeText, ":::slk:j:", Emote{}},
				{TokenTypeEmote, ":goat-turqouise-white-horns:", tokenizer.EmoteCache[":goat-turqouise-white-horns:"]},
				{TokenTypeText, ":fj::fd:::", Emote{}},
			},
		},
		{
			Name:    "emotes",
			Message: ":_DayoHog::_DayoHog::_DayoHog:",
			Expected: []Token{
				{TokenTypeEmote, ":_DayoHog:", tokenizer.EmoteCache[":_DayoHog:"]},
				{TokenTypeEmote, ":_DayoHog:", tokenizer.EmoteCache[":_DayoHog:"]},
				{TokenTypeEmote, ":_DayoHog:", tokenizer.EmoteCache[":_DayoHog:"]},
			},
		},
		{
			Name:    "effectEmotes",
			Message: "patternq3q3q3q3:wave2::goat-turqouise-white-horns::_DayoHog:",
			Expected: []Token{
				{TokenTypePattern, "q3q3q3q3", Emote{}},
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeEmote, ":goat-turqouise-white-horns:", tokenizer.EmoteCache[":goat-turqouise-white-horns:"]},
				{TokenTypeEmote, ":_DayoHog:", tokenizer.EmoteCache[":_DayoHog:"]},
			},
		},
		{
			Name:    "effectTextEmotes",
			Message: "cyan:wave2: Lets Go! :goat-turqouise-white-horns: Woo :_DayoHog:",
			Expected: []Token{
				{TokenTypeColour, "cyan", Emote{}},
				{TokenTypeEffect, "wave2", Emote{}},
				{TokenTypeText, "Lets Go!", Emote{}},
				{TokenTypeEmote, ":goat-turqouise-white-horns:", tokenizer.EmoteCache[":goat-turqouise-white-horns:"]},
				{TokenTypeText, "Woo", Emote{}},
				{TokenTypeEmote, ":_DayoHog:", tokenizer.EmoteCache[":_DayoHog:"]},
			},
		},
	}

	iterCommandTests := []Test{
		{
			Name:    "emptyCommand",
			Message: "!  ",
			Expected: []Token{
				{TokenTypeText, "!", Emote{}},
			},
		},
		{
			Name:    "invalidCommand",
			Message: "!anInvalidCommand",
			Expected: []Token{
				{TokenTypeText, "!anInvalidCommand", Emote{}},
			},
		},
		{
			Name:    "colorCommand",
			Message: "!color purple",
			Expected: []Token{
				{TokenTypeCommand, "color purple", Emote{}},
			},
		},
	}

	// Helper to run iterator tests
	RunIterTest := func(t *testing.T, iterator iter.Seq[Token], test Test) {
		i := 0
		fail := false
		tokens := []Token{}
		for tok := range iterator {
			expected := test.Expected[i]
			if (tok.Text != expected.Text) ||
				(tok.Type != expected.Type) ||
				(tok.Emote.ID != expected.Emote.ID) ||
				(tok.Emote.Name != expected.Emote.Name) ||
				len(tok.Emote.Locations) != len(expected.Emote.Locations) {
				fail = true
			}
			if !fail {
				for i, loc := range tok.Emote.Locations {
					if loc != expected.Emote.Locations[i] {
						fail = true
					}
				}
			}
			tokens = append(tokens, tok)
			i++
		}
		if fail {
			t.Logf("\n\nMessage:  [ %s ]\nExpected: %v\nGot:      %v\n\n", test.Message, test.Expected, tokens)
			t.Fail()
		}
	}

	for _, test := range effectTests {
		t.Run("Iter-"+test.Name, func(t *testing.T) {
			RunIterTest(t, tokenizer.Iter(test.Message), test)
		})
	}
	for _, test := range iterTests {
		t.Run("Iter-"+test.Name, func(t *testing.T) {
			RunIterTest(t, tokenizer.Iter(test.Message), test)
		})
	}
	for _, test := range iterYouTubeTests {
		t.Run("Iter-"+test.Name, func(t *testing.T) {
			RunIterTest(t, tokenizer.Iter(test.Message), test)
		})
	}
	for _, test := range iterCommandTests {
		t.Run("Iter-"+test.Name, func(t *testing.T) {
			RunIterTest(t, tokenizer.Iter(test.Message), test)
		})
	}
}

func TestScanColon(t *testing.T) {
	type Test struct {
		Name     string
		Message  string
		Expected []string
	}
	tests := []Test{
		{
			Name:     "interiorColons",
			Message:  "abc:d::e:fg:hij",
			Expected: []string{"abc", ":d:", ":e:", "fg", ":hij"},
		},
		{
			Name:     "exteriorColons",
			Message:  ":abc:d::e:fg:hij:",
			Expected: []string{":abc:", "d", ":", ":e:", "fg", ":hij:"},
		},
		{
			Name:     "onlyColonsEven",
			Message:  "::::::",
			Expected: []string{":", ":", ":", ":", ":", ":"},
		},
		{
			Name:     "onlyColonsOdd",
			Message:  ":::::::",
			Expected: []string{":", ":", ":", ":", ":", ":", ":"},
		},
		{
			Name:     "ytEmotes",
			Message:  "lol :_DayoHog::_DayoHog:",
			Expected: []string{"lol ", ":_DayoHog:", ":_DayoHog:"},
		},
		{
			Name:     "ytEmotes2",
			Message:  "yoooo:_DayoHog::goat-turqouise-white-horns::_DayoHog::goat-turqouise-white-horns:haha",
			Expected: []string{"yoooo", ":_DayoHog:", ":goat-turqouise-white-horns:", ":_DayoHog:", ":goat-turqouise-white-horns:", "haha"},
		},
		{
			Name:     "ytEmoteLeadingColon",
			Message:  "::_DayoHog::_DayoHog:",
			Expected: []string{":", ":_DayoHog:", ":_DayoHog:"},
		},
	}

	for _, test := range tests {
		t.Run("Scan-"+test.Name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(test.Message))
			scanner.Split(ScanSeparator(':'))

			i := 0
			for scanner.Scan() {
				if i >= len(test.Expected) {
					t.Fatalf("Overscan: [ %s ]", scanner.Text())
				}
				if scanner.Text() != test.Expected[i] {
					t.Errorf("\nScanned:  [ %s ]\nExpected: [ %s ]\n", scanner.Text(), test.Expected[i])
				}
				i++
			}
		})
	}
}
