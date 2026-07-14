package main

import "sort"

type systemCPUSet struct {
	ID                    uint32
	Group                 uint16
	LogicalProcessorIndex uint8
	CoreIndex             uint8
	EfficiencyClass       uint8
}

type cpuCoreKey struct {
	group uint16
	core  uint8
}

func allocateServerCPUSets(serverIDs []string, cpuSets []systemCPUSet) map[string][]uint32 {
	allocated := make(map[string][]uint32, len(serverIDs))
	if len(serverIDs) == 0 || len(cpuSets) == 0 {
		return allocated
	}
	ids := append([]string(nil), serverIDs...)
	sort.Strings(ids)
	sets := append([]systemCPUSet(nil), cpuSets...)
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].Group != sets[j].Group {
			return sets[i].Group < sets[j].Group
		}
		if sets[i].CoreIndex != sets[j].CoreIndex {
			return sets[i].CoreIndex < sets[j].CoreIndex
		}
		return sets[i].LogicalProcessorIndex < sets[j].LogicalProcessorIndex
	})
	cores := make([][]uint32, 0)
	var previous cpuCoreKey
	for index, cpuSet := range sets {
		key := cpuCoreKey{group: cpuSet.Group, core: cpuSet.CoreIndex}
		if index == 0 || key != previous {
			cores = append(cores, nil)
			previous = key
		}
		cores[len(cores)-1] = append(cores[len(cores)-1], cpuSet.ID)
	}
	for index, core := range cores {
		serverID := ids[index%len(ids)]
		allocated[serverID] = append(allocated[serverID], core...)
	}
	for index, serverID := range ids {
		if len(allocated[serverID]) == 0 {
			allocated[serverID] = append([]uint32(nil), cores[index%len(cores)]...)
		}
		sort.Slice(allocated[serverID], func(i, j int) bool { return allocated[serverID][i] < allocated[serverID][j] })
	}
	return allocated
}
