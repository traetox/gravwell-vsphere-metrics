package main

import (
	"context"
	"errors"
	"math"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	//"github.com/vmware/govmomi/object"
)

type client struct {
	*govmomi.Client
	host string
}

func NewClient(ctx context.Context, host, username, password string) (*client, error) {
	if host == `` {
		return nil, errors.New("missing host")
	} else if username == `` {
		return nil, errors.New("missing username")
	} else if password == `` {
		return nil, errors.New("missing password")
	}
	//var baseurl = fmt.Sprintf("https://username:password@%s"+vim25.Path, host)
	u, err := soap.ParseURL("https://" + host + vim25.Path)
	if err != nil {
		return nil, err
	}
	u.User = url.UserPassword(username, password)
	vclient, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, err
	}
	return &client{
		Client: vclient,
		host:   host,
	}, nil
}

func (c *client) HostCpuMemoryMetrics(ctx context.Context) (hosts map[string]Host, err error) {
	var v *view.ContainerView
	//var ss *object.HostStorageSystem
	m := view.NewManager(c.Client.Client)
	if v, err = m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"HostSystem"}, true); err != nil {
		return
	}
	defer v.Destroy(ctx)
	var hss []mo.HostSystem
	if err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"summary"}, &hss); err != nil {
		return
	}
	hosts = make(map[string]Host, len(hss))
	for _, hs := range hss {
		h := Host{
			id:           hs.Summary.Host.Value,
			PowerState:   string(hs.Summary.Runtime.PowerState),
			CpuSample:    getCpuSet(hs.Summary),
			MemorySample: getMemSet(hs),
		}
		if hs.Summary.Runtime.BootTime != nil {
			h.Boot = uint64(hs.Summary.Runtime.BootTime.Unix())
		}
		hosts[hs.Summary.Config.Name] = h
	}
	return
}

func (c *client) HostDsMetrics(ctx context.Context) (datastores map[string]Datastore, err error) {
	var v *view.ContainerView
	m := view.NewManager(c.Client.Client)
	if v, err = m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"Datastore"}, true); err != nil {
		return
	}
	defer v.Destroy(ctx)
	var dss []mo.Datastore
	if err = v.Retrieve(ctx, []string{"Datastore"}, []string{"summary", "info"}, &dss); err != nil {
		return
	}
	datastores = make(map[string]Datastore, len(dss))
	for _, ds := range dss {
		var usage float64
		used := ds.Summary.Capacity - (ds.Summary.FreeSpace + ds.Summary.Uncommitted)
		if used > 0 {
			usage = math.Round(float64(used*1000)/float64(ds.Summary.Capacity)) / 10.0
		}

		datastores[ds.Summary.Name] = Datastore{
			Capacity:    ds.Summary.Capacity,
			Free:        ds.Summary.FreeSpace,
			Uncommitted: ds.Summary.Uncommitted,
			Accessible:  ds.Summary.Accessible,
			Type:        ds.Summary.Type,
			Usage:       usage,
		}
	}
	return
}

func (c *client) VMMetrics(ctx context.Context, hosts map[string]Host) (vmms map[string]VM, err error) {
	var v *view.ContainerView
	m := view.NewManager(c.Client.Client)
	if v, err = m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"VirtualMachine"}, true); err != nil {
		return
	}
	defer v.Destroy(ctx)
	var vms []mo.VirtualMachine
	if err = v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"summary"}, &vms); err != nil {
		return
	}
	vmms = make(map[string]VM, len(vms))
	for _, vm := range vms {
		v := VM{
			PowerState:     string(vm.Summary.Runtime.PowerState),
			CpuSample:      getVmCpuSet(vm.Summary, hosts),
			MemorySample:   getVmMemSet(vm.Summary),
			MemoryOverhead: vm.Summary.Runtime.MemoryOverhead,
		}
		if vm.Summary.Runtime.BootTime != nil {
			v.Boot = uint64(vm.Summary.Runtime.BootTime.Unix())
		}
		vmms[vm.Summary.Config.Name] = v
	}
	return
}

func getCpuSet(s types.HostListSummary) (cpu CpuSample) {
	cpu.Cores = int64(s.Hardware.NumCpuCores)
	cpu.Mhz = int64(s.Hardware.CpuMhz)
	cpu.UsageTotal = cpu.Mhz * cpu.Cores
	//round CPU usage to the nearest 0.1
	cpu.Usage = math.Round(float64(s.QuickStats.OverallCpuUsage*1000)/float64(cpu.Mhz*cpu.Cores)) / 10.0
	return
}

func getVmCpuSet(s types.VirtualMachineSummary, hosts map[string]Host) (cpu CpuSample) {
	cpu.Cores = int64(s.Config.NumCpu)
	for _, v := range hosts {
		if v.id == s.Runtime.Host.Value {
			cpu.Mhz = v.Mhz
			break
		}
	}
	if cpu.Mhz > 0 {
		cpu.UsageTotal = cpu.Mhz * cpu.Cores
		//round CPU usage to the nearest 0.1
		cpu.Usage = math.Round(float64(s.QuickStats.OverallCpuUsage*1000)/float64(cpu.UsageTotal)) / 10.0
	}
	return
}

func getMemSet(hs mo.HostSystem) (mem MemorySample) {
	mem.Total = hs.Summary.Hardware.MemorySize
	mem.Used = int64(hs.Summary.QuickStats.OverallMemoryUsage) * 1024 * 1024      //reported in MB for some damn reason
	mem.Percentage = math.Round(float64(mem.Used*1000)/float64(mem.Total)) / 10.0 //round to .1
	return
}

func getVmMemSet(vm types.VirtualMachineSummary) (mem MemorySample) {
	mem.Total = int64(vm.Config.MemorySizeMB) * 1024 * 1024
	mem.Used = int64(vm.QuickStats.GuestMemoryUsage) * 1024 * 1024                //reported in MB for some damn reason
	mem.Percentage = math.Round(float64(mem.Used*1000)/float64(mem.Total)) / 10.0 //round to .1
	return
}
