// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	utils "mica-shim/pkg/fileutils"
	"mica-shim/pkg/pedestal"
	"path/filepath"
	"strconv"
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
	MStatus MicaCommand = "status"

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
	CPU         []int
	Name        string
	Path        string
	Ped         string
	PedCfg      string
	Debug       bool
	VCPU        int
	CPUWeight   int
	CPUCapacity int
	Memory      int
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
// int cpu weight;
// int cpu capacity;
// int memory;
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
	// memory size
	memory int
	// network config
	network [MaxNetworkLen]byte
}

// dummyCPUArr is a dummy CPU array for testing, always [1,4,5]
func dummyCPUArr() []int {
	return []int{1, 4, 5}
}

// ParseCPUArr translates CPU array to CPU string format
// Example: [1,4,5] -> "1,4-5"
func ParseCPUArr(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}

	// Sort the CPU array
	sorted := make([]int, len(cpus))
	copy(sorted, cpus)

	// Simple bubble sort for small arrays
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Convert to string format
	var result strings.Builder
	start := sorted[0]
	end := sorted[0]

	for i := 1; i < len(sorted); i++ {
		if sorted[i] == end+1 {
			// Continue the range
			end = sorted[i]
		} else {
			// End the current range and start a new one
			if start == end {
				result.WriteString(strconv.Itoa(start))
			} else {
				result.WriteString(strconv.Itoa(start))
				result.WriteString("-")
				result.WriteString(strconv.Itoa(end))
			}
			result.WriteString(",")
			start = sorted[i]
			end = sorted[i]
		}
	}

	// Add the last range
	if start == end {
		result.WriteString(strconv.Itoa(start))
	} else {
		result.WriteString(strconv.Itoa(start))
		result.WriteString("-")
		result.WriteString(strconv.Itoa(end))
	}

	return result.String()
}

// autoBoot is a dummy function that always returns false
func autoBoot() bool {
	return false
}

// Deprecated: Use InitWithOpts instead for new implementations.
func (m *MicaClientConf) Init(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) {
	name = utils.ShortID(name)
	copy(m.name[:], name)
	copy(m.path[:], path)
	copy(m.ped[:], ped)
	copy(m.pedcfg[:], pedCfg)
	m.debug = debug

	// Set default values for new fields
	// Use dummy CPU array and convert to string
	cpuStr := ParseCPUArr(dummyCPUArr())
	copy(m.cpuStr[:], cpuStr)

	m.vcpuNum = 0
	m.cpuWeight = 0
	m.cpuCapacity = 0
	m.memory = 0

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
	cpuStr := ParseCPUArr(opts.CPU)
	copy(m.cpuStr[:], cpuStr)

	// Set other fields
	m.vcpuNum = opts.VCPU
	m.cpuWeight = opts.CPUWeight
	m.cpuCapacity = opts.CPUCapacity
	m.memory = opts.Memory
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
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.memory))
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
		CPU:         dummyCPUArr(), // Use dummy CPU array as default
		Name:        name,
		Path:        path,
		Ped:         ped,
		PedCfg:      pedCfg,
		Debug:       debug,
		VCPU:        0,
		CPUWeight:   0,
		CPUCapacity: 0,
		Memory:      0,
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

func MicaCtl(cmd MicaCommand, rawId string) error {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		return fmt.Errorf("mica socket directory does not exist, please check if micad is running")
	}
	shortId := utils.ShortID(rawId)
	clientSocketPath := filepath.Join(defs.MicaStateDir, shortId+".socket")
	s := newMicaSocket(clientSocketPath)
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

func Start(id string) error {
	if err := MicaCtl(MStart, id); err != nil {
		return fmt.Errorf("failed to start container %s", id)
	}
	return nil
}

// TODO: Extend mica response data, loading more information
// BUG: mica daemon stop command does not handle error, always return success
func Stop(id string) error {
	if err := MicaCtl(MStop, id); err != nil {
		return fmt.Errorf("failed to stop mica client %s %w", id, err)
	}
	return nil
}

// TALK: xen supports pause, but mica...
// TODO: might passthrough mica, directly to ped?
func Pause(id string) error {
	if err := MicaCtl(MPause, id); err != nil {
		return fmt.Errorf("failed to pause mica client %s %w", id, err)
	}
	return nil
}

// TODO: mica may not support, we handle this via ped directly
func Resume(id string) error {
	log.Debugf("resuming %s", id)
	shortId := utils.ShortID(id)
	return pedestal.Resume(shortId)
}

func Remove(id string) error {
	return MicaCtl(MRemove, id)
}

// Status returns structured status information for a specific client
// TODO: adapt return type to containerd-compatible status type
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

// Private Helpers

// validStatusResponse validates if the response string contains valid status information
// This function is kept for backward compatibility, but new code should use parseMicaStatus
// Consider this case: communication with mica daemon failed due to incorrect disconnection
// but status information is received	from mica daemon.
func validStatusResponse(res string) bool {
	if res == "" {
		return false
	}

	// Use the new parsing logic for validation
	status, err := parseMicaStatus(res)
	if err != nil {
		return false
	}

	return status.isValid()
}

func queryStatus(id string) (string, error) {
	// MicaCtl will construct the path to the client's specific control socket:
	// e.g., /tmp/mica/<socketId>.socket
	// It will then send the MStatus command to this socket.
	// BUG: micactl status will write status information directly to stdout!!!!
	// we have to manually parse status
	if err := MicaCtl(MStatus, id); err != nil {
		// MicaCtl might already return a detailed error.
		// We can add more context here if needed.
		return "", fmt.Errorf("failed to query status for client %s via MicaCtl: %w", id, err)
	}
	return "", nil
}

// parseMicaStatus parses the raw status response from micad into MicaStatus struct
// Format: "name                          cpu                state               services"
func parseMicaStatus(rawOutput string) (*MicaStatus, error) {
	if rawOutput == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Check for error responses
	if strings.Contains(rawOutput, defs.MicaFailed) || strings.Contains(rawOutput, "Error") {
		return nil, fmt.Errorf("error response: %s", rawOutput)
	}

	// Parse the formatted response
	// Expected format: "name                          cpu                state               services"
	fields := strings.Fields(rawOutput)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid status format: %s", rawOutput)
	}

	// Parse CPU field - now supports multi-core format like
	// "1-3,5" and empty string
	cpuStr := fields[1]
	if !isValidCPUString(cpuStr) {
		return nil, fmt.Errorf("invalid CPU field format: %s", cpuStr)
	}

	// If CPU string is empty, use MaxCPUNum() as fallback
	if cpuStr == "" {
		maxCPU := MaxCPUNum()
		if maxCPU > 0 {
			// Convert to range format (e.g., "0-3" for 4 CPUs)
			cpuStr = fmt.Sprintf("0-%d", maxCPU-1)
		} else {
			return nil, fmt.Errorf("failed to get max CPU number for empty CPU string")
		}
	}

	// Parse state
	state := parseMicaState(fields[2])
	if state == unknown {
		return nil, fmt.Errorf("unknown state: %s", fields[2])
	}

	// Parse services (if any)
	services := parseMicaServices(fields[3:])

	return &MicaStatus{
		Name:     fields[0],
		CPU:      cpuStr,
		State:    state,
		Services: services,
		Raw:      rawOutput,
	}, nil
}

// parseMicaState converts string to MicaState
func parseMicaState(stateStr string) MicaState {
	switch stateStr {
	case "Offline":
		return offline
	case "Configured":
		return configured
	case "Ready":
		return ready
	case "Running":
		return running
	case "Suspended":
		return suspended
	case "Stopped":
		return stopped
	case "Error":
		return stateErr
	// Add more states as needed
	default:
		return unknown
	}
}

// parseMicaServices extracts service information from response fields
func parseMicaServices(fields []string) []MicaService {
	var services []MicaService

	for _, field := range fields {
		serviceStr := strings.ToLower(field)
		switch {
		case strings.Contains(serviceStr, "pty"):
			services = append(services, servicePTY)
		case strings.Contains(serviceStr, "rpc"):
			services = append(services, serviceRPC)
		case strings.Contains(serviceStr, "umt"):
			services = append(services, serviceUMT)
		case strings.Contains(serviceStr, "debug"):
			services = append(services, serviceDebug)
		}
	}

	return services
}

// isValidCPUString validates the CPU string format
// Supports formats: "1", "1-3", "2-3,15", "1,13,5", ""(empty is All)
// NOTICE: Xen-related validation function
func isValidCPUString(cpuStr string) bool {
	// cpuStr == "" means all physical CPUs which are not pinned to Dom0
	if cpuStr == "" {
		return true
	}

	// Split by comma for multiple groups
	groups := strings.Split(cpuStr, ",")

	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			return false
		}

		// Check if it's a range (contains dash)
		if strings.Contains(group, "-") {
			parts := strings.Split(group, "-")
			if len(parts) != 2 {
				return false
			}

			// Validate both parts are integers
			start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

			if err1 != nil || err2 != nil || start < 0 || end < 0 || start > end {
				return false
			}
		} else {
			// Single CPU number
			if _, err := strconv.Atoi(group); err != nil {
				return false
			}
		}
	}

	return true
}

// ParseCPUString parses the CPU string format and returns individual CPU cores
// Examples: "1-3" -> [1,2,3], "2-3,5" -> [2,3,5], "1,3,5" -> [1,3,5]
func ParseCPUString(cpuStr string) ([]int, error) {
	if !isValidCPUString(cpuStr) {
		return nil, fmt.Errorf("invalid CPU string format: %s", cpuStr)
	}

	var cpus []int

	// Empty string means no specific CPUs
	if cpuStr == "" {
		return cpus, nil
	}

	groups := strings.Split(cpuStr, ",")

	for _, group := range groups {
		group = strings.TrimSpace(group)

		if strings.Contains(group, "-") {
			// Range format: "1-3"
			parts := strings.Split(group, "-")
			start, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, _ := strconv.Atoi(strings.TrimSpace(parts[1]))

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU: "5"
			cpu, _ := strconv.Atoi(group)
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

func success(res string) bool {
	return res != "" && !strings.Contains(res, defs.MicaFailed) && !strings.Contains(res, "Error")
}
