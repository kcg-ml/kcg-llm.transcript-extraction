package store
// JobUnit is one call: its identity, the prompt file it reads, and the image
// it passes to the model, if any.
type JobUnit struct {
	UnitId         string
	PromptFilePath string
	ImageFilePath  string
}
