package monitoring

import (
	"net/http"

	"github.com/mezonai/mmn/logx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type TxRejectedReason string

var (
	TxInvalidSignature    TxRejectedReason = "invalid_signature"
	TxSenderNotExist      TxRejectedReason = "sender_not_exist"
	TxInvalidNonce        TxRejectedReason = "invalid_nonce"
	TxTooManyPending      TxRejectedReason = "too_many_pending"
	TxInsufficientBalance TxRejectedReason = "insufficient_balance"
	TxRejectedUnknown     TxRejectedReason = "other"
)

type nodePromMetrics struct {
	mempoolSize        prometheus.Gauge
	txFinalizationTime prometheus.Histogram
	rejectedTxCount    *prometheus.CounterVec
	blockHeight        prometheus.Gauge
	finalizedTxCount   prometheus.Counter
}

func newNodePromMetrics() *nodePromMetrics {
	return &nodePromMetrics{
		mempoolSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mmn_node_mempool_size",
				Help: "The total pending transactions queued in node's mempool",
			},
		),
		txFinalizationTime: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name: "mmn_node_tx_finalization_time",
				Help: "Latency from submission to inclusion in block",
			},
		),
		rejectedTxCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mmn_node_rejected_tx_count",
				Help: "The total number of rejected transactions",
			},
			[]string{"reason"},
		),
		blockHeight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mmn_node_block_height",
				Help: "The current block height",
			},
		),
		finalizedTxCount: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "mmn_node_finalized_tx_count",
				Help: "The total number of transactions processed",
			},
		),
	}
}

var nodeMetrics *nodePromMetrics

func RegisterMetrics(mux *http.ServeMux) {
	logx.Info("Registering prometheus metrics")
	nodeMetrics = newNodePromMetrics()
	mux.Handle("/metrics", promhttp.Handler())
}

func SetMempoolSize(size int) {
	nodeMetrics.mempoolSize.Set(float64(size))
}

func RecordTxFinalizationTime(durationInSec float64) {
	nodeMetrics.txFinalizationTime.Observe(durationInSec)
}

func RecordRejectedTx(reason TxRejectedReason) {
	nodeMetrics.rejectedTxCount.With(prometheus.Labels{
		"reason": string(reason),
	}).Inc()
}

func SetBlockHeight(blockHeight uint64) {
	nodeMetrics.blockHeight.Set(float64(blockHeight))
}

func IncrementFinalizedTxCount() {
	nodeMetrics.finalizedTxCount.Inc()
}
