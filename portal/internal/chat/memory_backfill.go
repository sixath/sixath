package chat

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sixath/framework/memory"
)

const (
	startupBackfillBatchSize = 50
	startupBackfillSleep     = 200 * time.Millisecond
)

var (
	backfillOnce sync.Once
	backfillStartN int
	// backfillLaunch starts the job; tests may replace with a sync stub.
	backfillLaunch = launchUnitVectorBackfill
)

// StartUnitVectorBackfill launches a single background fill-missing job for the
// process. Units must be the durable SessionUnitsBackend (not Facade).
// No-ops when the vector index or embedder is unavailable.
func StartUnitVectorBackfill(units memory.SessionUnitsBackend) {
	backfillOnce.Do(func() {
		backfillStartN++
		idx := sharedUnitVectorIndex()
		emb := DefaultMemoryStoreOptions().UnitEmbedder
		if units == nil || idx == nil || emb == nil {
			return
		}
		backfillLaunch(units, idx, emb)
	})
}

func launchUnitVectorBackfill(units memory.SessionUnitsBackend, idx memory.UnitVectorIndex, emb memory.UnitEmbedder) {
	go func() {
		bf := memory.NewUnitBackfiller(memory.BackfillConfig{
			Units:        units,
			Index:        idx,
			Embedder:     emb,
			Force:        false,
			BatchSize:    startupBackfillBatchSize,
			BatchSleep:   startupBackfillSleep,
			EmbedTripped: memoryEmbedTripped,
		})
		st, err := bf.Run(context.Background())
		if err != nil {
			log.Printf("memory: unit vector backfill error: %v (stats=%+v)", err, st)
			return
		}
		if st.Tripped {
			log.Printf("memory: unit vector backfill embed tripped (stats=%+v)", st)
			return
		}
		log.Printf("memory: unit vector backfill done scanned=%d missing=%d upserted=%d skipped=%d failed=%d",
			st.Scanned, st.Missing, st.Upserted, st.Skipped, st.Failed)
	}()
}

// resetUnitVectorBackfillForTest clears the Once so tests can re-arm Start.
func resetUnitVectorBackfillForTest() {
	backfillOnce = sync.Once{}
	backfillStartN = 0
	backfillLaunch = launchUnitVectorBackfill
}
