package main

type Host struct {
	id         string
	PowerState string
	Boot       uint64
	CpuSample
	MemorySample
}

type CpuSample struct {
	Cores      int64
	Mhz        int64
	Usage      float64 //percent used
	UsageTotal int64   //the wonky mhz * corecount thing for fmware
}

type MemorySample struct {
	Total      int64
	Used       int64
	Percentage float64
}

type Datastore struct {
	Capacity    int64
	Free        int64
	Uncommitted int64
	Accessible  bool
	Type        string
	Usage       float64
}

type VM struct {
	PowerState     string
	Boot           uint64
	MemoryOverhead int64
	CpuSample
	MemorySample
}
