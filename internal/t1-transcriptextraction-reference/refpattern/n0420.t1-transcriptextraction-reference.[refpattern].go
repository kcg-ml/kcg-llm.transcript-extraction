package refpattern

import (
	"regexp"
)
// WordBoundaryLeft and WordBoundaryRight mark the 2 ends of a whole word.
//
// A pattern written as `\b(потому|поэтому|значит)\b` matches inside another
// word, because a Cyrillic letter is not a word character to a regular
// expression written without Unicode classes: `значит` then matches inside
// `значительно`. These 2 fragments name the character classes by hand, so the
// match is a whole word in every alphabet.
const (
	WordBoundaryLeft  = `(?:^|[^\p{L}\p{N}_])`
	WordBoundaryRight = `(?:[^\p{L}\p{N}_]|$)`
)

// TalkPattern matches a talk, a conference, a meetup, or a slide deck.
var TalkPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(data ?fest(?:\s+\p{L}+)?(?:\s+\d+)?|conference|meetup|workshop|конференц\p{L}*|митап|воркшоп|доклад|выступлени\p{L}*|слайды)` + WordBoundaryRight)

// ResultPattern matches a sentence that states a number with a unit or a
// comparison.
var ResultPattern = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?\s*(?:%|x|×|times|billion|million|b\b|m\b|gb|mb|tb|tokens?|params?|parameters?|layers?|heads?|dim|dimensions?|seconds?|minutes?|hours?|days?|t/s|tokens/s|fps|ms)\b|\b\d+x\b|\bspeedup\b|\b\d+\s*(?:to|-)\s*\d+x\b)`)

// ExperimentPattern matches a sentence that reports a measurement or a run.
var ExperimentPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(experiment|benchmark|ablation|measured|we ran|i ran|i tested|tested|evaluated|evaluation|results?|speedup|accuracy|perplexity|loss|throughput|tokens per second|t/s|baseline|compare|comparison|эксперимент|бенчмарк|замер|измерил|проверил|результат|точность|лосс|ускорение|сравнени)` + WordBoundaryRight)

// ReasonPattern matches a sentence that gives a cause or a justification.
var ReasonPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(because|since|therefore|thus|hence|so that|the reason|which means|that is why|потому|поэтому|значит|причина|следовательно)` + WordBoundaryRight)

// CausalPattern matches a sentence that names a cause and its effect.
var CausalPattern = regexp.MustCompile(`(?i)(` + WordBoundaryLeft + `(?:causes?|caused by|leads? to|led to|results? in|resulted in|gives? rise to|due to|owing to|thanks to|driven by|because of|as a result|consequently|therefore|thus|hence|so that|which means|that is why|this is why|the reason is)` + WordBoundaryRight + `|` + WordBoundaryLeft + `because` + WordBoundaryRight + `|` + WordBoundaryLeft + `if` + WordBoundaryRight + `[^.]{3,80}` + WordBoundaryLeft + `then` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:приводит|привело|вызывает|из-за|вследствие|поэтому|потому что|следовательно|значит)` + WordBoundaryRight + `)`)

// SuggestionPattern matches a sentence that gives advice.
var SuggestionPattern = regexp.MustCompile(`(?i)(` + WordBoundaryLeft + `you (?:should|must|need to|have to|can|could|might want to|ought to)` + WordBoundaryRight + `|` + WordBoundaryLeft + `if you (?:do|use|try|take|want|go|set|run|add|remove|switch)` + WordBoundaryRight + `|` + WordBoundaryLeft + `i (?:would|'d) (?:suggest|recommend|try|do)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:try|consider|make sure|be sure to|worth trying|better to)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:advise|recommend|suggest(?:ion)?)` + WordBoundaryRight + `|` + WordBoundaryLeft + `(?:советую|попробуй|стоит|лучше|надо|нужно|если (?:ты|вы))` + WordBoundaryRight + `)`)

// ClaimPattern matches a sentence that states a belief or a hypothesis.
var ClaimPattern = regexp.MustCompile(`(?i)` + WordBoundaryLeft + `(i think|i believe|i suspect|in my view|my hypothesis|hypothesis|i claim|it seems|it appears|probably|likely|i'd say|думаю|считаю|гипотеза|идея|утвержда|кажется|вероятно)` + WordBoundaryRight)

// TermPatternOf returns a pattern that matches a term as a whole word.
//
// The 2 ends are named by character class, not by `\b`, so the match is a whole
// word in Cyrillic and Chinese text too. A hyphen belongs to the word, so
// `nanogpt` does not match inside `modded-nanogpt`.
func TermPatternOf(term string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_-])` + regexp.QuoteMeta(term) +
		`(?:[^\p{L}\p{N}_-]|$)`)
}

// TalkSpanListOf returns the span of every talk name in a lower-case text.
//
// The right boundary of TalkPattern is a character that belongs to the next
// word, so a plain FindAll drops the talk that follows another talk in the
// same sentence: `Data Fest 11 conference` yields `data fest 11` and loses
// `conference`. This function scans on from the end of the matched name, so
// the next scan starts at the character the boundary consumed.
func TalkSpanListOf(lowerText string) [][2]int {
	talkSpanList := make([][2]int, 0, 4)
	scanStart := 0
	for scanStart < len(lowerText) {
		match := TalkPattern.FindStringSubmatchIndex(lowerText[scanStart:])
		if match == nil {
			break
		}
		talkSpanList = append(talkSpanList, [2]int{
			scanStart + match[2], scanStart + match[3]})
		scanStart += match[3]
	}
	return talkSpanList
}
