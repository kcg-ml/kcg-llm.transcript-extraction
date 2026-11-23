// Package pipelinechain runs the stages of the extraction chain in order.
//
// The rule stages make no model call, so they run to completion in seconds. A
// model stage runs through the durable job runner, which resumes after a kill.
package pipelinechain

import (
	"fmt"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/durablejob"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/jobunit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/messagesplit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/photoextraction"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/phototable"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/referenceextraction"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevanceconsensus"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevancesolo"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/relevancewindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summaryaggregation"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/summarywindow"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/telegramexport"
)

// TextExportFolderName is the folder of the export that holds text only.
const TextExportFolderName = "extract.telegram-log.vlad"

// ImageExportFolderName is the folder of the export that holds the photos.
const ImageExportFolderName = "extract.telegram-log.vlad-images"

// StageNameList names the stages a reader can run on their own.
var StageNameList = []string{"extract", "split", "references", "windows",
	"relevance-windows", "relevance-solo", "relevance-consensus", "photos",
	"photo-index", "aggregate", "rules", "all"}

// ChainSettings holds the options of one chain run.
type ChainSettings struct {
	ProjectFolderPath  string
	StageName          string
	JobSettings        durablejob.JobSettings
	VisionModelName    string
	VisionProviderName string
	RunModelStages     bool
}

// runRuleStage runs one rule stage and reports how long it took.
func runRuleStage(stageName string, stageFunction func() error) error {
	fmt.Printf("\n$ %s\n", stageName)
	startedAt := time.Now()
	runError := stageFunction()
	fmt.Printf("  %.1fs\n", time.Since(startedAt).Seconds())
	if runError != nil {
		fmt.Printf("  FAILED: %v\n", runError)
	}
	return runError
}

// runJobStage runs one durable job.
func runJobStage(settings ChainSettings, jobName string) error {
	unitList, unitError := jobunit.UnitListOf(settings.ProjectFolderPath, jobName)
	if unitError != nil {
		return unitError
	}
	jobSettings := settings.JobSettings
	jobSettings.JobName = jobName
	if jobName == "photo" {
		if settings.VisionModelName == "" {
			fmt.Println("\nskipped the photo model calls: give --vision-model to run them")
			return nil
		}
		jobSettings.ModelName = settings.VisionModelName
		jobSettings.ProviderName = settings.VisionProviderName
	}
	fmt.Printf("\n$ job --job %s\n", jobName)
	exitCode := durablejob.Run(settings.ProjectFolderPath, jobSettings, unitList)
	if exitCode != 0 && exitCode != 2 {
		return fmt.Errorf("job %s exited with code %d", jobName, exitCode)
	}
	return nil
}

// RunRules runs the 4 rule stages that make no model call.
func RunRules(projectFolderPath string) error {
	ruleStageList := []struct {
		stageName     string
		stageFunction func() error
	}{
		{"extract", func() error {
			return telegramexport.Run(projectFolderPath, TextExportFolderName)
		}},
		{"split", func() error { return messagesplit.Run(projectFolderPath) }},
		{"references", func() error { return referenceextraction.Run(projectFolderPath) }},
		{"windows", func() error { return summarywindow.Run(projectFolderPath) }},
		{"relevance-windows", func() error { return relevancewindow.Run(projectFolderPath, false) }},
	}
	for _, ruleStage := range ruleStageList {
		if runError := runRuleStage(ruleStage.stageName, ruleStage.stageFunction); runError != nil {
			return runError
		}
	}
	return nil
}

// Run executes the chain for one stage name.
func Run(settings ChainSettings) error {
	projectFolderPath := settings.ProjectFolderPath
	stageName := settings.StageName
	runRules := stageName == "all" || stageName == "rules"

	if runRules {
		if runError := RunRules(projectFolderPath); runError != nil {
			return runError
		}
	}
	if stageName == "extract" {
		return runRuleStage("extract", func() error {
			return telegramexport.Run(projectFolderPath, TextExportFolderName)
		})
	}
	if stageName == "split" {
		return runRuleStage("split", func() error { return messagesplit.Run(projectFolderPath) })
	}
	if stageName == "references" {
		return runRuleStage("references", func() error {
			return referenceextraction.Run(projectFolderPath)
		})
	}
	if stageName == "windows" {
		return runRuleStage("windows", func() error { return summarywindow.Run(projectFolderPath) })
	}
	if stageName == "relevance-windows" {
		return runRuleStage("relevance-windows", func() error {
			return relevancewindow.Run(projectFolderPath, false)
		})
	}
	if stageName == "relevance-solo" {
		return runRuleStage("relevance-solo", func() error {
			return relevancesolo.Run(projectFolderPath, false)
		})
	}
	if stageName == "relevance-consensus" {
		return runRuleStage("relevance-consensus", func() error {
			return relevanceconsensus.Run(projectFolderPath)
		})
	}
	if stageName == "photos" {
		return runRuleStage("photos", func() error {
			return photoextraction.Run(projectFolderPath, ImageExportFolderName, false, false)
		})
	}
	if stageName == "photo-index" {
		return runRuleStage("photo-index", func() error { return phototable.Run(projectFolderPath) })
	}
	if stageName == "aggregate" {
		if runError := runRuleStage("aggregate", func() error {
			return summaryaggregation.Run(projectFolderPath)
		}); runError != nil {
			return runError
		}
		return runRuleStage("photo-index", func() error { return phototable.Run(projectFolderPath) })
	}

	if !settings.RunModelStages {
		fmt.Println("\npipeline done (rule stages only)")
		return nil
	}

	for _, jobName := range []string{"w10", "rel", "photo"} {
		if runError := runJobStage(settings, jobName); runError != nil {
			return runError
		}
	}
	if runError := runRuleStage("relevance-solo", func() error {
		return relevancesolo.Run(projectFolderPath, false)
	}); runError != nil {
		return runError
	}
	if runError := runRuleStage("relevance-consensus", func() error {
		return relevanceconsensus.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}
	if runError := runRuleStage("aggregate", func() error {
		return summaryaggregation.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}
	for _, jobName := range []string{"w100", "month", "meta"} {
		if runError := runJobStage(settings, jobName); runError != nil {
			return runError
		}
		if runError := runRuleStage("aggregate", func() error {
			return summaryaggregation.Run(projectFolderPath)
		}); runError != nil {
			return runError
		}
	}
	if runError := runRuleStage("photo-index", func() error {
		return phototable.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}

	fmt.Println("\npipeline done")
	return nil
}
