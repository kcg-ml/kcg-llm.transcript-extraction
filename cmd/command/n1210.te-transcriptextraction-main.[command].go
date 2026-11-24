package main

import (
	"fmt"
	"os"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t0-transcriptextraction-table/path"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/cmd/subcommand"
)
// main dispatches one subcommand.
func main() {
	projectFolderPath := path.ProjectFolderPath()
	if len(os.Args) < 2 {
		fmt.Print(subcommand.SubcommandHelpText)
		os.Exit(1)
	}
	subcommandName := os.Args[1]
	argumentList := os.Args[2:]
	if subcommandName == "--help" || subcommandName == "-h" || subcommandName == "help" {
		fmt.Print(subcommand.SubcommandHelpText)
		return
	}

	subcommandFunctionOfName := map[string]func(string, []string) error{
		"extract":             subcommand.RunExtract,
		"split":               subcommand.RunSplit,
		"references":          subcommand.RunReferences,
		"translate":           subcommand.RunTranslate,
		"windows":             subcommand.RunWindows,
		"relevance-windows":   subcommand.RunRelevanceWindows,
		"relevance-solo":      subcommand.RunRelevanceSolo,
		"relevance-consensus": subcommand.RunRelevanceConsensus,
		"photos":              subcommand.RunPhotos,
		"photo-index":         subcommand.RunPhotoIndex,
		"idea-windows":        subcommand.RunIdeaWindows,
		"idea-index":          subcommand.RunIdeaIndex,
		"aggregate":           subcommand.RunAggregate,
		"job":                 subcommand.RunJob,
		"pipeline":            subcommand.RunPipeline,
	}
	subcommandFunction, isKnownSubcommand := subcommandFunctionOfName[subcommandName]
	if !isKnownSubcommand {
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", subcommandName, subcommand.SubcommandHelpText)
		os.Exit(1)
	}
	if runError := subcommandFunction(projectFolderPath, argumentList); runError != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runError)
		// A job ends with a code that says why it stopped, so a script can
		// tell a lock refusal from a failure. A plain error is exit 1.
		os.Exit(subcommand.ExitCodeOf(runError))
	}
}
