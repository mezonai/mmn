package monitoring

import (
	"net/http"
	"time"

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
	TxMempoolFull         TxRejectedReason = "mempool_full"
	TxDuplicated          TxRejectedReason = "duplicated"
	TxRejectedUnknown     TxRejectedReason = "other"
)

type nodePromMetrics struct {
	nodeUpUnixSeconds prometheus.Gauge
	mempoolSize       prometheus.Gauge
	timeToFinality    prometheus.Histogram
	blockTime         prometheus.Histogram
	rejectedTxCount   *prometheus.CounterVec
	blockHeight       prometheus.Gauge
	blockSizeBytes    prometheus.Histogram
	txInBlock         prometheus.Histogram
	ingressTxCount    prometheus.Counter
	receivedTxCount   prometheus.Counter
	peerCount         prometheus.Gauge
	trackerTx         prometheus.GaugeVec
}

func newNodePromMetrics() *nodePromMetrics {
	return &nodePromMetrics{
		nodeUpUnixSeconds: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mmn_node_up_timestamp_unix_seconds",
				Help: "Unix timestamp of the node",
			},
		),
		mempoolSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mmn_node_mempool_size",
				Help: "The total pending transactions queued in node's mempool",
			},
		),
		timeToFinality: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name: "mmn_node_time_to_finality",
				Help: "Latency in second from tx submission until being finalized and will not be reverted",
			},
		),
		blockTime: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name: "mmn_node_block_time",
				Help: "Duration in second between assembling of two consecutive blocks",
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
		blockSizeBytes: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name: "mmn_node_block_size_bytes",
				Help: "The block size in bytes",
			},
		),
		txInBlock: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name: "mmn_node_tx_in_block",
				Help: "Number of tx in block",
			},
		),
		ingressTxCount: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "mmn_node_ingress_tx_count",
				Help: "The total number of ingress transactions (received from client)",
			},
		),
		receivedTxCount: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "mmn_node_received_tx_count",
				Help: "The total number of received transactions (received from broadcast or client)",
			},
		),
		peerCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mmn_node_peer_count",
				Help: "The total number of peer connections",
			},
		),
		trackerTx: *promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mmn_node_tracker_tx",
				Help: "Number of transactions currently being processed (between mempool and ledger)",
			},
			[]string{"source"},
		),
	}
}

var nodeMetrics *nodePromMetrics

// InitMetrics initialize metrics for node but not expose to api yet, and return metrics cleanup function
func InitMetrics() {
	nodeMetrics = newNodePromMetrics()
	nodeMetrics.nodeUpUnixSeconds.SetToCurrentTime()
}

func RegisterMetrics(mux *http.ServeMux) {
	logx.Info("Registering prometheus metrics")
	mux.Handle("/metrics", promhttp.Handler())
}

func SetMempoolSize(size int) {
	nodeMetrics.mempoolSize.Set(float64(size))
}

func RecordTimeToFinality(duration time.Duration) {
	nodeMetrics.timeToFinality.Observe(duration.Seconds())
}

func RecordBlockTime(duration time.Duration) {
	nodeMetrics.blockTime.Observe(duration.Seconds())
}

func RecordRejectedTx(reason TxRejectedReason) {
	nodeMetrics.rejectedTxCount.With(prometheus.Labels{
		"reason": string(reason),
	}).Inc()
}

func SetBlockHeight(blockHeight uint64) {
	nodeMetrics.blockHeight.Set(float64(blockHeight))
}

func RecordBlockSizeBytes(sizeBytes int) {
	nodeMetrics.blockSizeBytes.Observe(float64(sizeBytes))
}

func RecordTxInBlock(txCount int) {
	nodeMetrics.txInBlock.Observe(float64(txCount))
}

func IncreaseIngressTxCount() {
	nodeMetrics.ingressTxCount.Inc()
}

func IncreaseReceivedTxCount() {
	nodeMetrics.receivedTxCount.Inc()
}

func SetPeerCount(peers int) {
	nodeMetrics.peerCount.Set(float64(peers))
}

func SetTrackerProcessingTx(bytes int64, source string) {
	nodeMetrics.trackerTx.With(prometheus.Labels{
		"source": source,
	}).Set(float64(bytes))
}