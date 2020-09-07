package mem

import (
	"fmt"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/system"
)

type MemStats struct {
	ps system.PS
}

func (_ *MemStats) Description() string {
	return "Read metrics about memory usage"
}

func (_ *MemStats) SampleConfig() string { return "" }

func uint64Format(metric uint64) float64 {
	metricStr := strconv.FormatUint(metric, 10)
	metricFloat, _ := strconv.ParseFloat(metricStr,64)
	return metricFloat
}

func (s *MemStats) Gather(acc telegraf.Accumulator) error {
	vm, err := s.ps.VMStat()
	if err != nil {
		return fmt.Errorf("error getting virtual memory info: %s", err)
	}

	fields := map[string]interface{}{
		"total":             uint64Format(vm.Total),
		"available":         uint64Format(vm.Available),
		"used":              uint64Format(vm.Used),
		"free":              uint64Format(vm.Free),
		"cached":            uint64Format(vm.Cached),
		"buffered":          uint64Format(vm.Buffers),
		"active":            uint64Format(vm.Active),
		"inactive":          uint64Format(vm.Inactive),
		"wired":             uint64Format(vm.Wired),
		"slab":              uint64Format(vm.Slab),
		"used_percent":      100 * float64(vm.Used) / float64(vm.Total),
		"available_percent": 100 * float64(vm.Available) / float64(vm.Total),
		"commit_limit":      uint64Format(vm.CommitLimit),
		"committed_as":      uint64Format(vm.CommittedAS),
		"dirty":             uint64Format(vm.Dirty),
		"high_free":         uint64Format(vm.HighFree),
		"high_total":        uint64Format(vm.HighTotal),
		"huge_page_size":    uint64Format(vm.HugePageSize),
		"huge_pages_free":   uint64Format(vm.HugePagesFree),
		"huge_pages_total":  uint64Format(vm.HugePagesTotal),
		"low_free":          uint64Format(vm.LowFree),
		"low_total":         uint64Format(vm.LowTotal),
		"mapped":            uint64Format(vm.Mapped),
		"page_tables":       uint64Format(vm.PageTables),
		"shared":            uint64Format(vm.Shared),
		"swap_cached":       uint64Format(vm.SwapCached),
		"swap_free":         uint64Format(vm.SwapFree),
		"swap_total":        uint64Format(vm.SwapTotal),
		"vmalloc_chunk":     uint64Format(vm.VMallocChunk),
		"vmalloc_total":     uint64Format(vm.VMallocTotal),
		"vmalloc_used":      uint64Format(vm.VMallocUsed),
		"write_back":        uint64Format(vm.Writeback),
		"write_back_tmp":    uint64Format(vm.WritebackTmp),
	}
	acc.AddGauge("mem", fields, nil)

	return nil
}

func init() {
	ps := system.NewSystemPS()
	inputs.Add("mem", func() telegraf.Input {
		return &MemStats{ps: ps}
	})
}
