//go:generate go run github.com/Al2Klimov/go-gen-source-repos

package main

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	linux "github.com/Al2Klimov/go-linux-apis"
	goMonPlugUtils "github.com/Al2Klimov/go-monplug-utils"
)

type nullWriter struct {
}

func (nullWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type thresholdRule struct {
	dev             *regexp.Regexp
	metricOffset    func(*perfdataDev) *perfdataMetric
	thresholdOffset func(*perfdataMetric) *goMonPlugUtils.OptionalThreshold
	threshold       goMonPlugUtils.OptionalThreshold
}

type perfdataMetric struct {
	total, perSecond goMonPlugUtils.Perfdata
}

type perfdataRx struct {
	bytes, packets, errs, drop, fifo, frame, compressed, multicast perfdataMetric
}

type perfdataTx struct {
	bytes, packets, errs, drop, fifo, colls, carrier, compressed perfdataMetric
}

type perfdataDev struct {
	rx perfdataRx
	tx perfdataTx
}

type metricOffset struct {
	netdev   func(*linux.NetDev) *uint64
	perfdata func(*perfdataDev) *perfdataMetric
}

type patternValues struct {
	patterns []*regexp.Regexp
}

var _ flag.Value = (*patternValues)(nil)

func (pv *patternValues) String() string {
	rendered := make([]string, 0, len(pv.patterns))
	for _, p := range pv.patterns {
		rendered = append(rendered, p.String())
	}

	return strings.Join(rendered, " ")
}

func (pv *patternValues) Set(s string) error {
	pv.patterns = append(pv.patterns, patternToRegex(s))
	return nil
}

var rArg = regexp.MustCompile(`\A(.+):([rt]x:.+?):((?:total|persec):[wc])=(.+?)\z`)
var rPerfLabel = regexp.MustCompile(`\A(.+):(\w+):(\w+):(\w+)\z`)

var metricOffsets = map[string]metricOffset{
	"rx:bytes":      {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Bytes }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.bytes }},
	"rx:packets":    {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Packets }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.packets }},
	"rx:errs":       {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Errs }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.errs }},
	"rx:drop":       {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Drop }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.drop }},
	"rx:fifo":       {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Fifo }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.fifo }},
	"rx:frame":      {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Frame }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.frame }},
	"rx:compressed": {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Compressed }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.compressed }},
	"rx:multicast":  {func(nd *linux.NetDev) *uint64 { return &nd.Receive.Multicast }, func(pd *perfdataDev) *perfdataMetric { return &pd.rx.multicast }},
	"tx:bytes":      {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Bytes }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.bytes }},
	"tx:packets":    {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Packets }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.packets }},
	"tx:errs":       {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Errs }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.errs }},
	"tx:drop":       {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Drop }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.drop }},
	"tx:fifo":       {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Fifo }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.fifo }},
	"tx:colls":      {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Colls }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.colls }},
	"tx:carrier":    {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Carrier }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.carrier }},
	"tx:compressed": {func(nd *linux.NetDev) *uint64 { return &nd.Transmit.Compressed }, func(pd *perfdataDev) *perfdataMetric { return &pd.tx.compressed }},
}

var thresholdOffsets = map[string]func(*perfdataMetric) *goMonPlugUtils.OptionalThreshold{
	"total:w":  func(root *perfdataMetric) *goMonPlugUtils.OptionalThreshold { return &root.total.Warn },
	"total:c":  func(root *perfdataMetric) *goMonPlugUtils.OptionalThreshold { return &root.total.Crit },
	"persec:w": func(root *perfdataMetric) *goMonPlugUtils.OptionalThreshold { return &root.perSecond.Warn },
	"persec:c": func(root *perfdataMetric) *goMonPlugUtils.OptionalThreshold { return &root.perSecond.Crit },
}

func main() {
	os.Exit(goMonPlugUtils.ExecuteCheck(onTerminal, checkLinuxNetdev))
}

func onTerminal() (output string) {
	return fmt.Sprintf(
		"For the terms of use, the source code and the authors\n"+
			"see the projects this program is assembled from:\n\n  %s\n",
		strings.Join(GithubcomAl2klimovGoGenSourceRepos, "\n  "),
	)
}

// nolint:funlen,gocognit
func checkLinuxNetdev() (output string, perfdata goMonPlugUtils.PerfdataCollection, errs map[string]error) {
	cli := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	cli.SetOutput(nullWriter{})

	duration := cli.Duration("d", time.Minute, "")

	var exclude patternValues

	cli.Var(&exclude, "e", "")

	if cli.Parse(os.Args[1:]) != nil {
		return "", nil, getUsageErrs()
	}

	args := cli.Args()
	rules := make([]thresholdRule, len(args))

	for i, arg := range args {
		match := rArg.FindStringSubmatch(arg)
		if match == nil {
			return "", nil, getUsageErrs()
		}

		if _, hasMetric := metricOffsets[match[2]]; !hasMetric {
			return "", nil, getUsageErrs()
		}

		var threshold goMonPlugUtils.OptionalThreshold

		if threshold.Set(match[4]) != nil {
			return "", nil, getUsageErrs()
		}

		rules[i] = thresholdRule{
			patternToRegex(match[1]),
			metricOffsets[match[2]].perfdata,
			thresholdOffsets[match[3]],
			threshold,
		}
	}

	if *duration < 1 {
		*duration = 1
	}

	signal.Ignore(syscall.SIGTERM)

	netDev1, errND1 := linux.GetNetDev()
	if errND1 != nil {
		return "", nil, map[string]error{"/proc/net/dev": errND1}
	}

	time.Sleep(*duration)

	netDev2, errND2 := linux.GetNetDev()
	if errND2 != nil {
		return "", nil, map[string]error{"/proc/net/dev": errND2}
	}

	perfdata = make(goMonPlugUtils.PerfdataCollection, 0, len(netDev1)*32)
	div := float64(*duration) / float64(time.Second)

Dev:
	for dev, before := range netDev1 {
		after, hasAfter := netDev2[dev]
		if !hasAfter {
			continue
		}

		for _, e := range exclude.patterns {
			if e.MatchString(dev) {
				continue Dev
			}
		}

		var perfdataPerDev perfdataDev

		perfdataPerDev.rx.bytes.perSecond.UOM = "B"
		perfdataPerDev.tx.bytes.perSecond.UOM = "B"

		for _, rule := range rules {
			threshold := rule.thresholdOffset(rule.metricOffset(&perfdataPerDev))
			if !threshold.IsSet && rule.dev.MatchString(dev) {
				*threshold = rule.threshold
			}
		}

		for name, offset := range metricOffsets {
			metric := offset.perfdata(&perfdataPerDev)
			prefix := fmt.Sprintf("%s:%s:", dev, name)
			before := offset.netdev(&before) // nolint:gosec,scopelint
			after := offset.netdev(&after)

			metric.total.Label = prefix + "total"
			metric.total.Value = float64(*after)
			metric.total.UOM = "c"
			metric.total.Min = goMonPlugUtils.OptionalNumber{
				IsSet: true,
				Value: 0,
			}

			metric.perSecond.Label = prefix + "persec"
			metric.perSecond.Value = float64(*after-*before) / div

			perfdata = append(perfdata, metric.total, metric.perSecond)
		}
	}

	sort.Slice(perfdata, func(i, j int) bool {
		a := perfdata[i].GetStatus()
		b := perfdata[j].GetStatus()

		if a == b {
			return perfdata[i].Label < perfdata[j].Label
		}

		return a > b
	})

	var wc, ok strings.Builder

	for _, buf := range [2]*strings.Builder{&wc, &ok} {
		buf.WriteString(`<table><thead><tr><th>Device</th><th>Metric</th><th>Value</th></tr></thead><tbody>`)
	}

	for _, pd := range perfdata {
		if match := rPerfLabel.FindStringSubmatch(pd.Label); match != nil {
			var buf *strings.Builder

			if pd.GetStatus() == goMonPlugUtils.Ok {
				buf = &ok
			} else {
				buf = &wc
			}

			buf.WriteString(`<tr><td>`)
			buf.WriteString(html.EscapeString(match[1]))
			buf.WriteString(`</td><td>`)
			buf.WriteString(match[2])
			buf.WriteByte(' ')
			buf.WriteString(match[3])

			if match[4] == "persec" {
				buf.WriteString(`/s`)
			}

			buf.WriteString(`</td><td>`)
			buf.WriteString(strconv.FormatFloat(pd.Value, 'f', -1, 64))
			buf.WriteString(`</td></tr>`)
		}
	}

	for _, buf := range [2]*strings.Builder{&wc, &ok} {
		buf.WriteString(`</tbody></table>`)
	}

	output = wc.String() + "\n\n" + ok.String()

	return
}

func getUsageErrs() map[string]error {
	return map[string]error{
		"Usage": errors.New(os.Args[0] + " [-d DURATION] [INTERFACE:METRIC:THRESHOLD=RANGE ...]"),
	}
}

func patternToRegex(pattern string) *regexp.Regexp {
	regex := regexp.QuoteMeta(pattern)
	regex = strings.ReplaceAll(regex, `\?`, `.`)
	regex = strings.ReplaceAll(regex, `\*`, `.*`)

	return regexp.MustCompile(`\A` + regex + `\z`)
}
