# pi.261122.llm-idea-extraction

Turn the Telegram chat export between `Synth` and `Vlad [russia][ml]` into a
searchable index of every paper, talk, repository, dataset, method, model,
experiment, result, reason, causal explanation, claim, and suggestion the chat
carries.

The goal is extraction, not summary. A summary that drops a paper, a number,
or a talk title is a failed run. Every stage keeps the message id beside the
extracted item, so a reader can check any line against the chat.

## What the index must hold

The 14 fields of one summary, in the order the model answers them:

| field | holds |
| --- | --- |
| `TOPICS` | the subject of the window |
| `PAPERS` | every paper: arXiv id, DOI, title, and what the chat says about it |
| `TALKS` | every talk, conference, meetup, and video |
| `CODE` | every repository, checkpoint, and dataset |
| `METHODS` | every method and technique, named as the speaker names it |
| `MODELS` | every model and checkpoint name |
| `EXPERIMENTS` | what was measured, on what, and the result |
| `RESULTS` | every number, size, and speedup, with what it measures |
| `REASONS` | every cause or justification a speaker gives |
| `CAUSAL` | every cause-and-effect pair, as `<cause> -> <effect>` |
| `CLAIMS` | every belief, hypothesis, and thesis |
| `OPEN` | every question the speakers leave open |
| `SUGGESTIONS` | every piece of advice: `you should do X`, or `if you do X, then Y` |
| `QUOTES` | every sentence worth quoting, with its message id |

A photo adds 6 more fields: `SHOWS`, `TEXT`, `TOPIC`, `RELEVANCE`, `NUMBERS`,
and `TERMS`.

## The 2 exports

| folder | holds | messages |
| --- | --- | --- |
| `extract.telegram-log.vlad/` | text only, `photo` is a placeholder | 10411 |
| `extract.telegram-log.vlad-images/` | text plus `photos/*.jpg` | 2199 |

The image export is a partial export of the same chat: it starts at the same
message and ends earlier. Read the text from the text export and the photos
from the image export, and join the 2 on the message id.

## The chain

Every stage is a Go package. A package sits in a project folder named
`<tier>-transcriptextraction-<project>`, and a group of declarations inside
that project is a sub-folder. There is no Python in this repository.

| group | reads | writes |
| --- | --- | --- |
| `t1-...-export/read` | `extract.telegram-log.vlad/result.json` | `data/messages.csv` and 6 side tables |
| `t1-...-split/*` | `data/messages.csv` | `data/parts_unbroken.csv`, `parts_sentence.csv`, `parts_paragraph.csv`, `parts_manifest.csv` |
| `t1-...-reference/collect` | `data/messages.csv` | `data/references.csv` |
| `t1-...-translate/table` | `data/messages.csv` | `data/translations.csv`, `prompts/message-translation-*.txt` |
| `t2-...-summary/window` | `data/messages.csv`, `data/references.csv`, `data/translations.csv` | `data/windows.csv`, `prompts/` |
| `t2-...-relevance/window` | `data/messages.csv`, `data/translations.csv` | `data/relevance_windows.csv`, `prompts/relevance-window-vote-*.txt` |
| `t2-...-relevance/solo` | `data/messages.csv` | `model-answer/job/relevance-window-vote/done/relevance-window-vote-of-one-long-message-*.txt` |
| `t2-...-relevance/consensus` | `model-answer/job/relevance-window-vote/done/` | `data/relevance_votes.csv`, `data/relevance.csv` |
| `t2-...-photo/table` | `extract.telegram-log.vlad-images/result.json` | `data/photos.csv`, `prompts/photo-content-*.txt` |
| `t2-...-photo/index` | `model-answer/job/photo-content/done/` | `data/photo_index.csv`, the photo rows of `data/index.csv` |
| `t2-...-idea/ideaprompt` | `data/messages.csv`, `data/summaries.csv` | `prompts/idea-extraction-*.txt` |
| `t2-...-idea/ideaparse` | `model-answer/job/idea-extraction/done/` | `data/idea_windows.csv`, `data/idea_items.csv` |
| `t2-...-idea/ideapaper` | `data/idea_items.csv` | `data/papers.csv` |
| `t2-...-idea/ideaconcept` | `data/idea_items.csv` | `data/concepts.csv` |
| `t2-...-aggregate/index` | `model-answer/` | `data/summaries.csv`, `data/summary_items.csv`, `data/summary_rollup.csv`, `data/index*.csv`, `prompts/summary-window-of-whole-chat-*.txt` |
| `t3-...-job/run` | `prompts/`, `data/photos.csv` | `model-answer/job/<job>/done/<id>.txt`, `logs/<job>.progress.log` |
| `t4-...-chain/chainunit` | `data/` | the unit list of one durable job |
| `t4-...-chain/chainrun` | — | runs the rule stages in order |
| `t0-...-table/table` | — | reads and writes a CSV table |
| `t0-...-table/pattern` | — | the shared text rules: URLs, word counts, rune-safe cuts |

`cmd/command` is the only entry point. Its subcommands are `extract`, `split`,
`references`, `translate`, `windows`, `relevance-windows`, `relevance-solo`,
`relevance-consensus`, `photos`, `photo-index`, `idea-windows`, `idea-index`,
`idea-paper`, `idea-concept`, `aggregate`, `job`, and `pipeline`.
`cmd/csvcheck` checks the column count of every CSV.

Run the rule stages with `./build.sh --rules`, then a model stage with
`transcriptextraction job`.

## The layout

A project folder is named `<tier>-transcriptextraction-<project>`, and the
tier is `t0` to `t4` or `te` for a command. A project imports only a project
of a lower tier, so a stage never imports the chain that calls it.

A group is a sub-folder of its project and is its own Go package. A file is
named `nNNNN.<project>.[<group>].go`, where `nNNNN` is the number of its
block: one project takes 100 numbers, and one group takes 10 inside that.
A file holds one type, and the functions that follow it.

`design.transcriptextraction-lexicographic-decomposition.txt` states the full
project list, the group split, the number blocks, and the import order.
`00-projects-and-groups.txt` is the short form of the same list.

## Build and run

`./build.sh` compiles `cmd/transcriptextraction` and `cmd/csvcheck` into
`.build/`, runs `go vet ./...`, checks that every subcommand answers `--help`
with exit 0, and checks that every CSV in `data/` has the column count its
header states. `./build.sh --clean` removes `data/`, `model-answer/`, `prompts/`, and
`logs/` first. `./build.sh --rules` also runs the rule stages, which make no
model call.

A model stage runs through `transcriptextraction job`, which is durable:

```
.build/transcriptextraction job --job relevance-window-vote --jobs 8 --poll 30
.build/transcriptextraction job --job relevance-window-vote --status
```

The 8 job names, and what one answer of each holds:

| job name | one answer holds |
| --- | --- |
| `message-translation` | the English text of 1 message that holds Cyrillic |
| `summary-window-of-10-messages` | the 14-field summary of 10 consecutive messages |
| `summary-window-of-100-messages` | the 14-field summary of 100 consecutive messages |
| `summary-window-of-one-month` | the 14-field summary of 1 calendar month |
| `summary-window-of-whole-chat` | the 14-field summary of the whole chat, rolled up from the months |
| `relevance-window-vote` | 1 vote on whether a window of messages is relevant to machine learning |
| `photo-content` | what 1 photo shows, its text, topic, relevance, numbers, and terms |
| `idea-extraction` | the phase 1 fields of 1 window: `PAPERS`, `TALKS`, `CODE`, `METHODS`, `MODELS`, each with its message id |

A job name is also the `window_kind` value of `data/windows.csv` and the
prefix of a `window_id`, so the 3 agree by construction. The constants sit in
`internal/t4-...-chain/chainunit/n1099...go` and
`internal/t2-...-summary/window/n0599...go`.

The job writes one answer file per unit under `model-answer/job/<job>/done/`, and a
unit is done when that file exists. A killed run resumes where it stopped. The
progress line prints every `--poll` seconds and goes to
`logs/<job>.progress.log`, so `tail -f logs/relevance-window-vote.progress.log` shows
`done/total`, the rate, the ETA, and the calls in flight.

The job names the provider, not just the model. A bare model name matches 9
authenticated providers, and `pi` then prints `Error: Model ... is ambiguous`
on stdout and still exits 0. The job treats that line as a failure, so an
error can never be saved as an answer.

## The message id

`message_key` is `<telegram message id:06d>-<yymmdd>`, for example
`760238-260219`. A part of a message adds `-P<index>`, for example
`760238-260219-P03`. A photo row uses the same key as its message.

## The 3 split modes

`unbroken` keeps one part per message and splits only a message longer than
100 words. `sentence` gives one part per sentence. `paragraph` gives one part
per paragraph, and a code fence stays whole. Every mode covers the same
224468 words, so a reader can pick the unit the index should point at.

## The 4 summary levels

10-message windows roll into 100-message windows, those roll into months, and
the months roll into one summary of the chat. A rollup is a model answer, not
a string join: `internal/t2-transcriptextraction-aggregate/rollup` writes `prompts/summary-window-of-whole-chat-*.txt` and
`transcriptextraction job --job summary-window-of-whole-chat` answers it.

## The photo stage

A photo message carries no text, so the photo is the only content. The model
answer names what the photo shows, the text inside it, its topic, its
relevance to machine learning, the numbers it states, and the terms it shows.

The photo stage needs a model that accepts image input. The default model
`deepseek-v4.1-flash-1017` on `local-dgx1` reads text only, so pass
`--model` and `--provider` naming a vision model. The provider is the full
`provider.model` string, for example
`--model glm-5.3-flash --provider zai.glm-5.3-flash-128k`. A bare
`--provider zai` answers `No API key found for zai`.

## The translation stage

254 of the 10411 messages hold Cyrillic. A prompt built from that text asks
the model a question in Russian, so the stage translates those messages first
and every later prompt reads the English.

`t1-...-translate/table` writes `data/translations.csv` with the columns
`message_key`, `message_id`, `date`, `from`, `text_en`, `source_char_len`, and
`english_char_len`. A message with no Cyrillic keeps its own text, so the
table holds every message a prompt needs.

`t3-...-job/answer` rejects an answer that repeats the source text or keeps
most of its Cyrillic letters, so an untranslated answer is retried and never
saved. A fragment, a URL, a name, or an English sentence that quotes a Russian
example needs no translation, and the check accepts it.

## Folders

- `data/` holds the CSV tables, and it is the source of truth for every later stage.
- `prompts/` holds one prompt file per window and per photo.
- `model-answer/` holds one answer file per window and per photo, plus
  `model-answer/job/<job>/` for the durable runner, which keeps its state in
  `model-answer/job/<job>/state.jsonl`.
- `logs/` holds `<job>.progress.log` for each durable job.
- `internal/` holds the tier projects and their groups, and `cmd/` holds the
  2 commands.
- `build.sh` at the repository root builds and checks the pipeline.

## Go only

This repository holds Go and no Python. A stage is a group under
`internal/<tier>-transcriptextraction-<project>/`, and `cmd/` holds the
commands that call it. A new stage adds a group, a subcommand, and a row in
the chain table above. Do not add a script in another language, and do not
call one from `build.sh`.

## Naming

Every identifier names the thing it holds and the place it sits, so a reader
knows what it is without reading its body. The name is a path from the
general to the specific, in Java style: `UpperCamelCase` for an exported
name, `lowerCamelCase` for an unexported one.

The prefix rule: a name starts with the noun of its own kind, then narrows.
The column names of the window table are `WindowTableColumnNames`, the rows of
the summary table are `SummaryTableColumnNames`, and the function that builds
the index table is `buildIndexTableList`. A reader who sees
`WindowTableColumnNames` knows the kind (column names), the subject (window),
and the table it belongs to (the window table) without opening the file.

3 worked examples from this repository:

| name | reads as |
| --- | --- |
| `refpattern.TermPatternOf` | a function in the reference-pattern group that returns the pattern of one term |
| `prompt.PromptCharBudget` | a constant in the summary-prompt group: the character budget of one prompt |
| `answer.AnswerWrongnessOf` | a function in the job-answer group that returns why one model answer is wrong |

A name that needs a comment to explain it is wrong, so rename it. Never write
`var`, `tmp`, `buf`, `res`, `val`, `data`, `info`, `obj`, or `item` as a name:
each one names no object of this domain. Write `messageRow`, `promptText`,
`stdoutBuffer`, `windowList`, `referenceTable`, or `itemText` instead.
