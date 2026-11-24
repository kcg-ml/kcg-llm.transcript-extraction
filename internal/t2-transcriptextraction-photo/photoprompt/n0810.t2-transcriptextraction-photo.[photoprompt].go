package photoprompt
// PhotoPromptTemplate is the instruction block of one photo prompt. The
// language line matters: a prompt that does not name the answer language gets
// Chinese or Russian field labels, and the photo index then parses no field.
const PhotoPromptTemplate = `You read one photo that a speaker sent in a Telegram chat about LLM training,
sparsity, pruning, and model architecture. The 2 speakers are ` + "`Synth`" + ` and
` + "`Vlad [russia][ml]`" + `.

The message that carried the photo:
  id: %s
  date: %s
  from: %s
  caption: %s
  reply to: %s

Answer in exactly these 6 lines, one value per line, no other text. Write
every value in English, even when the photo holds Russian or Chinese text:

SHOWS: what the photo shows, in 1 sentence (a chart, a slide, a screenshot of code, a table, a photo of a room)
TEXT: every readable string in the photo that matters: the title, the axis labels, the legend, the code line, the table header. Separate them with ` + "` | `" + `. Write ` + "`none`" + ` if the photo holds no readable text.
TOPIC: the LLM or machine-learning topic the photo belongs to, in 3 to 8 words. Write ` + "`not ml`" + ` if the photo is not about machine learning.
RELEVANCE: one of ` + "`high`" + `, ` + "`low`" + `, ` + "`none`" + `, then ` + "` - `" + ` and the reason
NUMBERS: every number the photo states, as ` + "`<what it measures> = <value>`" + `, separated by ` + "` ;; `" + `. Write ` + "`none`" + ` if the photo states no number.
TERMS: the method, model, or paper names the photo shows, comma separated. Write ` + "`none`" + ` if it names none.

Do not guess a number or a name you cannot read. If the photo is unreadable,
write ` + "`unreadable`" + ` on the SHOWS line and ` + "`none`" + ` on the other lines.
`
