package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

var promotionStarWorkerOnce sync.Once

func StartPromotionStarWorker() {
	promotionStarWorkerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				if err := model.ProcessPromotionStarEvents(5000); err != nil {
					logger.LogError(context.Background(), common.LogText("promotion accounting processing failed: %s", err.Error()))
				}
				<-ticker.C
			}
		})
	})
}
