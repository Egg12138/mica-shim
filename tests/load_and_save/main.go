package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/containerd/containerd/api/types/task"
)

// ContainerConfig represents the configuration similar to the project.
type ContainerConfig struct {
	// OCI Specification fields (simplified for testing).
	Spec struct {
		Process struct {
			Terminal bool `json:"terminal"`
			User     struct {
				UID uint32 `json:"uid"`
				GID uint32 `json:"gid"`
			} `json:"user"`
			Args []string `json:"args"`
			Env  []string `json:"env"`
			Cwd  string   `json:"cwd"`
		} `json:"process"`
		Linux struct {
			Resources struct {
				CPU struct {
					Quota  *int64 `json:"quota"`
					Period *int64 `json:"period"`
					Shares *uint64 `json:"shares"`
					Cpus   string `json:"cpus"`
				} `json:"cpu"`
				Memory struct {
					Limit      *int64 `json:"limit"`
					Reservation *int64 `json:"reservation"`
					Swap       *int64 `json:"swap"`
				} `json:"memory"`
			} `json:"resources"`
		} `json:"linux"`
	} `json:"spec"`

	// Bundle information.
	Bundle string            `json:"bundle"`
	Type   ContainerType     `json:"type"`
	Detach bool              `json:"detach"`
	ExtraLabels map[string]string `json:"extra_labels"`

	// MICA-specific configurations (simplified).
	OS           string `json:"os"`
	Ncpu         int    `json:"ncpu"`
	CpuLimit     int    `json:"cpu_limit"`
	MemoryLimit  int64  `json:"memory_limit"`
}

// ContainerType represents the type of container.
type ContainerType string

const (
	PodContainer ContainerType = "pod"
	PodSandbox   ContainerType = "sandbox"
	Regular      ContainerType = "regular"
	UnknownCtype ContainerType = "unknown"
)

// Container represents the container structure with static and dynamic fields.
type Container struct {
	// Dynamic fields.
	ExitTime time.Time `json:"exit_time"`
	ExitCode uint32    `json:"exit_code"`

	// Static fields.
	Bundle   string        `json:"bundle"`
	ID       string        `json:"id"`
	ShortID  string        `json:"short_id"`
	Status   task.Status   `json:"status"`
	CType    ContainerType `json:"c_type"`
	Config   *ContainerConfig `json:"config"`
}

// State represents the static state of a container for saving/loading.
type State struct {
	Bundle   string        `json:"bundle"`
	ID       string        `json:"id"`
	ShortID  string        `json:"short_id"`
	Status   task.Status   `json:"status"`
	CType    ContainerType `json:"c_type"`
	Config   *ContainerConfig `json:"config"`
}

func generateRandomContainer() *Container {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	id := fmt.Sprintf("container-%d-%d", rand.Intn(10000), rand.Intn(10000))
	shortID := id[:12]

	bundle := fmt.Sprintf("/tmp/bundle-%d", rand.Intn(10000))

	config := &ContainerConfig{
		Bundle: bundle,
		Type:   UnknownCtype, // Random type
		Detach: rand.Intn(2) == 1,
		ExtraLabels: make(map[string]string),
		OS:     []string{"zephyr","uniproton"}[rand.Intn(2)],
		Ncpu:   rand.Intn(8) + 1,
		// ignore the constraint "Ncpu <= CpuLimit"
		CpuLimit: rand.Intn(16),
		MemoryLimit: int64(rand.Intn(4096) * 1024 * 1024), // Random MB in bytes
	}

	for i := 0; i < rand.Intn(5)+2; i++ {
		key := fmt.Sprintf("label-%d", i)
		value := fmt.Sprintf("value-%d", rand.Intn(100))
		config.ExtraLabels[key] = value
	}

	config.Spec.Process.Terminal = rand.Intn(2) == 1
	config.Spec.Process.User.UID = uint32(rand.Intn(65536))
	config.Spec.Process.User.GID = uint32(rand.Intn(65536))
	config.Spec.Process.Cwd = "/tmp"
	
	for i := 0; i < rand.Intn(5)+1; i++ {
		config.Spec.Process.Args = append(config.Spec.Process.Args, fmt.Sprintf("arg-%d", i))
	}
	
	for i := 0; i < rand.Intn(8)+2; i++ {
		config.Spec.Process.Env = append(config.Spec.Process.Env, fmt.Sprintf("ENV_%d=value%d", i, rand.Intn(100)))
	}

	if rand.Intn(2) == 1 {
		quota := int64(rand.Intn(100000))
		config.Spec.Linux.Resources.CPU.Quota = &quota
	}
	if rand.Intn(2) == 1 {
		period := int64(100000)
		config.Spec.Linux.Resources.CPU.Period = &period
	}
	if rand.Intn(2) == 1 {
		shares := uint64(rand.Intn(1024) + 512)
		config.Spec.Linux.Resources.CPU.Shares = &shares
	}

	if rand.Intn(2) == 1 {
		limit := int64(rand.Intn(8192) * 1024 * 1024)
		config.Spec.Linux.Resources.Memory.Limit = &limit
	}

	return &Container{
		ExitTime: time.Now().Add(time.Duration(rand.Intn(3600)) * time.Second),
		ExitCode: uint32(rand.Intn(256)),
		Bundle:   bundle,
		ID:       id,
		ShortID:  shortID,
		Status:   task.Status(rand.Intn(8)), // Random status
		CType:    Regular,
		Config:   config,
	}
}

// Get the static state of the container.
func (c *Container) State() *State {
	return &State{
		Bundle:   c.Bundle,
		ID:       c.ID,
		ShortID:  c.ShortID,
		Status:   c.Status,
		CType:    c.CType,
		Config:   c.Config,
	}
}

func (c *Container) SaveState(filePath string) error {
	state := c.State()
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal container state: %w", err)
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, stateBytes, 0644); err != nil {
		return fmt.Errorf("failed to write state to file: %w", err)
	}

	return nil
}

func LoadContainerFromState(filePath string) (*Container, error) {
	stateBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return &Container{
		ExitTime: time.Time{}, // Dynamic field not saved
		ExitCode: 0,          // Dynamic field not saved
		Bundle:   state.Bundle,
		ID:       state.ID,
		ShortID:  state.ShortID,
		Status:   state.Status,
		CType:    state.CType,
		Config:   state.Config,
	}, nil
}

// Compare two containers to see if their static fields match.
func compareContainers(original, restored *Container) bool {
	if original.Bundle != restored.Bundle {
		fmt.Printf("Bundle mismatch: %s != %s\n", original.Bundle, restored.Bundle)
		return false
	}
	if original.ID != restored.ID {
		fmt.Printf("ID mismatch: %s != %s\n", original.ID, restored.ID)
		return false
	}
	if original.ShortID != restored.ShortID {
		fmt.Printf("ShortID mismatch: %s != %s\n", original.ShortID, restored.ShortID)
		return false
	}
	if original.Status != restored.Status {
		fmt.Printf("Status mismatch: %d != %d\n", original.Status, restored.Status)
		return false
	}
	if original.CType != restored.CType {
		fmt.Printf("CType mismatch: %s != %s\n", original.CType, restored.CType)
		return false
	}

	// Compare config
	if original.Config == nil || restored.Config == nil {
		if original.Config != restored.Config {
			fmt.Println("Config nil mismatch")
			return false
		}
	} else {
		if original.Config.Bundle != restored.Config.Bundle {
			fmt.Printf("Config.Bundle mismatch: %s != %s\n", original.Config.Bundle, restored.Config.Bundle)
			return false
		}
		if original.Config.Type != restored.Config.Type {
			fmt.Printf("Config.Type mismatch: %s != %s\n", original.Config.Type, restored.Config.Type)
			return false
		}
		if original.Config.OS != restored.Config.OS {
			fmt.Printf("Config.OS mismatch: %s != %s\n", original.Config.OS, restored.Config.OS)
			return false
		}
		if original.Config.Ncpu != restored.Config.Ncpu {
			fmt.Printf("Config.Ncpu mismatch: %d != %d\n", original.Config.Ncpu, restored.Config.Ncpu)
			return false
		}
		if original.Config.CpuLimit != restored.Config.CpuLimit {
			fmt.Printf("Config.CpuLimit mismatch: %d != %d\n", original.Config.CpuLimit, restored.Config.CpuLimit)
			return false
		}
		if original.Config.MemoryLimit != restored.Config.MemoryLimit {
			fmt.Printf("Config.MemoryLimit mismatch: %d != %d\n", original.Config.MemoryLimit, restored.Config.MemoryLimit)
			return false
		}

		// Compare extra labels
		if len(original.Config.ExtraLabels) != len(restored.Config.ExtraLabels) {
			fmt.Printf("ExtraLabels length mismatch: %d != %d\n", len(original.Config.ExtraLabels), len(restored.Config.ExtraLabels))
			return false
		}
		for k, v := range original.Config.ExtraLabels {
			if restored.Config.ExtraLabels[k] != v {
				fmt.Printf("ExtraLabels[%s] mismatch: %s != %s\n", k, v, restored.Config.ExtraLabels[k])
				return false
			}
		}

		// Compare spec fields
		if original.Config.Spec.Process.Terminal != restored.Config.Spec.Process.Terminal {
			fmt.Printf("Spec.Process.Terminal mismatch: %v != %v\n", original.Config.Spec.Process.Terminal, restored.Config.Spec.Process.Terminal)
			return false
		}
		if original.Config.Spec.Process.User.UID != restored.Config.Spec.Process.User.UID {
			fmt.Printf("Spec.Process.User.UID mismatch: %d != %d\n", original.Config.Spec.Process.User.UID, restored.Config.Spec.Process.User.UID)
			return false
		}
		if original.Config.Spec.Process.User.GID != restored.Config.Spec.Process.User.GID {
			fmt.Printf("Spec.Process.User.GID mismatch: %d != %d\n", original.Config.Spec.Process.User.GID, restored.Config.Spec.Process.User.GID)
			return false
		}
		if original.Config.Spec.Process.Cwd != restored.Config.Spec.Process.Cwd {
			fmt.Printf("Spec.Process.Cwd mismatch: %s != %s\n", original.Config.Spec.Process.Cwd, restored.Config.Spec.Process.Cwd)
			return false
		}

		if len(original.Config.Spec.Process.Args) != len(restored.Config.Spec.Process.Args) {
			fmt.Printf("Spec.Process.Args length mismatch: %d != %d\n", len(original.Config.Spec.Process.Args), len(restored.Config.Spec.Process.Args))
			return false
		}
		for i, arg := range original.Config.Spec.Process.Args {
			if restored.Config.Spec.Process.Args[i] != arg {
				fmt.Printf("Spec.Process.Args[%d] mismatch: %s != %s\n", i, arg, restored.Config.Spec.Process.Args[i])
				return false
			}
		}

		if len(original.Config.Spec.Process.Env) != len(restored.Config.Spec.Process.Env) {
			fmt.Printf("Spec.Process.Env length mismatch: %d != %d\n", len(original.Config.Spec.Process.Env), len(restored.Config.Spec.Process.Env))
			return false
		}
		for i, env := range original.Config.Spec.Process.Env {
			if restored.Config.Spec.Process.Env[i] != env {
				fmt.Printf("Spec.Process.Env[%d] mismatch: %s != %s\n", i, env, restored.Config.Spec.Process.Env[i])
				return false
			}
		}
	}

	return true
}

func main() {
	fmt.Println("Testing Container State Save/Load Functionality")
	fmt.Println("==================================================")

	// Test multiple containers with different random data
	numTests := 5
	allPassed := true

	for i := 0; i < numTests; i++ {
		fmt.Printf("\n--- Test %d ---\n", i+1)

		original := generateRandomContainer()
		fmt.Printf("Generated container with ID: %s\n", original.ID)
		fmt.Printf("Type: %s, OS: %s, CPUs: %d\n", original.CType, original.Config.OS, original.Config.Ncpu)

		stateFile := filepath.Join("/tmp", fmt.Sprintf("test_state_%d.json", i))

		fmt.Printf(">>>> Original container: %+v\n", original)
		fmt.Println("Saving state...")
		if err := original.SaveState(stateFile); err != nil {
			fmt.Printf("ERROR: Failed to save state: %v\n", err)
			allPassed = false
			continue
		}
		fmt.Printf("State saved to: %s\n", stateFile)

		fmt.Println("Loading state...")
		restored, err := LoadContainerFromState(stateFile)
		fmt.Printf("<<<<< Restored container: %+v\n", restored)
		if err != nil {
			fmt.Printf("ERROR: Failed to load state: %v\n", err)
			allPassed = false
			continue
		}

		fmt.Println("Comparing original and restored containers...")
		match := compareContainers(original, restored)
		
		if match {
			fmt.Println("✓ PASS: Static fields match perfectly!")
		} else {
			fmt.Println("✗ FAIL: Static fields do not match!")
			allPassed = false
		}

		fmt.Printf("Original exit time: %v\n", original.ExitTime)
		fmt.Printf("Restored exit time: %v\n", restored.ExitTime)
		fmt.Printf("Original exit code: %d\n", original.ExitCode)
		fmt.Printf("Restored exit code: %d\n", restored.ExitCode)
		fmt.Println("(Note: Dynamic fields should be different as they are not saved)")


		// os.Remove(stateFile)
	}

	fmt.Println("\n" + "==================================================")
	if allPassed {
		fmt.Println("🎉 ALL TESTS PASSED! Save/Load functionality works correctly.")
	} else {
		fmt.Println("❌ SOME TESTS FAILED! Check the output above for details.")
	}
}
