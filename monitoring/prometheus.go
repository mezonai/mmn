package monitoring

import (
	"github.com/mezonai/mmn/logx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

type nodePromMetrics struct {
	mempoolSize        *prometheus.GaugeVec
	txFinalizationTime *prometheus.HistogramVec
}

func newNodePromMetrics() *nodePromMetrics {
	return &nodePromMetrics{
		mempoolSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mmn_node_mempool_size",
				Help: "The total pending transactions queued in node's mempool",
			},
			[]string{},
		),
		txFinalizationTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "mmn_node_tx_finalization_time",
				Help: "Latency from submission to inclusion in block",
			},
			[]string{},
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
	nodeMetrics.mempoolSize.With(prometheus.Labels{}).Set(float64(size))
}

func RecordTxFinalizationTime(durationInSec float64) {
	nodeMetrics.txFinalizationTime.With(prometheus.Labels{}).Observe(durationInSec)
}
