package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	natsURL    = flag.String("nats-url", "http://localhost:8222", "NATS monitoring URL")
	listenAddr = flag.String("listen", ":9100", "Address to listen on for metrics")
)

type NATSConnection struct {
	CID            int64  `json:"cid"`
	Name           string `json:"name"`
	IP             string `json:"ip"`
	Port           int    `json:"port"`
	PendingBytes   int64  `json:"pending_bytes"`
	InMsgs         int64  `json:"in_msgs"`
	OutMsgs        int64  `json:"out_msgs"`
	InBytes        int64  `json:"in_bytes"`
	OutBytes       int64  `json:"out_bytes"`
	Subscriptions  int    `json:"subscriptions"`
}

type NATSConnzResponse struct {
	Connections []NATSConnection `json:"connections"`
}

type NATSCollector struct {
	inMsgs        *prometheus.Desc
	outMsgs       *prometheus.Desc
	inBytes       *prometheus.Desc
	outBytes      *prometheus.Desc
	pendingBytes  *prometheus.Desc
	subscriptions *prometheus.Desc
}

func NewNATSCollector() *NATSCollector {
	return &NATSCollector{
		inMsgs: prometheus.NewDesc(
			"nats_connection_in_msgs",
			"Number of messages received by this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
		outMsgs: prometheus.NewDesc(
			"nats_connection_out_msgs",
			"Number of messages sent by this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
		inBytes: prometheus.NewDesc(
			"nats_connection_in_bytes",
			"Number of bytes received by this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
		outBytes: prometheus.NewDesc(
			"nats_connection_out_bytes",
			"Number of bytes sent by this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
		pendingBytes: prometheus.NewDesc(
			"nats_connection_pending_bytes",
			"Number of pending bytes for this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
		subscriptions: prometheus.NewDesc(
			"nats_connection_subscriptions",
			"Number of subscriptions for this connection",
			[]string{"cid", "ip", "port", "name"},
			nil,
		),
	}
}

func (c *NATSCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.inMsgs
	ch <- c.outMsgs
	ch <- c.inBytes
	ch <- c.outBytes
	ch <- c.pendingBytes
	ch <- c.subscriptions
}

func (c *NATSCollector) Collect(ch chan<- prometheus.Metric) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(*natsURL + "/connz")
	if err != nil {
		log.Printf("Error fetching NATS connz: %v", err)
		return
	}
	defer resp.Body.Close()

	var connz NATSConnzResponse
	if err := json.NewDecoder(resp.Body).Decode(&connz); err != nil {
		log.Printf("Error decoding NATS connz: %v", err)
		return
	}

	for _, conn := range connz.Connections {
		cid := fmt.Sprintf("%d", conn.CID)
		port := fmt.Sprintf("%d", conn.Port)
		name := conn.Name
		if name == "" {
			name = "unknown"
		}

		ch <- prometheus.MustNewConstMetric(
			c.inMsgs, prometheus.CounterValue, float64(conn.InMsgs),
			cid, conn.IP, port, name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.outMsgs, prometheus.CounterValue, float64(conn.OutMsgs),
			cid, conn.IP, port, name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.inBytes, prometheus.CounterValue, float64(conn.InBytes),
			cid, conn.IP, port, name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.outBytes, prometheus.CounterValue, float64(conn.OutBytes),
			cid, conn.IP, port, name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.pendingBytes, prometheus.GaugeValue, float64(conn.PendingBytes),
			cid, conn.IP, port, name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.subscriptions, prometheus.GaugeValue, float64(conn.Subscriptions),
			cid, conn.IP, port, name,
		)
	}
}

func main() {
	flag.Parse()

	collector := NewNATSCollector()
	prometheus.MustRegister(collector)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>NATS Connection Exporter</title></head>
<body>
<h1>NATS Connection Exporter</h1>
<p><a href="/metrics">Metrics</a></p>
</body>
</html>`))
	})

	log.Printf("Starting NATS connection exporter on %s", *listenAddr)
	log.Printf("Monitoring NATS server at %s", *natsURL)
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
}
