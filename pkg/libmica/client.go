// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	defs "mica-shim/definitions"
	er "mica-shim/errors"
	log "mica-shim/logger"
	"mica-shim/pkg/pedestal"
	utils "mica-shim/pkg/utils"
	"path/filepath"
	"strings"
)

// Type Definitions

type MicaCommand string
type PedType int
type MicaState string
type MicaService string

// Constants

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "rm"
	MPause  MicaCommand = "pause"
	MResume MicaCommand = "resume"
	MStatus MicaCommand = "status"
	// miuca set <short_id> MemoryInMiB/CPUCapacity <value>
	MUpdate MicaCommand = "set"

	// TODO:
	// Mica message field length constants
	MaxNameLen         = 32
	MaxFirmwarePathLen = 128
	MaxCPUStringLen    = 128
	MaxNetworkLen      = 512
)

const (
	Xen PedType = iota
	ACRN
	FusionDock
)

const (
	unknown    MicaState = "unknown"
	offline    MicaState = "Offline"
	configured MicaState = "Configured"
	ready      MicaState = "Ready"
	running    MicaState = "Running"
	suspended  MicaState = "Suspended"
	stopped    MicaState = "Stopped"
	stateErr   MicaState = "Error"
)

const (
	servicePTY   MicaService = "pty"
	serviceRPC   MicaService = "rpc"
	serviceUMT   MicaService = "umt"
	serviceDebug MicaService = "debug"
)

type MicaExecutor struct {
	records MicaClientConf
	Id string
}

// Structs and Methods

// MicaStatus represents the complete status of a MICA client
// TODO: remove Raw field in the future for space saving
type MicaStatus struct {
	Name     string        `json:"name"`
	CPU      string        `json:"cpu"`
	State    MicaState     `json:"state"`
	Services []MicaService `json:"services"`
	Raw      string        `json:"raw"` // Original raw response
}

// string returns a string representation of MicaStatus
func (ms MicaStatus) string() string {
	return fmt.Sprintf("Name: %s, CPU: %s, State: %s, Services: %v",
		ms.Name, ms.CPU, ms.State, ms.Services)
}

// isRunning checks if the client is in running state
func (ms MicaStatus) isRunning() bool {
	return ms.State == running
}

// IsStopped checks if the client is in stopped state
func (ms MicaStatus) IsStopped() bool {
	return ms.State == stopped
}

// hasService checks if the client has a specific service
func (ms MicaStatus) hasService(service MicaService) bool {
	for _, s := range ms.Services {
		if s == service {
			return true
		}
	}
	return false
}

// isValid checks if the status contains valid information
func (ms MicaStatus) isValid() bool {
	return ms.Name != "" && isValidCPUString(ms.CPU) && ms.State != unknown
}

type mcsFS struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Ped     PedType  `json:"ped"`
	OS      string   `json:"os"`
	Mounted bool     `json:"mounted"`
	Options []string `json:"options"`
}

// MicaClientConfCreateOptions is an intermediate layer to pass configurations to MicaClientConf
type MicaClientConfCreateOptions struct {
	CPU         string
	Name        string
	Path        string
	Ped         string
	PedCfg      string
	Debug       bool
	VCPUs       int
	MaxVCPUs    int
	CPUWeight   int
	CPUCapacity int
	// TODO: add maxmem
	MemoryMB    int
	MaxMemMB   	int
	Network     string
}

// This is the conf struct mica daemon will see
// New struct based on mica daemon msg definition:
// #define MAX_NAME_LEN			32
// #define MAX_FIRMWARE_PATH_LEN	128
// #define MAX_CPUSTR_LEN			128
// #define MAX_NETWORK_LEN		512
// struct create msg {
// /* required configs */
// char name[MAX NAME LEN];
// char path[MAX FIRMWARE PATH LEN];
// /*optional configs for MICA*/
// char ped[MAX NAME LEN];
// char ped cfgIMAX FIRMWARE PATH LEN];
// bool debug;
// /*optional configs for pedestal */
// char cpu str[MAX CPUSTR LEN];
// int vcpu num;
// int maxvcpus num;
// int cpu weight;
// int cpu capacity;
// int memory; // in MB
// int maxmem; // in MB
// char network[MAX NETWORK LEN];
// };
type MicaClientConf struct {
	// name is container ID, assigned by containerd.
	name [MaxNameLen]byte
	// path is the firmware path (<OS>.elf)
	path [MaxFirmwarePathLen]byte
	// ped is string of pedestal type: xen, fusionDock, acrn, etc.
	ped [MaxNameLen]byte
	// for xen, pedcfg is the relative path of <OS>.bin
	pedcfg [MaxFirmwarePathLen]byte
	// debug flag
	debug bool
	// cpuStr is the allowed cpu range => cpu=1-3,5
	cpuStr [MaxCPUStringLen]byte
	// vcpuNum is the number of vcpus
	vcpuNum int
	// cpuWeight is the weight of cpu
	cpuWeight int
	// cpuCapacity is the capacity of cpu
	cpuCapacity int
	// memoryMB size in MiB
	memoryMB int
	// network config
	network [MaxNetworkLen]byte
}

// dummyCPUArr is a dummy CPU array for testing, always [1,4,5]
func dummyCPUArr() []int {
	return []int{1, 4, 5}
}



// autoBoot is a dummy function that always returns false
func autoBoot() bool {
	return false
}

// Deprecated: Use InitWithOpts instead for new implementations.
func (m *MicaClientConf) MockInit(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) {
	name = utils.ShortID(name)
	copy(m.name[:], name)
	copy(m.path[:], path)
	copy(m.ped[:], ped)
	copy(m.pedcfg[:], pedCfg)
	m.debug = debug

	// Set default values for new fields
	// Use dummy CPU array and convert to string
	cpuStr := pedestal.ParseCPUArr(dummyCPUArr())
	copy(m.cpuStr[:], cpuStr)

	m.vcpuNum = 0
	m.cpuWeight = 0
	m.cpuCapacity = 0
	m.memoryMB = 0

	// Clear network field
	for i := range m.network {
		m.network[i] = 0
	}
}

// InitWithOpts initializes MicaClientConf with the new options struct
func (m *MicaClientConf) InitWithOpts(opts MicaClientConfCreateOptions) {
	name := utils.ShortID(opts.Name)
	copy(m.name[:], name)
	copy(m.path[:], opts.Path)
	copy(m.ped[:], opts.Ped)
	copy(m.pedcfg[:], opts.PedCfg)
	m.debug = opts.Debug

	// Convert CPU array to string
	// cpuStr := pedestal.ParseCPUArr(opts.CPU)
	cpuStr := opts.CPU
	copy(m.cpuStr[:], cpuStr)

	// Set other fields
	m.vcpuNum = opts.VCPUs
	m.cpuWeight = opts.CPUWeight
	m.cpuCapacity = opts.CPUCapacity
	m.memoryMB = opts.MemoryMB
	copy(m.network[:], opts.Network)
}

func (m *MicaClientConf) pack() []byte {
	// Calculate total buffer size:
	// name[32] + path[128] + ped[32] + pedcfg[128] + debug(1) + cpuStr[128] +
	// vcpuNum(4) + cpuWeight(4) + cpuCapacity(4) + memory(4) + network[512]
	buf := make([]byte, MaxNameLen+MaxFirmwarePathLen+MaxNameLen+MaxFirmwarePathLen+1+MaxCPUStringLen+4+4+4+4+MaxNetworkLen) // Total: 993 bytes

	offset := 0
	copy(buf[offset:offset+MaxNameLen], m.name[:])
	offset += MaxNameLen
	copy(buf[offset:offset+MaxFirmwarePathLen], m.path[:])
	offset += MaxFirmwarePathLen
	copy(buf[offset:offset+MaxNameLen], m.ped[:])
	offset += MaxNameLen
	copy(buf[offset:offset+MaxFirmwarePathLen], m.pedcfg[:])
	offset += MaxFirmwarePathLen

	if m.debug {
		buf[offset] = 1
	} else {
		buf[offset] = 0
	}
	offset += 1

	copy(buf[offset:offset+MaxCPUStringLen], m.cpuStr[:])
	offset += MaxCPUStringLen
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.vcpuNum))
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.cpuWeight))
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.cpuCapacity))
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.memoryMB))
	offset += 4
	copy(buf[offset:offset+MaxNetworkLen], m.network[:])

	return buf
}

// Compatitble with status filter
type Filter struct {
	Name string
	Ped  bool
}

// Public API

// NewMicaCreateMsg creates and initializes a MicaClientConf.
// Deprecated: Use NewMicaCreateMsgWithOpts instead for new implementations.
func NewMicaCreateMsg(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) MicaClientConf {
	msg := MicaClientConf{}
	// Convert simple parameters to the new options format
	opts := MicaClientConfCreateOptions{
		CPU:         pedestal.ParseCPUArr(dummyCPUArr()),
		Name:        name,
		Path:        path,
		Ped:         ped,
		PedCfg:      pedCfg,
		Debug:       debug,
		VCPUs:        0,
		CPUWeight:   0,
		CPUCapacity: 0,
		MemoryMB:      0,
		Network:     "",
	}
	msg.InitWithOpts(opts)
	return msg
}

// NewMicaCreateMsgWithOpts creates and initializes a MicaClientConf with the new options struct.
func NewMicaCreateMsgWithOpts(opts MicaClientConfCreateOptions) MicaClientConf {
	msg := MicaClientConf{}
	msg.InitWithOpts(opts)
	return msg
}

// Create creates a new mica client.
// Use MicaCtl to control the mica client.
func Create(config MicaClientConf) error {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	return s.handleMsg(config.pack())
}

func CreateMicaClient(conf MicaClientConf) error {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// Do not dereference s here, as it is dropped in handleMsg().
	msg := conf.pack()
	if err := s.handleMsg(msg); err != nil {
		return err
	}
	return nil
}

// TODO: consider better way to parse variable parameters
func micaCtl(cmd MicaCommand, rawId string, opts... string) error {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		return er.ErrMicadNotRunning
	}
	// workaround: pause => stop
	switch cmd {
	case MPause:
		cmd = MStop
	case MResume:
		cmd = MStart
	}
	shortId := utils.ShortID(rawId)
	clientSocketPath := filepath.Join(defs.MicaStateDir, shortId+".socket")
	var s *micaSocket
	if defs.IsMock {
		s = newMicaSocket(defs.MicaCreatSocketPath)
	} else {
		s = newMicaSocket(clientSocketPath)
	}
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

func Start(id string) error {
	if err := micaCtl(MStart, id); err != nil {
		return fmt.Errorf("failed to start container %s", id)
	}
	return nil
}

// TODO: Extend mica response data, loading more information
// TODO: if client.socket does not exist, return nil; the logic is in dangerous, 
// we have to make sure that client os is down really
func Stop(id string) error {
	if completelyDown(id) {
		log.Infof("%s is already down, not need to stop it", id)
	}
	if err := micaCtl(MStop, id); err != nil {
		return fmt.Errorf("failed to stop mica client %s %w", id, err)
	}
	return nil
}

// TALK: xen supports pause, but mica...
// TODO: might passthrough mica, directly to ped?
func Pause(id string) error {
	if pedestal.GetHostPed() == pedestal.Xen {
		return pedestal.Pause(utils.ShortID(id))	
	} else {
		if err := micaCtl(MPause, id); err != nil {
			return fmt.Errorf("failed to pause mica client %s %w", id, err)
		}
		return nil
	}
}

// TODO: mica may not support, we handle this via ped directly
func Resume(id string) error {
	if pedestal.GetHostPed() == pedestal.Xen {
		return pedestal.Resume(utils.ShortID(id))	
	} else {
		if err := micaCtl(MResume, id); err != nil {
			return fmt.Errorf("failed to pause mica client %s %w", id, err)
		}
		return nil
	}
}

func Remove(id string) error {
	return micaCtl(MRemove, id)
}

// Status returns structured status information for a specific client
// TODO: support filter?
func Status(id string, filter Filter) (*MicaStatus, error) {
	res, err := queryStatus(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get status for client %s: %v", id, err)
	}

	status, err := parseMicaStatus(res)
	if err != nil {
		return nil, fmt.Errorf("failed to parse status for client %s: %v", id, err)
	}

	if !status.isValid() {
		return nil, fmt.Errorf("invalid status for client %s: %s", id, status.Raw)
	}

	// Apply filter if specified
	if filter.Ped && !status.hasService(servicePTY) {
		return nil, fmt.Errorf("client %s does not have PTY service", id)
	}

	return status, nil
}

// StatusToString converts MicaStatus back to string format for backward compatibility
func StatusToString(status *MicaStatus) string {
	if status == nil {
		return ""
	}
	return status.Raw
}

// FilterStatuses filters a list of statuses based on criteria
func FilterStatuses(statuses []*MicaStatus, nameFilter string, stateFilter MicaState, serviceFilter MicaService) []*MicaStatus {
	var filtered []*MicaStatus

	for _, status := range statuses {
		// Name filter
		if nameFilter != "" && !strings.Contains(status.Name, nameFilter) {
			continue
		}

		// State filter
		if stateFilter != unknown && status.State != stateFilter {
			continue
		}

		// Service filter
		if serviceFilter != "" && !status.hasService(serviceFilter) {
			continue
		}

		filtered = append(filtered, status)
	}

	return filtered
}
