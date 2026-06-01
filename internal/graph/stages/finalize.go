// Package stages provides pipeline stages for graph building.
package stages

import (
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"go.uber.org/zap"
)

// Finalize performs final cleanup and reporting.
func Finalize(ctx *PipelineContext) error {
	start := time.Now()
	defer func() {
		ctx.RecordTiming("finalize", time.Since(start))
	}()

	// Log summary
	logger.Info("Build pipeline completed",
		zap.Int("totalNodes", len(ctx.AllNodes)),
		zap.Int("totalFiles", len(ctx.ParseResults)),
		zap.Bool("isFullBuild", ctx.IsFullBuild),
		zap.Duration("totalDuration", time.Since(ctx.BuildStart)),
	)

	// Log timing breakdown
	if len(ctx.Timing) > 0 {
		logger.Info("Timing breakdown")
		for stage, duration := range ctx.Timing {
			logger.Info("  "+stage, zap.Duration("duration", duration))
		}
	}

	return nil
}