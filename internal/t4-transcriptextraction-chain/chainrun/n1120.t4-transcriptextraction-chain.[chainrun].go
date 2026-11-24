package chainrun

import (
	"fmt"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-reference/collect"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/consensus"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-aggregate/index"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-split/part"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-summary/prompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/relprompt"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-relevance/solo"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/chainunit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/stage"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t2-transcriptextraction-photo/table"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-export/write"
	translatetable "github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t1-transcriptextraction-translate/table"
)
// RunRules runs the 4 rule stages that make no model call.
func RunRules(projectFolderPath string) error {
	ruleStageList := []struct {
		stageName     string
		stageFunction func() error
	}{
		{"extract", func() error {
			return write.Run(projectFolderPath, stage.TextExportFolderName)
		}},
		{"split", func() error { return part.Run(projectFolderPath) }},
		{"references", func() error { return collect.Run(projectFolderPath) }},
		{"translate", func() error { return translatetable.Run(projectFolderPath) }},
		{"windows", func() error { return prompt.Run(projectFolderPath) }},
		{"relevance-windows", func() error { return relprompt.Run(projectFolderPath, false) }},
	}
	for _, ruleStage := range ruleStageList {
		if runError := stage.RunRuleStage(ruleStage.stageName, ruleStage.stageFunction); runError != nil {
			return runError
		}
	}
	return nil
}

// Run executes the chain for one stage name.
func Run(settings stage.ChainSettings) error {
	projectFolderPath := settings.ProjectFolderPath
	stageName := settings.StageName
	runRules := stageName == "all" || stageName == "rules"

	if runRules {
		if runError := RunRules(projectFolderPath); runError != nil {
			return runError
		}
	}
	if stageName == "extract" {
		return stage.RunRuleStage("extract", func() error {
			return write.Run(projectFolderPath, stage.TextExportFolderName)
		})
	}
	if stageName == "split" {
		return stage.RunRuleStage("split", func() error { return part.Run(projectFolderPath) })
	}
	if stageName == "references" {
		return stage.RunRuleStage("references", func() error {
			return collect.Run(projectFolderPath)
		})
	}
	if stageName == "translate" {
		return stage.RunRuleStage("translate", func() error {
			return translatetable.Run(projectFolderPath)
		})
	}
	if stageName == "windows" {
		return stage.RunRuleStage("windows", func() error { return prompt.Run(projectFolderPath) })
	}
	if stageName == "relevance-windows" {
		return stage.RunRuleStage("relevance-windows", func() error {
			return relprompt.Run(projectFolderPath, false)
		})
	}
	if stageName == "relevance-solo" {
		return stage.RunRuleStage("relevance-solo", func() error {
			return solo.Run(projectFolderPath, false)
		})
	}
	if stageName == "relevance-consensus" {
		return stage.RunRuleStage("relevance-consensus", func() error {
			return consensus.Run(projectFolderPath)
		})
	}
	if stageName == "photos" {
		return stage.RunRuleStage("photos", func() error {
			return table.Run(projectFolderPath, stage.ImageExportFolderName, false, false)
		})
	}
	if stageName == "photo-index" {
		return stage.RunRuleStage("photo-index", func() error { return index.Run(projectFolderPath) })
	}
	if stageName == "aggregate" {
		if runError := stage.RunRuleStage("aggregate", func() error {
			return index.Run(projectFolderPath)
		}); runError != nil {
			return runError
		}
		return stage.RunRuleStage("photo-index", func() error { return index.Run(projectFolderPath) })
	}

	if !settings.RunModelStages {
		fmt.Println("\npipeline done (rule stages only)")
		return nil
	}

	for _, jobName := range []string{chainunit.JobNameMessageTranslation} {
		if runError := stage.RunJobStage(settings, jobName); runError != nil {
			return runError
		}
	}
	// The window prompts carry the English text, so the windows are rendered
	// again once the translations exist.
	if runError := stage.RunRuleStage("windows", func() error {
		return prompt.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}
	if runError := stage.RunRuleStage("relevance-windows", func() error {
		return relprompt.Run(projectFolderPath, false)
	}); runError != nil {
		return runError
	}
	for _, jobName := range []string{chainunit.JobNameSummaryWindowOf10Messages,
		chainunit.JobNameRelevanceWindowVote, chainunit.JobNamePhotoContent} {
		if runError := stage.RunJobStage(settings, jobName); runError != nil {
			return runError
		}
	}
	if runError := stage.RunRuleStage("relevance-solo", func() error {
		return solo.Run(projectFolderPath, false)
	}); runError != nil {
		return runError
	}
	if runError := stage.RunRuleStage("relevance-consensus", func() error {
		return consensus.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}
	if runError := stage.RunRuleStage("aggregate", func() error {
		return index.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}
	for _, jobName := range []string{chainunit.JobNameSummaryWindowOf100Messages,
		chainunit.JobNameSummaryWindowOfOneMonth, chainunit.JobNameSummaryWindowOfWholeChat} {
		if runError := stage.RunJobStage(settings, jobName); runError != nil {
			return runError
		}
		if runError := stage.RunRuleStage("aggregate", func() error {
			return index.Run(projectFolderPath)
		}); runError != nil {
			return runError
		}
	}
	if runError := stage.RunRuleStage("photo-index", func() error {
		return index.Run(projectFolderPath)
	}); runError != nil {
		return runError
	}

	fmt.Println("\npipeline done")
	return nil
}
