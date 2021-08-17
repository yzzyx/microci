package ansi

import (
	"fmt"
	"strconv"
	"strings"
)

type ansiSettings struct {
	Color      int
	Background int
	Bold       bool
	Light      bool
	Italic     bool
	Underline  bool
	Blink      bool
}

func newAnsiSettings() ansiSettings {
	return ansiSettings{
		Color:      -1,
		Background: -1,
	}
}

func diffBool(out *strings.Builder, a, b bool, closeTag string, startTag string) {
	if a != b {
		if b != false {
			out.WriteString(closeTag)
		} else {
			out.WriteString(startTag)
		}
	}

}

func diffInt(out *strings.Builder, a, b int, closeTag string, startTag string, args ...interface{}) {
	if a != b {
		if b > -1 {
			out.WriteString(closeTag)
		}
		if a > -1 {
			fmt.Fprintf(out, startTag, args...)
		}
	}
}

// ToString compares current and new settings and produces HTML-tags based on them
func (s ansiSettings) ToString(current ansiSettings) string {
	out := &strings.Builder{}
	diffInt(out, s.Color, current.Color, `</span>`, `<span class="c%d">`, s.Color)
	diffInt(out, s.Background, current.Background, `</span>`, `<span class="b%d">`, s.Background)

	diffBool(out, s.Bold, current.Bold, "</b>", "<b>")
	diffBool(out, s.Italic, current.Italic, "</i>", "<i>")
	diffBool(out, s.Underline, current.Underline, "</u>", "<u>")
	diffBool(out, s.Blink, current.Blink, "</span>", `<span class="blink">`)
	return out.String()
}

// ToHTML converts ANSI escape codes to html code and returns the result
func ToHTML(s string) string {
	out := strings.Builder{}

	s = strings.ReplaceAll(s, "\n", "<br>")
	settings := newAnsiSettings()
	for {
		idx := strings.IndexRune(s, '\033')
		if idx == -1 || len(s) == idx {
			break
		}

		if s[idx+1] != '[' {
			continue
		}

		out.WriteString(s[:idx])
		s = s[idx+2:]

		var separator byte
		nextVal := func() int {
			nextSep := strings.IndexAny(s, ";m")
			code, err := strconv.Atoi(s[:nextSep])
			if err != nil {
				// if it's not a number, we'll ignore it
				return -1
			}
			separator = s[nextSep]
			s = s[nextSep+1:]
			return code
		}

		currentSettings := settings
		for len(s) > 0 && separator != 'm' {
			code := nextVal()
			if code == -1 {
				break
			}
			switch code {
			case 0: // Reset
				settings = newAnsiSettings()
			case 1:
				settings.Bold = true
			case 2:
				settings.Light = true
			case 3:
				settings.Italic = true
			case 4:
				settings.Underline = true
			case 5:
				settings.Blink = true
			case 22: // Normal intensity
				settings.Light = false
				settings.Bold = false
			case 30, 31, 32, 33, 34, 35, 36, 37: // Standard FG
				settings.Color = code - 30
			case 90, 91, 92, 93, 94, 95, 96, 97: // Bright FG
				settings.Color = code - 90 + 8
			case 40, 41, 42, 43, 44, 45, 46, 47: // Standard FG
				settings.Background = code - 40
			case 100, 101, 102, 103, 104, 105, 106, 107: // Bright FG
				settings.Background = code - 100 + 8
			case 38, 48: // 256-color FG,BG
				if nextVal() != 5 {
					break
				}
				if code == 38 {
					settings.Color = nextVal()
				} else {
					settings.Background = nextVal()
				}
			}
		}
		out.WriteString(settings.ToString(currentSettings))
	}
	out.WriteString(s)
	out.WriteString(newAnsiSettings().ToString(settings))
	return out.String()
}
