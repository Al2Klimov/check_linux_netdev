//go:generate go run vendor/github.com/Al2Klimov/go-gen-source-repos/main.go github.com/Al2Klimov/check_linux_netdev

package main

import (
	"errors"
	"flag"
	"fmt"
	_ "github.com/Al2Klimov/go-gen-source-repos"
	linux "github.com/Al2Klimov/go-linux-apis"
	. "github.com/Al2Klimov/go-monplug-utils"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type nullWriter struct {
}

func (nullWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type thresholdRule struct {
	dev       *regexp.Regexp
	offset    uintptr
	threshold OptionalThreshold
}

type perfdataMetric struct {
	total, perSecond Perfdata
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
	netdev, perfdata uintptr
}

var rArg = regexp.MustCompile(`\A(.+):([rt]x:.+?):((?:total|persec):[wc])=(.+?)\z`)

var metricOffsets = func() map[string]metricOffset {
	type i = uintptr
	type p = unsafe.Pointer

	var nd linux.NetDev
	var pd perfdataDev

	return map[string]metricOffset{
		"rx:bytes":      {i(p(&nd.Receive.Bytes)) - i(p(&nd)), i(p(&pd.rx.bytes)) - i(p(&pd))},
		"rx:packets":    {i(p(&nd.Receive.Packets)) - i(p(&nd)), i(p(&pd.rx.packets)) - i(p(&pd))},
		"rx:errs":       {i(p(&nd.Receive.Errs)) - i(p(&nd)), i(p(&pd.rx.errs)) - i(p(&pd))},
		"rx:drop":       {i(p(&nd.Receive.Drop)) - i(p(&nd)), i(p(&pd.rx.drop)) - i(p(&pd))},
		"rx:fifo":       {i(p(&nd.Receive.Fifo)) - i(p(&nd)), i(p(&pd.rx.fifo)) - i(p(&pd))},
		"rx:frame":      {i(p(&nd.Receive.Frame)) - i(p(&nd)), i(p(&pd.rx.frame)) - i(p(&pd))},
		"rx:compressed": {i(p(&nd.Receive.Compressed)) - i(p(&nd)), i(p(&pd.rx.compressed)) - i(p(&pd))},
		"rx:multicast":  {i(p(&nd.Receive.Multicast)) - i(p(&nd)), i(p(&pd.rx.multicast)) - i(p(&pd))},
		"tx:bytes":      {i(p(&nd.Transmit.Bytes)) - i(p(&nd)), i(p(&pd.tx.bytes)) - i(p(&pd))},
		"tx:packets":    {i(p(&nd.Transmit.Packets)) - i(p(&nd)), i(p(&pd.tx.packets)) - i(p(&pd))},
		"tx:errs":       {i(p(&nd.Transmit.Errs)) - i(p(&nd)), i(p(&pd.tx.errs)) - i(p(&pd))},
		"tx:drop":       {i(p(&nd.Transmit.Drop)) - i(p(&nd)), i(p(&pd.tx.drop)) - i(p(&pd))},
		"tx:fifo":       {i(p(&nd.Transmit.Fifo)) - i(p(&nd)), i(p(&pd.tx.fifo)) - i(p(&pd))},
		"tx:colls":      {i(p(&nd.Transmit.Colls)) - i(p(&nd)), i(p(&pd.tx.colls)) - i(p(&pd))},
		"tx:carrier":    {i(p(&nd.Transmit.Carrier)) - i(p(&nd)), i(p(&pd.tx.carrier)) - i(p(&pd))},
		"tx:compressed": {i(p(&nd.Transmit.Compressed)) - i(p(&nd)), i(p(&pd.tx.compressed)) - i(p(&pd))},
	}
}()

var thresholdOffsets = func() map[string]uintptr {
	type i = uintptr
	type p = unsafe.Pointer

	var root perfdataMetric

	return map[string]uintptr{
		"total:w":  i(p(&root.total.Warn)) - i(p(&root)),
		"total:c":  i(p(&root.total.Crit)) - i(p(&root)),
		"persec:w": i(p(&root.perSecond.Warn)) - i(p(&root)),
		"persec:c": i(p(&root.perSecond.Crit)) - i(p(&root)),
	}
}()

func main() {
	os.Exit(ExecuteCheck(onTerminal, checkLinuxNetdev))
}

func onTerminal() (output string) {
	return fmt.Sprintf(
		"For the terms of use, the source code and the authors\n"+
			"see the projects this program is assembled from:\n\n  %s\n",
		strings.Join(GithubcomAl2klimovGo_gen_source_repos, "\n  "),
	)
}

func checkLinuxNetdev() (output string, perfdata PerfdataCollection, errs map[string]error) {
	cli := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	cli.SetOutput(nullWriter{})

	duration := cli.Duration("d", time.Minute, "")

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

		var threshold OptionalThreshold

		if threshold.Set(match[4]) != nil {
			return "", nil, getUsageErrs()
		}

		regex := regexp.QuoteMeta(match[1])
		regex = strings.Replace(regex, `\?`, `.`, -1)
		regex = strings.Replace(regex, `\*`, `.*`, -1)

		rules[i] = thresholdRule{
			regexp.MustCompile(`\A` + regex + `\z`),
			metricOffsets[match[2]].perfdata + thresholdOffsets[match[3]],
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

	perfdata = make(PerfdataCollection, 0, len(netDev1)*32)
	div := float64(*duration) / float64(time.Second)

	for dev, before := range netDev1 {
		type i = uintptr
		type p = unsafe.Pointer

		after, hasAfter := netDev2[dev]
		if !hasAfter {
			continue
		}

		var perfdataPerDev perfdataDev

		perfdataPerDev.rx.bytes.perSecond.UOM = "B"
		perfdataPerDev.tx.bytes.perSecond.UOM = "B"

		for _, rule := range rules {
			threshold := (*OptionalThreshold)(p(i(p(&perfdataPerDev)) + rule.offset))
			if !threshold.IsSet && rule.dev.MatchString(dev) {
				*threshold = rule.threshold
			}
		}

		for name, offset := range metricOffsets {
			metric := (*perfdataMetric)(p(i(p(&perfdataPerDev)) + offset.perfdata))
			prefix := fmt.Sprintf("%s:%s:", dev, name)
			before := (*uint64)(p(i(p(&before)) + offset.netdev))
			after := (*uint64)(p(i(p(&after)) + offset.netdev))

			metric.total.Label = prefix + "total"
			metric.total.Value = float64(*after)
			metric.total.UOM = "c"
			metric.total.Min = OptionalNumber{true, 0}

			metric.perSecond.Label = prefix + "persec"
			metric.perSecond.Value = float64(*after-*before) / div

			perfdata = append(perfdata, metric.total, metric.perSecond)
		}
	}

	return
}

func getUsageErrs() map[string]error {
	return map[string]error{
		"Usage": errors.New(os.Args[0] + " [-d DURATION] [INTERFACE:METRIC:THRESHOLD=RANGE ...]"),
	}
}
