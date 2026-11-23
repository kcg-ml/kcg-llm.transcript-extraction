// Command transcriptextraction runs the stages of the transcript-extraction
// pipeline.
//
// Every stage is one subcommand, so a reader runs one binary and reads one
// help text:
//
//	transcriptextraction extract              result.json -> data/messages.csv
//	transcriptextraction split                messages    -> data/parts_<mode>.csv
//	transcriptextraction references           messages    -> data/references.csv
//	transcriptextraction windows              messages    -> data/windows.csv, prompts/
//	transcriptextraction relevance-windows    messages    -> data/relevance_windows.csv
//	transcriptextraction relevance-solo       long messages decided by rules
//	transcriptextraction relevance-consensus  votes       -> data/relevance.csv
//	transcriptextraction photos               image export -> data/photos.csv
//	transcriptextraction photo-index          answers     -> data/photo_index.csv
//	transcriptextraction aggregate            answers     -> data/summaries.csv, data/index*.csv
//	transcriptextraction job                  one durable model job
//	transcriptextraction pipeline             the whole chain in order
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/durablejob"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/jobunit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/messagesplit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/photoextraction"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/phototable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/pipelinechain"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/referenceextraction"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevanceconsensus"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevancesolo"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevancewindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summaryaggregation"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summarywindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/telegramexport"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/textrule"
)

// DefaultModelName is the model a job calls when the operator names none.
const DefaultModelName = "deepseek-v4.1-flash-1017"

// DefaultProviderName names the provider of DefaultModelName.
//
// A bare model name matches 9 authenticated providers, and pi then exits 0 with
// an `Error: Model ... is ambiguous` line on stdout. The provider is always
// named, so a call cannot fail that way.
const DefaultProviderName = "local-dgx1.deepseek-v4.1-flash-1017-128k"

// subcommandHelpText is the help text of the whole command.
const subcommandHelpText = `transcriptextraction runs the stages of the transcript-extraction pipeline.

Usage:
  transcriptextraction <subcommand> [flags]

Subcommands:
  extract              read extract.telegram-log.vlad/result.json and write
                       data/messages.csv and its 6 side tables
  split                split every message into parts in 3 modes
  references           pull out every reference of every message
  windows              build the summary windows and their prompts
  relevance-windows    build the 3 overlapping relevance windows per message
  relevance-solo       decide the relevance of a long message with rules
  relevance-consensus  turn the 3 votes per message into 1 verdict
  photos               read the image export and write one prompt per photo
  photo-index          join the photo answers into data/index.csv
  aggregate            turn the model answers into the summary and index tables
  job                  run one durable model job, with a progress line
  pipeline             run the whole chain in order

Run "transcriptextraction <subcommand> --help" for the flags of one subcommand.
`

// runExtract runs the extract stage.
func runExtract(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("extract", flag.ExitOnError)
	exportFolderName := flagSet.String("export", pipelinechain.TextExportFolderName,
		"folder of the text export, holding result.json")
	flagSet.Parse(argumentList)
	return telegramexport.Run(projectFolderPath, *exportFolderName)
}

// runSplit runs the split stage.
func runSplit(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("split", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return messagesplit.Run(projectFolderPath)
}

// runReferences runs the reference extraction stage.
func runReferences(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("references", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return referenceextraction.Run(projectFolderPath)
}

// runWindows runs the summary window stage.
func runWindows(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("windows", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return summarywindow.Run(projectFolderPath)
}

// runRelevanceWindows runs the relevance window stage.
func runRelevanceWindows(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-windows", flag.ExitOnError)
	forceRewrite := flagSet.Bool("force", false,
		"rewrite a prompt file that already exists")
	flagSet.Parse(argumentList)
	return relevancewindow.Run(projectFolderPath, *forceRewrite)
}

// runRelevanceSolo runs the rule pass over the long messages.
func runRelevanceSolo(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-solo", flag.ExitOnError)
	forceRewrite := flagSet.Bool("force", false,
		"overwrite an answer that already exists")
	flagSet.Parse(argumentList)
	return relevancesolo.Run(projectFolderPath, *forceRewrite)
}

// runRelevanceConsensus runs the consensus stage.
func runRelevanceConsensus(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-consensus", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return relevanceconsensus.Run(projectFolderPath)
}

// runPhotos runs the photo extraction stage.
func runPhotos(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("photos", flag.ExitOnError)
	imageExportFolderName := flagSet.String("export", pipelinechain.ImageExportFolderName,
		"folder of the image export, holding result.json and photos/")
	forceRewrite := flagSet.Bool("force", false,
		"rewrite a prompt file that already exists")
	strictMode := flagSet.Bool("strict", false,
		"stop when the export does not parse, instead of keeping the complete messages")
	flagSet.Parse(argumentList)
	return photoextraction.Run(projectFolderPath, *imageExportFolderName, *forceRewrite, *strictMode)
}

// runPhotoIndex runs the photo index stage.
func runPhotoIndex(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("photo-index", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return phototable.Run(projectFolderPath)
}

// runAggregate runs the aggregation stage.
func runAggregate(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("aggregate", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return summaryaggregation.Run(projectFolderPath)
}

// addJobFlags adds the flags every job shares to a flag set.
func addJobFlags(flagSet *flag.FlagSet, settings *durablejob.JobSettings) {
	flagSet.IntVar(&settings.WorkerCount, "jobs", 8, "number of calls in flight")
	flagSet.IntVar(&settings.PollSeconds, "poll", 30,
		"seconds between progress lines")
	flagSet.StringVar(&settings.ModelName, "model", DefaultModelName, "model name")
	flagSet.StringVar(&settings.ProviderName, "provider", DefaultProviderName,
		"provider name; a bare model name is ambiguous across providers")
	flagSet.IntVar(&settings.TimeoutSeconds, "timeout", 600,
		"seconds before one call is killed")
	flagSet.IntVar(&settings.RetryCount, "retries", 2, "retries of one unit")
	flagSet.IntVar(&settings.RetrySleepSeconds, "retry-sleep", 5,
		"seconds between retries")
	flagSet.IntVar(&settings.UnitLimit, "limit", 0,
		"stop after this many units; 0 runs every unit")
	flagSet.BoolVar(&settings.PrintStatusOnly, "status", false,
		"print the progress of the job and exit")
	flagSet.BoolVar(&settings.RetryFailedOnly, "retry-failed", false,
		"clear the failed markers so the next run retries them")
}

// runJob runs one durable model job.
func runJob(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("job", flag.ExitOnError)
	jobName := flagSet.String("job", "", "job name: w10, w100, month, meta, rel, photo")
	settings := durablejob.JobSettings{}
	addJobFlags(flagSet, &settings)
	flagSet.Parse(argumentList)
	if *jobName == "" {
		return fmt.Errorf("give --job; the jobs are %v", jobunit.JobNameList)
	}
	if !jobunit.IsKnownJobName(*jobName) {
		return fmt.Errorf("unknown job %q; the jobs are %v", *jobName, jobunit.JobNameList)
	}
	unitList, unitError := jobunit.UnitListOf(projectFolderPath, *jobName)
	if unitError != nil {
		return unitError
	}
	settings.JobName = *jobName
	exitCode := durablejob.Run(projectFolderPath, settings, unitList)
	if exitCode != 0 {
		return fmt.Errorf("job %s exited with code %d", *jobName, exitCode)
	}
	return nil
}

// runPipeline runs the whole chain in order.
func runPipeline(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("pipeline", flag.ExitOnError)
	stageName := flagSet.String("stage", "rules",
		"stage to run: rules, extract, split, references, windows, "+
			"relevance-windows, relevance-solo, relevance-consensus, photos, "+
			"photo-index, aggregate, all")
	runModelStages := flagSet.Bool("model-stages", false,
		"also run the model jobs, which take hours")
	visionModelName := flagSet.String("vision-model", "",
		"a model that accepts image input, for the photo job")
	visionProviderName := flagSet.String("vision-provider", "",
		"provider of the vision model")
	settings := durablejob.JobSettings{}
	addJobFlags(flagSet, &settings)
	flagSet.Parse(argumentList)

	return pipelinechain.Run(pipelinechain.ChainSettings{
		ProjectFolderPath:  projectFolderPath,
		StageName:          *stageName,
		JobSettings:        settings,
		VisionModelName:    *visionModelName,
		VisionProviderName: *visionProviderName,
		RunModelStages:     *runModelStages,
	})
}

// main dispatches one subcommand.
func main() {
	projectFolderPath := textrule.ProjectFolderPath()
	if len(os.Args) < 2 {
		fmt.Print(subcommandHelpText)
		os.Exit(1)
	}
	subcommandName := os.Args[1]
	argumentList := os.Args[2:]
	if subcommandName == "--help" || subcommandName == "-h" || subcommandName == "help" {
		fmt.Print(subcommandHelpText)
		return
	}

	subcommandFunctionOfName := map[string]func(string, []string) error{
		"extract":             runExtract,
		"split":               runSplit,
		"references":          runReferences,
		"windows":             runWindows,
		"relevance-windows":   runRelevanceWindows,
		"relevance-solo":      runRelevanceSolo,
		"relevance-consensus": runRelevanceConsensus,
		"photos":              runPhotos,
		"photo-index":         runPhotoIndex,
		"aggregate":           runAggregate,
		"job":                 runJob,
		"pipeline":            runPipeline,
	}
	subcommandFunction, isKnownSubcommand := subcommandFunctionOfName[subcommandName]
	if !isKnownSubcommand {
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", subcommandName, subcommandHelpText)
		os.Exit(1)
	}
	if runError := subcommandFunction(projectFolderPath, argumentList); runError != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", runError)
		os.Exit(1)
	}
}
