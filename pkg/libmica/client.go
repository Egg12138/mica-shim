// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	defs "mica-shim/definitions"
	utils "mica-shim/pkg/fileutils"
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
	MStatus MicaCommand = "status"
)

const (
	Baremetal PedType = iota
	Jailhouse
	Xen
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
	Name     string      `json:"name"`
	CPU      string         `json:"cpu"`
	State    MicaState   `json:"state"`
	Services []MicaService `json:"services"`
	Raw      string      `json:"raw"` // Original raw response
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

// NOTICE: we have to ensure the length of each field consistency with the length of the field in mica daemon
// This is the conf struct mica daemon will see
// TODO: add explaination for each field
type MicaClientConf struct {
	// cpu is the scheduled CPU.
	cpu uint32
	// name is assigned by containerd.
	name [32]byte
	// path is the relative path in the bundle.
	path   [128]byte
	ped    [32]byte
	pedcfg [128]byte
	debug  bool
}

func (m *MicaClientConf) Init(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) {
	m.cpu = cpu
	name = utils.ShortID(name)
	copy(m.name[:], name)
	copy(m.path[:], path)
	copy(m.ped[:], ped)
	copy(m.pedcfg[:], pedCfg)
	m.debug = debug
}

func (m *MicaClientConf) pack() []byte {
	buf := make([]byte, 4+32+128+32+128+1) // Total: 325 bytes

	binary.LittleEndian.PutUint32(buf[0:4], m.cpu)
	copy(buf[4:36], m.name[:])
	copy(buf[36:164], m.path[:])
	copy(buf[164:196], m.ped[:])
	copy(buf[196:324], m.pedcfg[:])

	if m.debug {
		buf[324] = 1
	} else {
		buf[324] = 0
	}

	return buf
}

// Compatitble with status filter
type Filter struct {
	Name string
	Ped  bool
}

// Public API

// NewMicaCreateMsg creates and initializes a MicaClientConf.
func NewMicaCreateMsg(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) MicaClientConf {
	msg := MicaClientConf{}
	msg.Init(cpu, name, path, ped, pedCfg, debug)
	return msg
}

// MicaCreate creates a new mica client.
// Use MicaCtl to control the mica client.
func MicaCreate(config MicaClientConf) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	return s.handleMsg(config.pack())
}

func CreateMicaClient(conf MicaClientConf) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// Do not dereference s here, as it is dropped in handleMsg().
	msg := conf.pack()
	return s.handleMsg(msg)
}

func MicaCtl(cmd MicaCommand, rawId string) (string, error) {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		return "", fmt.Errorf("mica socket directory does not exist, please check if micad is running")
	}
	shortId := utils.ShortID(rawId)
	clientSocketPath := filepath.Join(defs.MicaStateDir, shortId+".socket")
	s := newMicaSocket(clientSocketPath)
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

func StartMicaClient(id string) (string, error) {
	return MicaCtl(MStart, id)
}

// TODO: Extend mica response data, loading more information
// BUG: mica daemon stop command does not handle error, always return success
func Stop(id string) (error) {
	res, err := MicaCtl(MStop, id)
	if err != nil || !success(res) {
		return fmt.Errorf("failed to stop mica client %s, resposne = <%s>: %w", id, res, err)
	}
	return nil
}

func Remove(id string) (string, error) {
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
	res, err := MicaCtl(MStatus, id)
	if err != nil {
		// MicaCtl might already return a detailed error.
		// We can add more context here if needed.
		return "", fmt.Errorf("failed to query status for client %s via MicaCtl: %w", id, err)
	}
	return res, nil
}

// parseMicaStatus parses the raw status response from micad into MicaStatus struct
// Format: "name                          cpu                state               services"
func parseMicaStatus(rawResponse string) (*MicaStatus, error) {
	if rawResponse == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Check for error responses
	if strings.Contains(rawResponse, defs.MicaFailed) || strings.Contains(rawResponse, "Error") {
		return nil, fmt.Errorf("error response: %s", rawResponse)
	}

	// Parse the formatted response
	// Expected format: "name                          cpu                state               services"
	fields := strings.Fields(rawResponse)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid status format: %s", rawResponse)
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
		Raw:      rawResponse,
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
