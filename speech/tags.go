// Package speech holds rules about narration text that don't depend on which
// voice speaks it.
package speech

import (
	"regexp"
	"unicode/utf8"
)

// tagCandidate finds bracketed text that may be an audio tag. It allows no "["
// or "]" inside, so candidates never overlap or nest.
var tagCandidate = regexp.MustCompile(`\[[A-Za-z][A-Za-z ,'’\-]*\]`)

// StripTags removes audio tags from narration text. Audio tags are stage
// directions for the voice, such as "[whispers]" or "[exaggerated clueless
// voice]": ElevenLabs v3 and later act them out, while any other voice would
// read them aloud.
//
// A tag is "[", a letter, then letters, spaces, commas, apostrophes or
// hyphens, then "]". It has to stand on its own: at the start of the text or
// after whitespace, and followed by the end of the text, whitespace or one of
// , . ! ? ; : …  So code in narration, such as arr[0], [1, 2, 3] or
// list[index], is left alone.
//
// Removing a tag also removes the one whitespace character after it, or the
// one before it when punctuation or the end of the text follows. Text without
// tags is returned unchanged.
//
// codevideo-types' stripSpeechTags applies the same rule; both run
// testdata/speech-tag-cases.json.
func StripTags(text string) string {
	var tags [][2]int
	for _, loc := range tagCandidate.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && !isSpace(text[start-1]) {
			continue
		}
		if end < len(text) && !isSpace(text[end]) && !startsWithPunctuation(text[end:]) {
			continue
		}
		tags = append(tags, [2]int{start, end})
	}
	if len(tags) == 0 {
		return text
	}

	out := text[:tags[0][0]]
	for i, tag := range tags {
		stop := len(text)
		if i+1 < len(tags) {
			stop = tags[i+1][0]
		}
		next := text[tag[1]:stop]
		if next != "" && isSpace(next[0]) {
			next = next[1:]
		} else if (next == "" || startsWithPunctuation(next)) && out != "" && isSpace(out[len(out)-1]) {
			out = out[:len(out)-1]
		}
		out += next
	}
	return out
}

// HasTags reports whether text has an audio tag that StripTags would remove.
func HasTags(text string) bool {
	return StripTags(text) != text
}

// isSpace is ASCII whitespace only, so this agrees exactly with the
// TypeScript twin, which works on UTF-16 units.
func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func startsWithPunctuation(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	switch r {
	case ',', '.', '!', '?', ';', ':', '…':
		return true
	}
	return false
}
