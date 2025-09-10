package monitoring

import (
	"github.com/mezonai/mmn/logx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

type nodePromMetrics struct {
	nodeName    string
	mempoolSize *prometheus.GaugeVec
}

func newNodePromMetrics(nodeName string) *nodePromMetrics {
	return &nodePromMetrics{
		nodeName: nodeName,
		mempoolSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mmn_node_mempool_size",
				Help: "The total pending transactions queued in node's mempool",
			},
			[]string{"node"},
		),
	}
}

var nodeMetrics *nodePromMetrics

func RegisterMetrics(mux *http.ServeMux, nodeName string) {
	logx.Info("Registering prometheus metrics")
	nodeMetrics = newNodePromMetrics(nodeName)
	mux.Handle("/metrics", promhttp.Handler())
}

func SetMempoolSize(size int) {
	nodeMetrics.mempoolSize.With(prometheus.Labels{
		"node": nodeMetrics.nodeName,
	}).Set(float64(size))
}
