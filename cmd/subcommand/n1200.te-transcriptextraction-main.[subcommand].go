package subcommand

import (
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/store"
	"flag"
	"fmt"
	"strings"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/chainrun"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/chainunit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-reference/collect"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/consensus"
	aggregateindex "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/index"
	photoindex "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-photo/index"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-idea/ideaparse"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-idea/ideaprompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/part"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/prompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/relprompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/run"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/solo"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/stage"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-photo/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-export/write"
)
// DefaultModelName is the model a job calls when the operator names none.
const DefaultModelName = "deepseek-v4.1-flash-1017"

// DefaultProviderName names the provider of DefaultModelName.
//
// A bare model name matches 9 authenticated providers, and pi then exits 0 with
// an `Error: Model ... is ambiguous` line on stdout. The provider is always
// named, so a call cannot fail that way.
const DefaultProviderName = "local-dgx1.deepseek-v4.1-flash-1017-128k"

// SubcommandHelpText is the help text of the whole command.
const SubcommandHelpText = `transcriptextraction runs the stages of the transcript-extraction pipeline.

Usage:
  transcriptextraction <subcommand> [flags]

Subcommands:
  extract              read extract.telegram-log.vlad/result.json and write
                       data/messages.csv and its 6 side tables
  split                split every message into parts in 3 modes
  references           pull out every reference of every message
  translate            translate every Cyrillic message into English
  windows              build the summary windows and their prompts
  relevance-windows    build the 3 overlapping relevance windows per message
  relevance-solo       decide the relevance of a long message with rules
  relevance-consensus  turn the 3 votes per message into 1 verdict
  photos               read the image export and write one prompt per photo
  photo-index          join the photo answers into data/index.csv
  idea-windows         build the phase 1 idea-extraction windows and prompts
  idea-index           turn the idea answers into data/idea_items.csv
  aggregate            turn the model answers into the summary and index tables
  job                  run one durable model job, with a progress line
  pipeline             run the whole chain in order

Run "transcriptextraction <subcommand> --help" for the flags of one subcommand.
`

// RunExtract runs the extract stage.
func RunExtract(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("extract", flag.ExitOnError)
	exportFolderName := flagSet.String("export", stage.TextExportFolderName,
		"folder of the text export, holding result.json")
	flagSet.Parse(argumentList)
	return write.Run(projectFolderPath, *exportFolderName)
}

// RunSplit runs the split stage.
func RunSplit(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("split", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return part.Run(projectFolderPath)
}

// RunReferences runs the reference extraction stage.
func RunReferences(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("references", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return collect.Run(projectFolderPath)
}

// RunTranslate runs the translation stage: it writes the prompt of every
// Cyrillic message and data/translations.csv from the answers.
func RunTranslate(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("translate", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return translatetable.Run(projectFolderPath)
}

// RunWindows runs the summary window stage.
func RunWindows(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("windows", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return prompt.Run(projectFolderPath)
}

// RunRelevanceWindows runs the relevance window stage.
func RunRelevanceWindows(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-windows", flag.ExitOnError)
	forceRewrite := flagSet.Bool("force", false,
		"rewrite a prompt file that already exists")
	flagSet.Parse(argumentList)
	return relprompt.Run(projectFolderPath, *forceRewrite)
}

// RunRelevanceSolo runs the rule pass over the long messages.
func RunRelevanceSolo(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-solo", flag.ExitOnError)
	forceRewrite := flagSet.Bool("force", false,
		"overwrite an answer that already exists")
	flagSet.Parse(argumentList)
	return solo.Run(projectFolderPath, *forceRewrite)
}

// RunRelevanceConsensus runs the consensus stage.
func RunRelevanceConsensus(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("relevance-consensus", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return consensus.Run(projectFolderPath)
}

// RunPhotos runs the photo extraction stage.
func RunPhotos(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("photos", flag.ExitOnError)
	imageExportFolderName := flagSet.String("export", stage.ImageExportFolderName,
		"folder of the image export, holding result.json and photos/")
	forceRewrite := flagSet.Bool("force", false,
		"rewrite a prompt file that already exists")
	strictMode := flagSet.Bool("strict", false,
		"stop when the export does not parse, instead of keeping the complete messages")
	flagSet.Parse(argumentList)
	return table.Run(projectFolderPath, *imageExportFolderName, *forceRewrite, *strictMode)
}

// RunPhotoIndex runs the photo index stage.
func RunPhotoIndex(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("photo-index", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return photoindex.Run(projectFolderPath)
}

// RunIdeaWindows runs the phase 1 idea-extraction window stage.
func RunIdeaWindows(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("idea-windows", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return ideaprompt.Run(projectFolderPath)
}

// RunIdeaIndex turns the idea answers into data/idea_items.csv.
func RunIdeaIndex(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("idea-index", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return ideaparse.Run(projectFolderPath)
}

// RunAggregate runs the aggregation stage.
func RunAggregate(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("aggregate", flag.ExitOnError)
	flagSet.Parse(argumentList)
	return aggregateindex.Run(projectFolderPath)
}

// AddJobFlags adds the flags every job shares to a flag set.
func AddJobFlags(flagSet *flag.FlagSet, settings *store.JobSettings) {
	flagSet.IntVar(&settings.WorkerCount, "jobs", 8, "number of calls in flight")
	flagSet.IntVar(&settings.PollSeconds, "poll", 30,
		"seconds between progress lines")
	flagSet.StringVar(&settings.ModelName, "model", DefaultModelName, "model name")
	flagSet.StringVar(&settings.ProviderName, "provider", DefaultProviderName,
		"provider name; a bare model name is ambiguous across providers")
	flagSet.IntVar(&settings.TimeoutSeconds, "timeout", 0,
		"seconds before one call is killed; 0 uses the bound measured for the job")
	flagSet.IntVar(&settings.RetryCount, "retries", 2, "retries of one unit")
	flagSet.IntVar(&settings.RetrySleepSeconds, "retry-sleep", 5,
		"seconds between retries")
	flagSet.IntVar(&settings.StallSeconds, "stall", 90,
		"seconds a call may go without growing its output before it is killed; "+
			"0 uses the bound measured for the job")
	flagSet.BoolVar(&settings.ProbeOnly, "probe", false,
		"make one call on the first pending unit and print its rate, saving no answer")
	flagSet.IntVar(&settings.UnitLimit, "limit", 0,
		"stop after this many units; 0 runs every unit")
	flagSet.BoolVar(&settings.PrintStatusOnly, "status", false,
		"print the progress of the job and exit")
	flagSet.BoolVar(&settings.RetryFailedOnly, "retry-failed", false,
		"clear the failed markers so the next run retries them")
}

// RunJob runs one durable model job.
func RunJob(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("job", flag.ExitOnError)
	jobName := flagSet.String("job", "",
		"job name: "+strings.Join(chainunit.JobNameList, ", "))
	settings := store.JobSettings{}
	AddJobFlags(flagSet, &settings)
	flagSet.Parse(argumentList)
	if *jobName == "" {
		return fmt.Errorf("give --job; the jobs are %v", chainunit.JobNameList)
	}
	if !chainunit.IsKnownJobName(*jobName) {
		return fmt.Errorf("unknown job %q; the jobs are %v", *jobName, chainunit.JobNameList)
	}
	unitList, unitError := chainunit.UnitListOf(projectFolderPath, *jobName)
	if unitError != nil {
		return unitError
	}
	settings.JobName = *jobName
	if settings.TimeoutSeconds == 0 {
		settings.TimeoutSeconds = chainunit.TimeoutSecondsOf(*jobName)
	}
	if settings.StallSeconds == 90 {
		// 90 is the default, and it is also the floor of the measured
		// bounds. A job whose measured bound is above 90 takes its own, so
		// a healthy slow call is not killed by the default.
		settings.StallSeconds = chainunit.StallSecondsOf(*jobName)
	}
	if settings.StallSeconds == 0 {
		settings.StallSeconds = chainunit.StallSecondsOf(*jobName)
	}
	exitCode := run.Run(projectFolderPath, settings, unitList)
	if exitCode != 0 {
		return &ExitCodeError{Code: exitCode,
			ReasonText: fmt.Sprintf("job %s exited with code %d", *jobName, exitCode)}
	}
	return nil
}

// RunPipeline runs the whole chain in order.
func RunPipeline(projectFolderPath string, argumentList []string) error {
	flagSet := flag.NewFlagSet("pipeline", flag.ExitOnError)
	stageName := flagSet.String("stage", "rules",
		"stage to run: rules, extract, split, references, translate, windows, "+
			"relevance-windows, relevance-solo, relevance-consensus, photos, "+
			"photo-index, aggregate, all")
	runModelStages := flagSet.Bool("model-stages", false,
		"also run the model jobs, which take hours")
	visionModelName := flagSet.String("vision-model", "",
		"a model that accepts image input, for the photo job")
	visionProviderName := flagSet.String("vision-provider", "",
		"provider of the vision model")
	settings := store.JobSettings{}
	AddJobFlags(flagSet, &settings)
	flagSet.Parse(argumentList)

	return chainrun.Run(stage.ChainSettings{
		ProjectFolderPath:  projectFolderPath,
		StageName:          *stageName,
		JobSettings:        settings,
		VisionModelName:    *visionModelName,
		VisionProviderName: *visionProviderName,
		RunModelStages:     *runModelStages,
	})
}
