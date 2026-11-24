package path

import (
	"os"
	"path/filepath"
)
// ProjectFolderPath returns the folder that holds data/, prompts/,
// model-answer/, and
// logs/. The stages run from the repository root, so the working folder is the
// project folder, unless the operator names another one in the environment
// variable TRANSCRIPTEXTRACTION_PROJECT_FOLDER.
func ProjectFolderPath() string {
	namedFolderPath := os.Getenv(ProjectFolderVariableName)
	if namedFolderPath != "" {
		return namedFolderPath
	}
	workingFolderPath, workingError := os.Getwd()
	if workingError != nil {
		return "."
	}
	return workingFolderPath
}

// ProjectFolderVariableName names the environment variable that overrides the
// project folder, so a parity run can write into a scratch folder.
const ProjectFolderVariableName = "TRANSCRIPTEXTRACTION_PROJECT_FOLDER"

// DataFolderPath returns the folder of the CSV tables.
func DataFolderPath() string {
	return filepath.Join(ProjectFolderPath(), "data")
}

// PromptFolderPath returns the folder of the prompt files.
func PromptFolderPath() string {
	return filepath.Join(ProjectFolderPath(), "prompts")
}

// OutputFolderPath returns the folder of the model answers, one file per
// unit, plus the runner state of each job.
func OutputFolderPath() string {
	return filepath.Join(ProjectFolderPath(), "model-answer")
}

// LogFolderPath returns the folder of the run logs.
func LogFolderPath() string {
	return filepath.Join(ProjectFolderPath(), "logs")
}
