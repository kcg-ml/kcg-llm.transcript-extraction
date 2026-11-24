package read
// ExportFile is the part of result.json this stage reads.
type ExportFile struct {
	Name     string          `json:"name"`
	Messages []ExportMessage `json:"messages"`
}
