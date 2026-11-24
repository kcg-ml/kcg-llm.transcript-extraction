package label

import (
	"regexp"
	"strings"
)
// LabelBefore returns the reference label that sits just before a position,
// for example `paper:` or `код:`.
//
// The label must end the 40-character window, and a word character must not
// sit in front of it: `for papers` yields `papers`, while `OpenReview:` yields
// nothing because `review` starts inside the word `OpenReview`.
func LabelBefore(text string, position int) string {
	windowStart := position - 40
	if windowStart < 0 {
		windowStart = 0
	}
	window := strings.ToLower(text[windowStart:position])
	bestLabel := ""
	bestIndex := -1
	for _, labelMatch := range ReferenceLabelPattern.FindAllStringSubmatchIndex(window, -1) {
		if labelMatch[0] <= bestIndex {
			continue
		}
		bestLabel = window[labelMatch[2]:labelMatch[3]]
		bestIndex = labelMatch[0]
	}
	return bestLabel
}

// referenceLabelList names the labels the 2 speakers write in front of a
// reference.
var referenceLabelList = []string{
	"статья", "статьи", "paper", "papers", "препринт", "preprint", "pdf",
	"arxiv", "doi", "код", "code", "repo", "repository", "github", "hf",
	"huggingface", "blog", "пост", "post", "ревью", "review", "видео", "video",
	"слайды", "slides", "тред", "thread", "твит", "tweet",
}

// ReferenceLabelPattern matches a reference label that ends the text it reads,
// with an optional colon after it. The left boundary is a Unicode word
// boundary, so a label that starts inside a longer word does not match.
//
// The right boundary is `\s*[:：]?\s*$` and nothing else. Go's `\b` is
// ASCII-only, so it does not end a Cyrillic label: `статья:` would never match.
var ReferenceLabelPattern = regexp.MustCompile(
	`(?:^|[^\p{L}\p{N}_])(` + strings.Join(referenceLabelList, "|") + `)` +
		`\s*[:：]?\s*$`)
