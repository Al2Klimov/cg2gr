package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type multiString []string

var _ flag.Value = (*multiString)(nil)

func (m multiString) String() string {
	return strings.Join(m, " ")
}

func (m *multiString) Set(s string) error {
	*m = append(*m, s)
	return nil
}

type metric struct {
	sysPath, name string
}

func main() {
	interval := flag.Duration("i", time.Minute, "DURATION")
	host := flag.String("h", "127.0.0.1", "HOST")
	port := flag.Uint("p", 2003, "PORT")

	var services multiString
	flag.Var(&services, "s", "SERVICE")

	flag.Parse()

	hostname, errHn := os.Hostname()
	if errHn != nil {
		panic(errHn)
	}

	metricHost := escapeMetric(hostname)

	metrics := make([]metric, 0, len(services)*2)
	for _, service := range services {
		metricService := escapeMetric(service)

		metrics = append(
			metrics,
			metric{
				fmt.Sprintf("/sys/fs/cgroup/cpu/system.slice/%s.service/cpuacct.usage", service),
				fmt.Sprintf("cg2gr.%s.services.%s.cpuacct.usage", metricHost, metricService),
			},
			metric{
				fmt.Sprintf("/sys/fs/cgroup/memory/system.slice/%s.service/memory.usage_in_bytes", service),
				fmt.Sprintf("cg2gr.%s.services.%s.memory.usage_in_bytes", metricHost, metricService),
			},
		)
	}

	conn, errDl := net.Dial("tcp", net.JoinHostPort(*host, strconv.FormatUint(uint64(*port), 10)))
	if errDl != nil {
		panic(errDl)
	}

	connMtx := &sync.Mutex{}

	for _, metric := range metrics {
		metric := metric

		go func() {
			periodically := time.NewTicker(*interval)
			for {
				ts := <-periodically.C

				value, err := ioutil.ReadFile(metric.sysPath)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}

					panic(err)
				}

				buf := &bytes.Buffer{}
				buf.WriteString(metric.name)
				buf.WriteString(" ")
				buf.Write(bytes.TrimSpace(value))

				if _, err := fmt.Fprintf(buf, " %d\n", ts.Unix()); err != nil {
					panic(err)
				}

				connMtx.Lock()
				if _, err := io.Copy(conn, buf); err != nil {
					panic(err)
				}
				connMtx.Unlock()
			}
		}()
	}

	select {}
}

var nonWord = regexp.MustCompile(`\W+`)

func escapeMetric(raw string) string {
	return nonWord.ReplaceAllStringFunc(raw, urlEncode)
}

func urlEncode(raw string) string {
	builder := &strings.Builder{}
	for i := 0; i < len(raw); i++ {
		if _, err := fmt.Fprintf(builder, "%%%02X", raw[i]); err != nil {
			panic(err)
		}
	}

	return builder.String()
}
