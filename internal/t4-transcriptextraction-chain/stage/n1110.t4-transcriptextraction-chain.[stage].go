package stage

import (
	"fmt"
	"time"

	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t4-transcriptextraction-chain/chainunit"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/run"
	"github.com/kcg-ml/kcg-llm.transcript-extraction/internal/t3-transcriptextraction-job/unit"
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
	JobSettings        unit.JobSettings
	VisionModelName    string
	VisionProviderName string
	RunModelStages     bool
}

// RunRuleStage runs one rule stage and reports how long it took.
func RunRuleStage(stageName string, stageFunction func() error) error {
	fmt.Printf("\n$ %s\n", stageName)
	startedAt := time.Now()
	runError := stageFunction()
	fmt.Printf("  %.1fs\n", time.Since(startedAt).Seconds())
	if runError != nil {
		fmt.Printf("  FAILED: %v\n", runError)
	}
	return runError
}

// RunJobStage runs one durable job.
func RunJobStage(settings ChainSettings, jobName string) error {
	unitList, unitError := chainunit.UnitListOf(settings.ProjectFolderPath, jobName)
	if unitError != nil {
		return unitError
	}
	jobSettings := settings.JobSettings
	jobSettings.JobName = jobName
	if jobName == chainunit.JobNamePhotoContent {
		if settings.VisionModelName == "" {
			fmt.Println("\nskipped the photo model calls: give --vision-model to run them")
			return nil
		}
		jobSettings.ModelName = settings.VisionModelName
		jobSettings.ProviderName = settings.VisionProviderName
	}
	fmt.Printf("\n$ job --job %s\n", jobName)
	exitCode := run.Run(settings.ProjectFolderPath, jobSettings, unitList)
	if exitCode != 0 && exitCode != 2 {
		return fmt.Errorf("job %s exited with code %d", jobName, exitCode)
	}
	return nil
}
