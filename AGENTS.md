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

Every stage is a Go package under `internal/`, and one command runs them all.
There is no Python in this repository.

| package | reads | writes |
| --- | --- | --- |
| `internal/telegramexport` | `extract.telegram-log.vlad/result.json` | `data/messages.csv` and 6 side tables |
| `internal/messagesplit` | `data/messages.csv` | `data/parts_unbroken.csv`, `parts_sentence.csv`, `parts_paragraph.csv`, `parts_manifest.csv` |
| `internal/referenceextraction` | `data/messages.csv` | `data/references.csv` |
| `internal/summarywindow` | `data/messages.csv`, `data/references.csv` | `data/windows.csv`, `prompts/` |
| `internal/relevancewindow` | `data/messages.csv` | `data/relevance_windows.csv`, `prompts/rel-*.txt` |
| `internal/relevancesolo` | `data/messages.csv` | `out/jobs/rel/done/rel-solo-*.txt` |
| `internal/relevanceconsensus` | `out/jobs/rel/done/` | `data/relevance_votes.csv`, `data/relevance.csv` |
| `internal/photoextraction` | `extract.telegram-log.vlad-images/result.json` | `data/photos.csv`, `prompts/photo-*.txt` |
| `internal/phototable` | `out/jobs/photo/done/` | `data/photo_index.csv`, the photo rows of `data/index.csv` |
| `internal/summaryaggregation` | `out/` | `data/summaries.csv`, `data/summary_items.csv`, `data/summary_rollup.csv`, `data/index*.csv`, `prompts/meta-*.txt` |
| `internal/durablejob` | `prompts/`, `data/photos.csv` | `out/jobs/<job>/done/<id>.txt`, `logs/<job>.progress.log` |
| `internal/jobunit` | `data/` | the unit list of one durable job |
| `internal/pipelinechain` | — | runs the rule stages in order |
| `internal/csvtable` | — | reads and writes a CSV table |
| `internal/textrule` | — | the shared text rules: URLs, word counts, rune-safe cuts |

`cmd/transcriptextraction` is the only entry point. Its subcommands are
`extract`, `split`, `references`, `windows`, `relevance-windows`,
`relevance-solo`, `relevance-consensus`, `photos`, `photo-index`, `aggregate`,
`job`, and `pipeline`. `cmd/csvcheck` checks the column count of every CSV.

Run the rule stages with `./build.sh --rules`, then a model stage with
`transcriptextraction job`.

## Build and run

`./build.sh` compiles `cmd/transcriptextraction` and `cmd/csvcheck` into
`.build/`, runs `go vet ./...`, checks that every subcommand answers `--help`
with exit 0, and checks that every CSV in `data/` has the column count its
header states. `./build.sh --clean` removes `data/`, `out/`, `prompts/`, and
`logs/` first. `./build.sh --rules` also runs the rule stages, which make no
model call.

A model stage runs through `transcriptextraction job`, which is durable:

```
.build/transcriptextraction job --job rel --jobs 8 --poll 30
.build/transcriptextraction job --job rel --status
```

The job writes one answer file per unit under `out/jobs/<job>/done/`, and a
unit is done when that file exists. A killed run resumes where it stopped. The
progress line prints every `--poll` seconds and goes to
`logs/<job>.progress.log`, so `tail -f logs/rel.progress.log` shows
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
a string join: `internal/summaryaggregation` writes `prompts/meta-*.txt` and
`transcriptextraction job --job meta` answers it.

## The photo stage

A photo message carries no text, so the photo is the only content. The model
answer names what the photo shows, the text inside it, its topic, its
relevance to machine learning, the numbers it states, and the terms it shows.

The photo stage needs a model that accepts image input. The default model
`deepseek-v4.1-flash-1017` on `local-dgx1` reads text only, so pass
`--model` and `--provider` naming a vision model.

## Folders

- `data/` holds the CSV tables, and it is the source of truth for every later stage.
- `prompts/` holds one prompt file per window and per photo.
- `out/` holds one answer file per window and per photo, plus
  `out/jobs/<job>/` for the durable runner, which keeps its state in
  `out/jobs/<job>/state.jsonl`.
- `logs/` holds `<job>.progress.log` for each durable job.
- `internal/` holds one package per stage, and `cmd/` holds the 2 commands.
- `build.sh` at the repository root builds and checks the pipeline.

## Go only

This repository holds Go and no Python. A stage is a package under
`internal/`, and `cmd/` holds the commands that call it. A new stage adds a
package, a subcommand, and a row in the chain table above. Do not add a
script in another language, and do not call one from `build.sh`.

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
| `referenceextraction.TermPatternOf` | a function in the reference-extraction package that returns the pattern of one term |
| `summarywindow.PromptCharBudget` | a constant in the summary-window package: the character budget of one prompt |
| `durablejob.AnswerWrongnessOf` | a function in the durable-job package that returns why one model answer is wrong |

A name that needs a comment to explain it is wrong, so rename it. Never write
`var`, `tmp`, `buf`, `res`, `val`, `data`, `info`, `obj`, or `item` as a name:
each one names no object of this domain. Write `messageRow`, `promptText`,
`stdoutBuffer`, `windowList`, `referenceTable`, or `itemText` instead.
