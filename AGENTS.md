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

| script | reads | writes |
| --- | --- | --- |
| `bin/tg_extract.py` | `extract.telegram-log.vlad/result.json` | `data/messages.csv` and 6 side tables |
| `bin/tg_split.py` | `data/messages.csv` | `data/parts_unbroken.csv`, `parts_sentence.csv`, `parts_paragraph.csv` |
| `bin/tg_references.py` | `data/messages.csv` | `data/references.csv` |
| `bin/tg_windows.py` | `data/messages.csv`, `data/references.csv` | `data/windows.csv`, `prompts/` |
| `bin/tg_relevance.py` | `data/messages.csv` | `data/relevance_windows.csv`, `prompts/rel-*.txt` |
| `bin/tg_job.py` | `prompts/`, `data/photos.csv` | `out/jobs/<job>/done/<id>.txt`, `logs/<job>.progress.log` |
| `bin/tg_run_pi.py` | `prompts/` | `out/<window_id>.txt`, `logs/` |
| `bin/tg_aggregate.py` | `out/` | `data/summaries.csv`, `data/index*.csv`, `prompts/meta-*.txt` |
| `bin/tg_relevance_consensus.py` | `out/jobs/rel/done/` | `data/relevance_votes.csv`, `data/relevance.csv` |
| `bin/tg_photos.py` | `extract.telegram-log.vlad-images/result.json` | `data/photos.csv`, `prompts/photo-*.txt` |
| `bin/tg_photo_index.py` | `out/jobs/photo/done/` | `data/photo_index.csv`, the photo rows of `data/index.csv` |
| `bin/tg_pipeline.py` | — | runs the rule chain in order |

Run the whole chain with `./build.sh --rules`, then `bin/tg_job.py` for the
model stages.

## Build and run

`./build.sh` compiles every script in `bin/`, checks that each answers
`--help`, and checks that every CSV in `data/` has the column count its header
states. `./build.sh --clean` removes `data/`, `out/`, `prompts/`, and `logs/`
first. `./build.sh --rules` also runs the rule stages, which make no model call.

A model stage runs through `bin/tg_job.py`, which is durable:

```
python3 bin/tg_job.py --job rel --jobs 8 --poll 30
python3 bin/tg_job.py --job rel --status
```

The job writes one answer file per unit under `out/jobs/<job>/done/`, and a
unit is done when that file exists. A killed run resumes where it stopped. The
progress line prints every `--poll` seconds and goes to
`logs/<job>.progress.log`, so `tail -f logs/rel.progress.log` shows
`done/total`, the rate, the ETA, and the calls in flight.

`bin/tg_job.py` names the provider, not just the model. A bare model name
matches 9 authenticated providers, and `pi` then prints
`Error: Model ... is ambiguous` on stdout and still exits 0. The job treats
that line as a failure, so an error can never be saved as an answer.

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
a string join: `bin/tg_aggregate.py` writes `prompts/meta-*.txt` and
`bin/tg_job.py --job meta` answers it.

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
- `out/` holds one answer file per window and per photo, plus `run-state.csv`
  and `out/jobs/<job>/` for the durable runner.
- `logs/` holds one log file per run of `bin/tg_run_pi.py`, and
  `<job>.progress.log` for each durable job.
- `bin/` holds the stage scripts and the durable job runner.
- `build.sh` at the repository root builds and checks the pipeline.